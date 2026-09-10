package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mjrusso/herdlord/internal/demo"
)

type session struct {
	store targetStore
	demo  *demoDriver
}

type demoDriver struct {
	client     *demo.Client
	running    bool
	generation uint64
}

type demoTickMsg struct {
	generation uint64
}

func liveSession(store targetStore) session {
	return session{store: store}
}

func demoSession(client *demo.Client) session {
	return session{
		store: demoTargetStore{client: client},
		demo:  &demoDriver{client: client},
	}
}

func (s session) isDemo() bool { return s.demo != nil }

func (s session) demoRunning() bool {
	return s.demo != nil && s.demo.running
}

func (s session) demoGeneration() uint64 {
	if s.demo == nil {
		return 0
	}
	return s.demo.generation
}

func (s *session) toggleDemo() (running, ok bool) {
	if s.demo == nil {
		return false, false
	}
	s.demo.generation++
	s.demo.running = !s.demo.running
	return s.demo.running, true
}

func (s *session) applyDemoTick(msg demoTickMsg) (message string, ok bool) {
	if s.demo == nil || !s.demo.running || msg.generation != s.demo.generation {
		return "", false
	}
	return s.demo.client.ScriptStep(), true
}

func demoTick(generation uint64) tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
		return demoTickMsg{generation: generation}
	})
}
