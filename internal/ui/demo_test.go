package ui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/pasture"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func TestDemoStartsPausedAndCanBeRun(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	if m.session.demoRunning() || !strings.Contains(m.activityView(), "DEMO · PAUSED") || !strings.Contains(m.activity[0], "demo ready") || !strings.Contains(testFooterView(m), "p start") {
		t.Fatalf("demo did not start paused: running=%v activity=%q", m.session.demoRunning(), ansi.Strip(m.activityView()))
	}
	if strings.Contains(m.helpOverlay(), "Refresh targets") {
		t.Fatal("demo help advertises disabled refresh action")
	}
	refresh := make(chan struct{}, 1)
	m.pollers["agi"] = poller{refresh: refresh}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if len(refresh) != 0 || len(m.refreshPending) != 0 {
		t.Fatal("demo refresh key triggered a refresh")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if !m.session.demoRunning() || cmd == nil || !strings.Contains(m.activityView(), "DEMO · RUNNING") || !strings.Contains(testFooterView(m), "p pause") {
		t.Fatalf("start failed: running=%v cmd=%v footer=%q", m.session.demoRunning(), cmd != nil, ansi.Strip(testFooterView(m)))
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if m.session.demoRunning() || cmd != nil || !strings.Contains(testFooterView(m), "p start") {
		t.Fatalf("pause failed: running=%v cmd=%v footer=%q", m.session.demoRunning(), cmd != nil, ansi.Strip(testFooterView(m)))
	}
}

func TestDemoAllowsInspectorAndOutputButNotAttach(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	m.pasture.Close()
	m.statuses[m.targets[0].Name] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "demo-pane", Agent: "codex", Status: "idle"}}}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	footer := ansi.Strip(testFooterView(m))
	if !strings.Contains(footer, "o output") || !strings.Contains(footer, "i inspect") || strings.Contains(footer, "attach") {
		t.Fatalf("demo agent controls = %q", footer)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !m.showInspector {
		t.Fatal("demo inspector did not open")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if m.overlay.kind != overlayOutput {
		t.Fatal("demo output panel did not open")
	}
	m.overlay = overlayState{}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay.kind == overlayAttach {
		t.Fatal("demo opened attach confirmation")
	}
}

func TestSeededUIStateTransitionsRemainConsistent(t *testing.T) {
	t.Setenv("HERDLORD_KITTY", "1")
	m := newASCIIDemo(time.Second, time.Second)
	client := m.session.demo.client
	random := rand.New(rand.NewSource(99173))
	keys := []string{"p", "v", "g", "R", "i", "?", "esc", "up", "down", "t", "d", "q", " "}
	states := []string{"idle", "working", "blocked", "done", "unknown"}
	nextTarget := 0

	for step := range 1_000 {
		switch random.Intn(8) {
		case 0:
			m.Update(tea.WindowSizeMsg{Width: random.Intn(141), Height: random.Intn(61)})
		case 1:
			m.Update(pasture.TickMsg{Generation: m.pasture.Generation()})
		case 2:
			m.Update(demoTickMsg{generation: m.session.demoGeneration()})
		case 3:
			name := fmt.Sprintf("field-%d", nextTarget)
			nextTarget++
			if err := client.AddTarget(target.Target{Name: name}); err != nil {
				t.Fatal(err)
			}
			m.reconcile(client.Targets())
		case 4:
			if targets := client.Targets(); len(targets) > 0 {
				name := targets[random.Intn(len(targets))].Name
				if err := client.RemoveTarget(name); err != nil {
					t.Fatal(err)
				}
				m.reconcile(client.Targets())
			}
		case 5:
			if len(m.targets) > 0 {
				configured := m.targets[random.Intn(len(m.targets))]
				agent := herdr.Agent{PaneID: fmt.Sprintf("agent-%d", step), Status: states[random.Intn(len(states))]}
				sendPollStatus(m, configured.Name, poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{agent}})
			}
		default:
			key := keys[random.Intn(len(keys))]
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		}
		_ = m.View()
		assertUIStateConsistent(t, m, step)
	}
}

func assertUIStateConsistent(t *testing.T, m *Model, step int) {
	t.Helper()
	targets := make(map[string]bool, len(m.targets))
	for _, configured := range m.targets {
		targets[configured.Name] = true
	}
	for name := range m.statuses {
		if !targets[name] {
			t.Fatalf("step %d: status retained removed target %q", step, name)
		}
	}
	if (m.overlay.kind == overlayDelete || m.overlay.kind == overlayAttach) && !targets[m.overlay.target] {
		t.Fatalf("step %d: overlay %d retained removed target %q", step, m.overlay.kind, m.overlay.target)
	}
	if m.overlay.kind == overlayEdit && !targets[m.editTarget] {
		t.Fatalf("step %d: edit overlay retained removed target %q", step, m.editTarget)
	}
	if len(m.targets) == 0 {
		if m.targetCursor != 0 {
			t.Fatalf("step %d: empty target cursor = %d", step, m.targetCursor)
		}
	} else if m.targetCursor < 0 || m.targetCursor >= len(m.targets) {
		t.Fatalf("step %d: target cursor %d outside %d targets", step, m.targetCursor, len(m.targets))
	}
}

