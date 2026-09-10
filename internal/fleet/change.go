package fleet

import (
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
)

type AgentKey struct {
	Target string
	Pane   string
}

func NewAgentKey(targetName, paneID string) AgentKey {
	return AgentKey{Target: targetName, Pane: paneID}
}

func (k AgentKey) TargetName() string { return k.Target }

type AgentChangeKind uint8

const (
	AgentAppeared AgentChangeKind = iota
	AgentStatusChanged
	AgentDisappeared
)

type AgentChange struct {
	Kind     AgentChangeKind
	Previous herdr.Agent
	Next     herdr.Agent
}

type StatusChange struct {
	Previous        poll.TargetStatus
	Next            poll.TargetStatus
	StateTransition bool
	Agents          []AgentChange
}

func DiffStatus(previous, next poll.TargetStatus, exists bool) StatusChange {
	change := StatusChange{Previous: previous, Next: next}
	change.StateTransition = exists && previous.State != poll.Checking && previous.State != next.State &&
		(previous.State != poll.BackingOff || next.State.Usable())
	if !exists || !previous.State.Usable() || !next.State.Usable() {
		return change
	}
	old := make(map[string]herdr.Agent, len(previous.Agents))
	for _, agent := range previous.Agents {
		old[agent.PaneID] = agent
	}
	current := make(map[string]bool, len(next.Agents))
	for _, agent := range next.Agents {
		current[agent.PaneID] = true
		before, found := old[agent.PaneID]
		switch {
		case !found:
			change.Agents = append(change.Agents, AgentChange{Kind: AgentAppeared, Next: agent})
		case before.Status != agent.Status:
			change.Agents = append(change.Agents, AgentChange{Kind: AgentStatusChanged, Previous: before, Next: agent})
		}
	}
	for _, agent := range previous.Agents {
		if !current[agent.PaneID] {
			change.Agents = append(change.Agents, AgentChange{Kind: AgentDisappeared, Previous: agent})
		}
	}
	return change
}
