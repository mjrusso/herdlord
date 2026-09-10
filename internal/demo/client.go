package demo

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/target"
)

type demoProfile struct {
	workspaces []string
	tabs       []string
	titles     []string
	actions    []string
}

var demoProfiles = map[string]demoProfile{
	"agi": {
		workspaces: []string{"cognitive-core", "multimodal-lab", "tool-use", "reasoning-evals", "memory"},
		tabs:       []string{"planning", "perception", "retrieval", "reasoning", "evaluation"},
		titles:     []string{"Grounding a response", "Planning tool calls", "Reviewing multimodal context", "Checking factual consistency", "Waiting for human feedback", "Testing long-term memory"},
		actions:    []string{"ranked candidate plans", "grounded a claim", "checked tool output", "reviewed multimodal context", "ran a reasoning evaluation"},
	},
	"rsi": {
		workspaces: []string{"optimizer", "self-review", "benchmark-lab", "safety-gates", "rollback"},
		tabs:       []string{"critique", "mutations", "regression", "comparison", "verification"},
		titles:     []string{"Reviewing its last patch", "Running capability evaluations", "Comparing candidate prompts", "Checking regression gates", "Preparing a rollback", "Measuring improvement"},
		actions:    []string{"critiqued the previous result", "tested a candidate mutation", "measured the benchmark delta", "checked a regression gate", "verified rollback safety"},
	},
	"asi": {
		workspaces: []string{"fleet-control", "world-model", "alignment", "observatory", "compute"},
		tabs:       []string{"coordination", "forecasting", "governance", "containment", "allocation"},
		titles:     []string{"Coordinating specialist agents", "Forecasting downstream effects", "Checking policy constraints", "Allocating compute", "Monitoring the fleet", "Reconciling world models"},
		actions:    []string{"coordinated specialist agents", "updated a world-model forecast", "checked a policy constraint", "balanced compute allocation", "reviewed a containment boundary"},
	},
}

var fallbackDemoProfile = demoProfile{
	workspaces: []string{"project", "api", "client", "docs", "infra", "sandbox"},
	tabs:       []string{"dashboard", "implementation", "tests", "review", "deploy", "scratch"},
	titles:     []string{"Reviewing changes", "Running tests", "Tracing a failure", "Waiting for input", "Preparing a patch"},
	actions:    []string{"completed simulated work", "reviewed a change", "checked test output"},
}

var demoAgents = []string{"codex", "claude", "gemini"}

type targetState struct {
	agents []herdr.Agent
}

type scriptAction struct {
	kind   uint8
	offset int
}

const (
	scriptCycle uint8 = iota
	scriptAdd
	scriptRemove
	scriptMove
	scriptMetadata
)

var defaultScript = []scriptAction{
	{kind: scriptCycle, offset: 0},
	{kind: scriptCycle, offset: 1},
	{kind: scriptAdd},
	{kind: scriptCycle, offset: -1},
	{kind: scriptMetadata, offset: 2},
	{kind: scriptMove, offset: 3},
	{kind: scriptCycle, offset: 4},
	{kind: scriptRemove},
	{kind: scriptMove, offset: 5},
	{kind: scriptMetadata, offset: 6},
}

type Client struct {
	mu           sync.Mutex
	targets      []target.Target
	states       map[string]*targetState
	nextAgent    int
	activeTarget int
	agentOrder   []string
	readCounts   map[string]int
	random       *rand.Rand
	scriptStep   int
}

func New() *Client {
	targets := []target.Target{{Name: "agi"}, {Name: "rsi"}, {Name: "asi"}}
	c := &Client{targets: targets, states: make(map[string]*targetState, len(targets)), readCounts: make(map[string]int), random: rand.New(rand.NewSource(42))}
	for _, configured := range targets {
		c.states[configured.Name] = &targetState{}
	}
	for range 2 {
		for _, status := range []string{"idle", "working", "blocked", "done"} {
			c.addAgent(status)
		}
	}
	return c
}

func (c *Client) Targets() []target.Target {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]target.Target(nil), c.targets...)
}

func (c *Client) AddTarget(configured target.Target) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	latest := append(append([]target.Target(nil), c.targets...), configured)
	if err := target.Validate(latest); err != nil {
		return err
	}
	c.targets = latest
	c.states[configured.Name] = &targetState{}
	return nil
}

func (c *Client) UpdateTarget(original string, configured target.Target) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.targetIndex(original)
	if index < 0 {
		return fmt.Errorf("target %q is not configured", original)
	}
	configured.Paused = c.targets[index].Paused
	latest := append([]target.Target(nil), c.targets...)
	latest[index] = configured
	if err := target.Validate(latest); err != nil {
		return err
	}
	state := c.states[original]
	delete(c.states, original)
	c.states[configured.Name] = state
	c.targets = latest
	return nil
}