func TestDemoTickRecordsEventAndContinues(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	before := m.activitySequence
	_, cmd := m.Update(demoTickMsg{generation: m.session.demoGeneration()})
	if m.activitySequence != before+1 || cmd == nil {
		t.Fatalf("automatic tick: sequence=%d, want %d; scheduled=%v", m.activitySequence, before+1, cmd != nil)
	}
}

func TestDemoIgnoresTicksFromBeforePause(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	staleGeneration := m.session.demoGeneration()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})

	activityCount := len(m.activity)
	_, cmd := m.Update(demoTickMsg{generation: staleGeneration})
	if cmd != nil || len(m.activity) != activityCount {
		t.Fatalf("stale tick was not ignored: cmd=%v activity=%v", cmd != nil, m.activity)
	}
}

func TestPauseAndResumePreserveScriptPosition(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m.Update(demoTickMsg{generation: m.session.demoGeneration()})
	if !strings.Contains(m.activity[len(m.activity)-1], "demo-1") {
		t.Fatalf("first scripted event = %q", m.activity[len(m.activity)-1])
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m.Update(demoTickMsg{generation: m.session.demoGeneration()})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m.Update(demoTickMsg{generation: m.session.demoGeneration()})
	if !strings.Contains(m.activity[len(m.activity)-1], "demo-2") {
		t.Fatalf("resumed scripted event = %q", m.activity[len(m.activity)-1])
	}
}

func TestActivityRailHasStableHeight(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	before := strings.Count(m.activityView(), "\n")
	for range 5 {
		m.recordActivity("an event occurred")
	}
	if after := strings.Count(m.activityView(), "\n"); after != before || after != 2 {
		t.Fatalf("activity rail lines changed from %d to %d", before+1, after+1)
	}
	if len(m.activity) != 6 {
		t.Fatalf("activity rail retained %d events, want 6", len(m.activity))
	}
}

func TestDemoPastureFitsExactCompactTerminal(t *testing.T) {
	for _, test := range []struct {
		name     string
		renderer pasture.Renderer
	}{{name: "ascii", renderer: pasture.ASCII}, {name: "kitty", renderer: pasture.Kitty}} {
		t.Run(test.name, func(t *testing.T) {
			m := NewDemo(time.Second, time.Second, test.renderer)
			client := m.session.demo.client
			for _, configured := range m.targets {
				agents, err := client.Snapshot(t.Context(), configured, "")
				if err != nil {
					t.Fatal(err)
				}
				m.statuses[configured.Name] = poll.TargetStatus{State: poll.OK, Agents: agents}
			}
			m.Update(tea.WindowSizeMsg{Width: 141, Height: 43})
			if rendered := strings.Count(testDashboardView(m), "\n") + 1; rendered > m.height {
				t.Fatalf("43x141 terminal rendered %d rows; Bubble Tea discards %d rows from the top", rendered, rendered-m.height)
			}
		})
	}
}

func TestDemoPastureClosesOnShortTerminal(t *testing.T) {
	for _, test := range []struct {
		name     string
		renderer pasture.Renderer
	}{{name: "ascii", renderer: pasture.ASCII}, {name: "kitty", renderer: pasture.Kitty}} {
		t.Run(test.name, func(t *testing.T) {
			m := NewDemo(time.Second, time.Second, test.renderer)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
			if m.pasture.Visible() {
				t.Fatal("demo pasture remained open in a terminal that is too short")
			}
			if !strings.Contains(m.message, "too short") {
				t.Fatalf("notice = %q, want a terminal-height explanation", m.message)
			}
			if rendered := renderedHeight(m.View()); rendered > m.height {
				t.Fatalf("16x80 terminal rendered %d rows", rendered)
			}
		})
	}
}

func TestDemoUsesTargetManagerForTargetCRUD(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	client := m.session.demo.client
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if m.overlay.kind != overlayTargets {
		t.Fatalf("t opened overlay %q, want target manager", m.overlay.kind)
	}

	m.openAddForm()
	m.addInputs[0].SetValue("workbox")
	_, cmd := m.finishAdd()
	if cmd == nil {
		t.Fatal("demo target add did not validate through the shared save flow")
	}
	m.Update(cmd())
	if m.targetIndex("workbox") < 0 || m.overlay.kind != overlayTargets {
		t.Fatalf("demo target was not added after validation: targets=%v overlay=%q", m.targets, m.overlay.kind)
	}

	m.targetCursor = m.targetIndex("workbox")
	m.toggleFocused()
	configured, _ := m.findTarget("workbox")
	if !configured.Paused {
		t.Fatal("demo target was not paused")
	}

	if err := client.RemoveTarget("workbox"); err != nil {
		t.Fatal(err)
	}
	m.reconcile(client.Targets())
	if m.targetIndex("workbox") >= 0 {
		t.Fatal("demo target was not removed")
	}
}
