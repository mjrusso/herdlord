package ui

import (
	"context"
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func (m *Model) start(t target.Target) {
	if t.Paused {
		return
	}
	m.generations[t.Name]++
	generation := m.generations[t.Name]
	if m.program == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan struct{}, 1)
	m.cancels[t.Name], m.refresh[t.Name] = cancel, ch
	initial := m.statuses[t.Name]
	go m.manager.RunFrom(ctx, t, initial, pollSender{program: m.program, generation: generation}, ch)
}

func (m *Model) stop(name string) {
	m.updateRefreshProgress(name)
	m.generations[name]++
	if cancel := m.cancels[name]; cancel != nil {
		cancel()
	}
	delete(m.cancels, name)
	delete(m.refresh, name)
}

func (m *Model) watchConfig() tea.Cmd {
	path := m.configPath
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		targets, err := target.Load(path)
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
		if exists && reflect.DeepEqual(previous, configured) {
			continue
		}
		if exists {
			m.stop(configured.Name)
			m.clearTargetOutputs(configured.Name)
		}
		previousStatus := m.statuses[configured.Name]
		if !exists || !samePollingTarget(previous, configured) {
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
	for name := range old {
		if _, exists := next[name]; exists {
			continue
		}
		m.stop(name)
		delete(m.statuses, name)
		m.clearTargetOutputs(name)
	}
	m.targets = append([]target.Target(nil), latest...)
	m.targetCursor = min(m.targetCursor, max(0, len(m.targets)-1))
	m.rebuildRows()
	m.syncFocusedOutput()
}

func samePollingTarget(a, b target.Target) bool {
	a.Paused, b.Paused = false, false
	return reflect.DeepEqual(a, b)
}

func (m *Model) clearTargetOutputs(name string) {
	for key := range m.outputs {
		if strings.HasPrefix(key, name+"\x00") {
			delete(m.outputs, key)
		}
	}
	for key := range m.inflight {
		if strings.HasPrefix(key, name+"\x00") {
			delete(m.inflight, key)
		}
	}
}

func (m *Model) stopAll() {
	for name := range m.cancels {
		m.stop(name)
	}
}
