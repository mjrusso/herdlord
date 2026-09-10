package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func (m *Model) start(t target.Target) {
	if t.Paused {
		return
	}
	m.pollerGeneration++
	current := poller{generation: m.pollerGeneration}
	generation := current.generation
	if m.program == nil {
		m.pollers[t.Name] = current
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan struct{}, 1)
	current.cancel, current.refresh = cancel, ch
	m.pollers[t.Name] = current
	initial := m.statuses[t.Name]
	go m.manager.RunFrom(ctx, t, initial, pollSender{program: m.program, generation: generation}, ch)
}

func (m *Model) stop(name string) {
	m.updateRefreshProgress(name)
	current, exists := m.pollers[name]
	if !exists {
		return
	}
	if current.cancel != nil {
		current.cancel()
	}
	delete(m.pollers, name)
}

func (m *Model) watchConfig() tea.Cmd {
	store := m.session.store
	if !store.watchable() {
		return nil
	}
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		targets, err := store.load()
		return configMsg{targets: targets, err: err}
	})
}

func (m *Model) reconcile(latest []target.Target) {
	old := make(map[string]target.Target, len(m.targets))
	for _, configured := range m.targets {
		old[configured.Name] = configured
	}
	next := make(map[string]target.Target, len(latest))
	for _, configured := range latest {
		next[configured.Name] = configured
		previous, exists := old[configured.Name]
		samePoller := exists && previous.SamePollingIdentity(configured)
		switch {
		case !exists || !samePoller:
			m.recordActivity(targetChangeActivity(previous, configured, exists))
			if exists {
				m.stop(configured.Name)
				m.clearTargetOutputs(configured.Name)
				m.pasture.ClearTarget(configured.Name)
			}
		case previous.Paused != configured.Paused:
			m.recordActivity(targetChangeActivity(previous, configured, true))
			m.stop(configured.Name)
			m.clearTargetOutputs(configured.Name)
		case !previous.SameInteractiveIdentity(configured):
			m.recordActivity(targetChangeActivity(previous, configured, true))
			continue
		default:
			continue
		}
		previousStatus := m.statuses[configured.Name]
		if !samePoller {
			previousStatus = poll.TargetStatus{}
		}
		delete(m.statuses, configured.Name)
		if configured.Paused {
			m.statuses[configured.Name] = poll.TargetStatus{State: poll.Paused, LastSuccess: previousStatus.LastSuccess}
		} else {
			m.statuses[configured.Name] = poll.TargetStatus{State: poll.Checking, LastSuccess: previousStatus.LastSuccess}
			m.start(configured)
		}
	}
	for _, configured := range m.targets {
		name := configured.Name
		if _, exists := next[name]; exists {
			continue
		}
		m.stop(name)
		m.recordActivity(name + " target removed")
		delete(m.statuses, name)
		m.clearTargetOutputs(name)
		m.pasture.ClearTarget(name)
	}
	m.targets = append([]target.Target(nil), latest...)
	m.targetCursor = min(m.targetCursor, max(0, len(m.targets)-1))
	m.reconcileTargetOverlay()
	m.rebuildRows()
	m.syncFocusedOutput()
}

func (m *Model) clearTargetOutputs(name string) {
	m.deleteOutputs(func(key fleet.AgentKey) bool { return key.Target == name })
}

func (m *Model) stopAll() {
	for name, current := range m.pollers {
		if current.cancel != nil {
			m.stop(name)
		}
	}
}

func (m *Model) applyStatus(name string, status poll.TargetStatus) tea.Cmd {
	previous, exists := m.statuses[name]
	change := fleet.DiffStatus(previous, status, exists)
	if !m.session.isDemo() {
		m.recordPollActivity(name, change)
	}
	m.pasture.RecordBubbles(fleet.Snapshot{Targets: m.targets, Statuses: m.statuses}, name, change)
	m.pasture.ReconcileAgents(name, status)
	m.clearMissingAgentOutputs(name, status)
	m.statuses[name] = status
	m.updateRefreshProgress(name)
	m.rebuildRows()
	return m.readFocused()
}