func (c *Client) RemoveTarget(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.targetIndex(name)
	if index < 0 {
		return fmt.Errorf("target %q is not configured", name)
	}
	c.removeTargetAt(index)
	return nil
}

func (c *Client) ToggleTargetPaused(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.targetIndex(name)
	if index < 0 {
		return fmt.Errorf("target %q is not configured", name)
	}
	c.targets[index].Paused = !c.targets[index].Paused
	return nil
}

func (c *Client) Status(_ context.Context, _ target.Target) (herdr.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return herdr.Status{Protocol: herdr.Protocol, Version: "demo", Path: "demo"}, nil
}

func (c *Client) Snapshot(_ context.Context, configured target.Target, _ string) ([]herdr.Agent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.states[configured.Name]
	if !ok {
		return nil, errors.New("simulated target is offline")
	}
	return append([]herdr.Agent(nil), state.agents...), nil
}

func (c *Client) Read(_ context.Context, configured target.Target, _ string, pane string, _ int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, exists := c.states[configured.Name]
	if !exists {
		return "", errors.New("simulated target disappeared")
	}
	var selected *herdr.Agent
	for i := range state.agents {
		if state.agents[i].PaneID == pane {
			selected = &state.agents[i]
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("simulated agent %q is unavailable", pane)
	}
	c.readCounts[pane]++
	read := c.readCounts[pane]
	if read%11 == 0 {
		return "", errors.New("simulated terminal read failed")
	}
	if read%7 == 0 {
		return "", nil
	}
	return simulatedOutput(configured.Name, *selected, read), nil
}

func (c *Client) removeAgent(pane string) string {
	targetIndex, agentIndex := c.findAgent(pane)
	if targetIndex < 0 {
		return "no agent available to remove"
	}
	name := c.targets[targetIndex].Name
	state := c.states[name]
	state.agents = append(state.agents[:agentIndex], state.agents[agentIndex+1:]...)
	c.removeAgentFromOrder(pane)
	delete(c.readCounts, pane)
	return fmt.Sprintf("%s / %s left the pasture", name, pane)
}

func (c *Client) cycleAgent(pane string) string {
	targetIndex, agentIndex := c.findAgent(pane)
	if targetIndex < 0 {
		return "no agent available to change"
	}
	name := c.targets[targetIndex].Name
	agent := &c.states[name].agents[agentIndex]
	previous := agent.Status
	switch previous {
	case "idle":
		agent.Status = "working"
	case "working":
		agent.Status = "blocked"
	case "blocked":
		agent.Status = "done"
	default:
		agent.Status = "idle"
	}
	agent.Revision++
	return fmt.Sprintf("%s / %s  %s -> %s", name, pane, previous, agent.Status)
}

func (c *Client) ScriptStep() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.targets) == 0 {
		return ""
	}
	action := defaultScript[c.scriptStep%len(defaultScript)]
	c.scriptStep++
	switch action.kind {
	case scriptCycle:
		if pane := c.scriptAgent(action.offset); pane != "" {
			return c.cycleAgent(pane)
		}
	case scriptAdd:
		return c.addAgent("idle")
	case scriptRemove:
		if pane := c.scriptAgent(-1); pane != "" {
			return c.removeAgent(pane)
		}
	case scriptMove:
		if pane := c.scriptAgent(action.offset); pane != "" {
			return c.moveAgentToNextTarget(pane)
		}
	case scriptMetadata:
		if pane := c.scriptAgent(action.offset); pane != "" {
			return c.changeAgentMetadata(pane)
		}
	}
	return "script skipped: no agent available"
}

func (c *Client) scriptAgent(offset int) string {
	if len(c.agentOrder) == 0 {
		return ""
	}
	if offset < 0 {
		return c.agentOrder[len(c.agentOrder)-1]
	}
	return c.agentOrder[offset%len(c.agentOrder)]
}

func (c *Client) removeTargetAt(index int) {
	name := c.targets[index].Name
	for _, agent := range c.states[name].agents {
		c.removeAgentFromOrder(agent.PaneID)
		delete(c.readCounts, agent.PaneID)
	}
	delete(c.states, name)
	c.targets = append(c.targets[:index], c.targets[index+1:]...)
	if c.activeTarget >= len(c.targets) {
		c.activeTarget = 0
	}
}

func (c *Client) moveAgentToNextTarget(pane string) string {
	fromIndex, agentIndex := c.findAgent(pane)
	if fromIndex < 0 || len(c.targets) < 2 {
		return "no alternate target available for agent move"
	}
	toIndex := (fromIndex + 1) % len(c.targets)
	from, to := c.targets[fromIndex].Name, c.targets[toIndex].Name
	agent := c.states[from].agents[agentIndex]
	c.states[from].agents = append(c.states[from].agents[:agentIndex], c.states[from].agents[agentIndex+1:]...)
	c.states[to].agents = append(c.states[to].agents, agent)
	return fmt.Sprintf("%s / %s moved to %s", from, pane, to)
}

