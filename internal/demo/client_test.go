package demo

import (
	"context"
	"strings"
	"testing"

	"github.com/mjrusso/herdlord/internal/target"
)

func TestClientStartsWithEveryAgentState(t *testing.T) {
	client := New()
	states := make(map[string]int)
	for _, configured := range client.Targets() {
		agents, err := client.Snapshot(context.Background(), configured, "demo")
		if err != nil {
			t.Fatal(err)
		}
		for _, agent := range agents {
			states[agent.Status]++
		}
	}
	for _, state := range []string{"idle", "working", "blocked", "done"} {
		if states[state] < 2 {
			t.Fatalf("initial simulation has %d %s agents, want at least 2: %v", states[state], state, states)
		}
	}
}

func TestClientPopulatesWorkspaceAndTabNames(t *testing.T) {
	client := New()
	for range 20 {
		client.addAgent("idle")
	}

	identities := make(map[string]bool)
	agentKinds := make(map[string]bool)
	for _, configured := range client.Targets() {
		agents, err := client.Snapshot(context.Background(), configured, "demo")
		if err != nil {
			t.Fatal(err)
		}
		for _, agent := range agents {
			if agent.Workspace == "" || agent.WorkspaceID == "" || agent.Tab == "" || agent.TabID == "" || agent.CWD == "" {
				t.Fatalf("agent has incomplete simulated identity: %#v", agent)
			}
			identities[agent.Workspace+"/"+agent.Tab] = true
			agentKinds[agent.Agent] = true
		}
	}
	if len(identities) < 2 {
		t.Fatalf("simulated identities do not vary: %v", identities)
	}
	if len(agentKinds) < 2 {
		t.Fatalf("simulated agent implementations do not vary: %v", agentKinds)
	}
}

func TestDefaultTargetsUseThemedProfiles(t *testing.T) {
	client := New()
	for _, configured := range client.Targets() {
		profile := demoProfileFor(configured.Name)
		agents, err := client.Snapshot(context.Background(), configured, "demo")
		if err != nil {
			t.Fatal(err)
		}
		if len(agents) == 0 {
			t.Fatalf("target %q has no agents", configured.Name)
		}
		for _, agent := range agents {
			if !contains(profile.workspaces, agent.Workspace) || !contains(profile.tabs, agent.Tab) || !contains(profile.titles, agent.Title()) {
				t.Fatalf("target %q has agent outside its profile: %#v", configured.Name, agent)
			}
			if !strings.HasPrefix(agent.CWD, "/work/"+configured.Name+"/") {
				t.Fatalf("target %q has unrelated directory %q", configured.Name, agent.CWD)
			}
		}
		output, err := client.Read(context.Background(), configured, "demo", agents[0].PaneID, 120)
		if err != nil {
			t.Fatal(err)
		}
		if !containsSubstring(profile.actions, output) {
			t.Fatalf("target %q output is not themed:\n%s", configured.Name, output)
		}
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, text string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func TestClientReturnsEvolvingOutputAndIndependentFailures(t *testing.T) {
	client := New()
	configured := client.Targets()[0]

	first, err := client.Read(context.Background(), configured, "demo", "demo-1", 120)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Read(context.Background(), configured, "demo", "demo-1", 120)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(second, "status: idle") {
		t.Fatalf("output did not evolve:\n%s\n%s", first, second)
	}
	for read := 3; read <= 6; read++ {
		if _, err := client.Read(context.Background(), configured, "demo", "demo-1", 120); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := client.Read(context.Background(), configured, "demo", "demo-1", 120); err != nil || output != "" {
		t.Fatalf("seventh read = %q, %v; want empty output", output, err)
	}
	for read := 8; read <= 10; read++ {
		if _, err := client.Read(context.Background(), configured, "demo", "demo-1", 120); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Read(context.Background(), configured, "demo", "demo-1", 120); err == nil || !strings.Contains(err.Error(), "terminal read failed") {
		t.Fatalf("eleventh read error = %v", err)
	}
}

func TestClientReadDoesNotCrossTargetBoundaries(t *testing.T) {
	client := New()
	targets := client.Targets()
	agents, err := client.Snapshot(context.Background(), targets[0], "demo")
	if err != nil || len(agents) == 0 {
		t.Fatalf("first target agents = %#v, %v", agents, err)
	}
	output, err := client.Read(context.Background(), targets[1], "demo", agents[0].PaneID, 120)
	if err == nil || output != "" {
		t.Fatalf("cross-target read = %q, %v; want an unavailable-agent error", output, err)
	}
}

func TestClientChangesAgentMetadata(t *testing.T) {
	client := New()
	configured := client.Targets()[0]
	before, err := client.Snapshot(context.Background(), configured, "demo")
	if err != nil {
		t.Fatal(err)
	}
	client.changeAgentMetadata("demo-1")
	after, err := client.Snapshot(context.Background(), configured, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Revision != before[0].Revision+1 {
		t.Fatalf("metadata revision = %d, want %d", after[0].Revision, before[0].Revision+1)
	}
	if after[0].Workspace == before[0].Workspace && after[0].Tab == before[0].Tab && after[0].TerminalTitle == before[0].TerminalTitle {
		t.Fatalf("metadata did not change: before=%#v after=%#v", before[0], after[0])
	}
}

func TestDemoSequenceIsDeterministic(t *testing.T) {
	first, second := New(), New()
	for step := 0; step < 100; step++ {
		if a, b := first.ScriptStep(), second.ScriptStep(); a != b {
			t.Fatalf("script step %d differs for the same seed: %v != %v", step, a, b)
		}
	}
}

func TestScriptedSimulationFollowsDefaultSequence(t *testing.T) {
	client := New()
	wants := []string{"demo-1", "demo-2", "entered the pasture", "demo-9", "metadata changed", "moved to", "demo-5"}
	for step, want := range wants {
		if event := client.ScriptStep(); !strings.Contains(event, want) {
			t.Fatalf("script step %d = %q, want %q", step, event, want)
		}
	}
}

func TestClientTargetCRUDPreservesSimulationState(t *testing.T) {
	client := New()
	configured := target.Target{Name: "workbox", Prefix: []string{"ssh", "workbox", "--"}}
	if err := client.AddTarget(configured); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateTarget("workbox", target.Target{Name: "renamed"}); err != nil {
		t.Fatal(err)
	}
	if err := client.ToggleTargetPaused("renamed"); err != nil {
		t.Fatal(err)
	}
	targets := client.Targets()
	if targets[len(targets)-1].Name != "renamed" || !targets[len(targets)-1].Paused {
		t.Fatalf("updated target = %#v", targets[len(targets)-1])
	}
	if err := client.RemoveTarget("renamed"); err != nil {
		t.Fatal(err)
	}
	if client.targetIndex("renamed") >= 0 {
		t.Fatal("removed target remains configured")
	}
}