func (c *Client) addAgent(status string) string {
	configured := c.targets[c.activeTarget]
	profile := demoProfileFor(configured.Name)
	c.nextAgent++
	pane := fmt.Sprintf("demo-%d", c.nextAgent)
	workspace := profile.workspaces[c.random.Intn(len(profile.workspaces))]
	tab := profile.tabs[c.random.Intn(len(profile.tabs))]
	title := profile.titles[c.random.Intn(len(profile.titles))]
	c.states[configured.Name].agents = append(c.states[configured.Name].agents, herdr.Agent{
		WorkspaceID:           "workspace-" + workspace,
		Workspace:             workspace,
		TabID:                 "workspace-" + workspace + ":tab-" + tab,
		Tab:                   tab,
		PaneID:                pane,
		TerminalID:            pane,
		Agent:                 demoAgents[c.random.Intn(len(demoAgents))],
		Status:                status,
		CWD:                   "/work/" + configured.Name + "/" + workspace,
		TerminalTitle:         title,
		TerminalTitleStripped: title,
		Revision:              1,
	})
	c.agentOrder = append(c.agentOrder, pane)
	c.activeTarget = (c.activeTarget + 1) % len(c.targets)
	return fmt.Sprintf("%s / %s entered the pasture", configured.Name, pane)
}

func (c *Client) changeAgentMetadata(pane string) string {
	targetIndex, agentIndex := c.findAgent(pane)
	if targetIndex < 0 {
		return "no agent available to update"
	}
	agent := &c.states[c.targets[targetIndex].Name].agents[agentIndex]
	profile := demoProfileFor(c.targets[targetIndex].Name)
	switch c.random.Intn(3) {
	case 0:
		agent.Workspace = differentValue(c.random, profile.workspaces, agent.Workspace)
		agent.WorkspaceID = "workspace-" + agent.Workspace
		agent.CWD = "/work/" + c.targets[targetIndex].Name + "/" + agent.Workspace
		agent.TabID = agent.WorkspaceID + ":tab-" + agent.Tab
	case 1:
		agent.Tab = differentValue(c.random, profile.tabs, agent.Tab)
		agent.TabID = agent.WorkspaceID + ":tab-" + agent.Tab
	default:
		agent.TerminalTitle = differentValue(c.random, profile.titles, agent.TerminalTitle)
		agent.TerminalTitleStripped = agent.TerminalTitle
	}
	agent.Revision++
	return fmt.Sprintf("%s / %s metadata changed", c.targets[targetIndex].Name, pane)
}

func differentValue(random *rand.Rand, values []string, current string) string {
	if len(values) < 2 {
		if len(values) == 1 {
			return values[0]
		}
		return current
	}
	index := random.Intn(len(values) - 1)
	for _, value := range values {
		if value == current {
			continue
		}
		if index == 0 {
			return value
		}
		index--
	}
	return values[0]
}

func simulatedOutput(targetName string, agent herdr.Agent, read int) string {
	profile := demoProfileFor(targetName)
	lines := []string{
		fmt.Sprintf("$ %s --workspace %s --tab %s", agent.Agent, agent.Workspace, agent.Tab),
		fmt.Sprintf("[%02d] %s", read, agent.TerminalTitle),
		fmt.Sprintf("cwd: %s", agent.CWD),
		fmt.Sprintf("status: %s", agent.Status),
	}
	for step := max(1, read-3); step <= read; step++ {
		action := profile.actions[(step-1)%len(profile.actions)]
		lines = append(lines, fmt.Sprintf("step %02d: %s", step, action))
	}
	return strings.Join(lines, "\n") + "\n"
}

func demoProfileFor(targetName string) demoProfile {
	if profile, exists := demoProfiles[targetName]; exists {
		return profile
	}
	return fallbackDemoProfile
}

func (c *Client) findAgent(pane string) (int, int) {
	for targetIndex, configured := range c.targets {
		for agentIndex, agent := range c.states[configured.Name].agents {
			if agent.PaneID == pane {
				return targetIndex, agentIndex
			}
		}
	}
	return -1, -1
}

func (c *Client) targetIndex(name string) int {
	for index, configured := range c.targets {
		if configured.Name == name {
			return index
		}
	}
	return -1
}

func (c *Client) removeAgentFromOrder(pane string) {
	for index, candidate := range c.agentOrder {
		if candidate == pane {
			c.agentOrder = append(c.agentOrder[:index], c.agentOrder[index+1:]...)
			return
		}
	}
}
