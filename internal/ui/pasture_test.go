package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/pasture"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func TestUnavailableKittyRendererBecomesNotice(t *testing.T) {
	t.Setenv("HERDLORD_RENDERER", "ascii")
	t.Setenv("HERDLORD_KITTY", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-256color")
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 50})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if !strings.Contains(m.message, "Kitty graphics are not available") {
		t.Fatalf("renderer notice = %q", m.message)
	}
}

func TestKittyUploadPersistsAcrossRepeatedViews(t *testing.T) {
	m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, pasture.Kitty)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if first := m.View(); !strings.Contains(first, "\x1b_Ga=t") {
		t.Fatal("first pasture view omitted Kitty setup")
	}
	if second := m.View(); !strings.Contains(second, "\x1b_Ga=t") {
		t.Fatal("second pasture view discarded Kitty setup before a flush interval")
	}
}

func TestPastureDeclinesShortTerminal(t *testing.T) {
	for _, test := range []struct {
		name     string
		renderer pasture.Renderer
	}{{name: "ascii", renderer: pasture.ASCII}, {name: "kitty", renderer: pasture.Kitty}} {
		t.Run(test.name, func(t *testing.T) {
			m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, test.renderer)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
			if m.pasture.Visible() {
				t.Fatal("pasture opened in a terminal that is too short")
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

func TestPastureDeclinesNarrowTerminal(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 50})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if m.pasture.Visible() {
		t.Fatal("pasture opened in a terminal that is too narrow")
	}
	if !strings.Contains(m.message, "too narrow") {
		t.Fatalf("notice = %q, want a terminal-width explanation", m.message)
	}
}

func TestPastureRequiresRoomForTargetPen(t *testing.T) {
	for _, test := range []struct {
		name     string
		renderer pasture.Renderer
	}{{name: "ascii", renderer: pasture.ASCII}, {name: "kitty", renderer: pasture.Kitty}} {
		t.Run(test.name, func(t *testing.T) {
			m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, test.renderer)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 39})
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
			if m.pasture.Visible() {
				t.Fatal("pasture opened without room for a target pen")
			}
			if !strings.Contains(m.message, "too short") {
				t.Fatalf("notice = %q, want a terminal-height explanation", m.message)
			}
		})
	}
}

func TestPastureOpensWithRoomForTargetPen(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if !m.pasture.Visible() || !strings.Contains(m.View(), "+- local -+") {
		t.Fatal("pasture did not open and render its target pen")
	}
}

func TestInspectorIsHiddenWhilePastureIsVisible(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "idle"}}}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !strings.Contains(m.View(), "Agent inspector") {
		t.Fatal("test setup did not open the inspector in the table")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if !m.pasture.Visible() || strings.Contains(m.View(), "Agent inspector") {
		t.Fatal("inspector rendered under the pasture")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !m.showInspector || strings.Contains(m.View(), "Agent inspector") {
		t.Fatal("i acted on the inspector under the pasture")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if m.pasture.Visible() || !strings.Contains(m.View(), "Agent inspector") {
		t.Fatal("inspector did not return with the table")
	}
}

func TestPastureIgnoresTickFromBeforeReopen(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.openPasture()
	stale := m.pasture.Generation()
	m.pasture.Close()
	m.openPasture()
	if _, cmd := m.Update(pasture.TickMsg{Generation: stale}); cmd != nil {
		t.Fatal("stale pasture tick scheduled another tick")
	}
	if _, cmd := m.Update(pasture.TickMsg{Generation: m.pasture.Generation()}); cmd == nil {
		t.Fatal("current pasture tick did not schedule another tick")
	}
}

func TestPastureTickAdvancesOnlyWhileVisible(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.openPasture()
	if _, cmd := m.Update(pasture.TickMsg{Generation: m.pasture.Generation()}); cmd == nil {
		t.Fatal("visible pasture did not schedule another tick")
	}
	m.pasture.Close()
	if _, cmd := m.Update(pasture.TickMsg{Generation: m.pasture.Generation()}); cmd != nil {
		t.Fatal("hidden pasture scheduled another tick")
	}
}

func TestPastureFallsBackToASCII(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	view := m.View()
	if !m.pasture.Visible() || !strings.Contains(view, "+- local -+") || strings.Contains(view, "\x1b_G") {
		t.Fatalf("ASCII fallback was not rendered: pasture=%v", m.pasture.Visible())
	}
}

func TestPastureRendererKeyPreservesHerd(t *testing.T) {
	m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, pasture.Kitty)
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "idle"}}}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	_ = m.View()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	view := m.View()
	if strings.Contains(view, "\x1b_Ga=p") || !strings.Contains(view, "(____)") {
		t.Fatal("renderer key did not select ASCII with the existing herd")
	}
}

func TestWindowSizeMessageSyncsPastureState(t *testing.T) {
	targets := []target.Target{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	statuses := map[string]poll.TargetStatus{
		"a": {State: poll.OK, Agents: []herdr.Agent{{PaneID: "a1", Status: "idle"}}},
		"b": {State: poll.OK, Agents: []herdr.Agent{{PaneID: "b1", Status: "idle"}}},
		"c": {State: poll.OK, Agents: []herdr.Agent{{PaneID: "c1", Status: "idle"}}},
	}
	m := newASCIIModel(targets, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.statuses = statuses
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 70})
	m.openPasture()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	actual := testPastureDisplay(m).View

	fresh := newASCIIModel(targets, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	fresh.statuses = statuses
	fresh.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	fresh.openPasture()
	expected := testPastureDisplay(fresh).View
	if actual != expected {
		t.Fatal("resized pasture differs from a pasture opened at the new size")
	}
}

func TestPastureViewUsesRealTargetsAndAgents(t *testing.T) {
	m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, pasture.Kitty)
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "codex", Status: "working"},
		{PaneID: "p2", Agent: "claude", Status: "blocked"},
	}}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	view := m.View()
	if !strings.Contains(view, "local") || !strings.Contains(view, "\x1b_Ga=t") {
		t.Fatal("pasture view did not render its target and initialize Kitty")
	}
	if strings.Contains(view, "claude") || strings.Contains(view, "codex") || strings.Contains(view, "Selected") {
		t.Fatal("pasture view rendered table selection details")
	}
	cursor := m.table.Cursor()
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.table.Cursor() != cursor || m.overlay.kind != overlayNone || strings.Contains(testFooterView(m), "attach") {
		t.Fatal("pasture view exposed agent selection behavior")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if m.pasture.Visible() || !strings.Contains(m.View(), "a=d,d=I") {
		t.Fatal("switching to the table did not delete Kitty resources")
	}
}

func TestViewDoesNotAdvanceKittyRendererState(t *testing.T) {
	m := NewDemo(time.Second, time.Second, pasture.Kitty)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	first, second := m.View(), m.View()
	if first != second {
		t.Fatal("View changed Kitty output without a model update")
	}
	if !strings.Contains(first, "\x1b_Ga=t") {
		t.Fatal("repeated View calls discarded pending Kitty setup")
	}
}

func TestModalClearsPastureGraphicsWithoutReplayingThem(t *testing.T) {
	// A terminal too narrow for the panel falls back to an unframed modal, which still has to clear the sprites.
	for _, width := range []int{100, 40} {
		m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, pasture.Kitty)
		m.statuses["local"] = poll.TargetStatus{State: poll.OK}
		m.Update(tea.WindowSizeMsg{Width: width, Height: 50})
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
		_ = m.View()
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		view := m.View()
		if !strings.Contains(view, "\x1b_Ga=d,d=i") || strings.Contains(view, "\x1b_Ga=p,i") {
			t.Fatalf("modal at width %d did not clear Kitty placements cleanly", width)
		}
	}
}

func TestDemoSeedsPastureHeightBeforeFirstUpdate(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	want, _ := m.measureContentHeight(0, 0)
	if got := m.frame.pasture.Viewport.Height; got != want {
		t.Fatalf("initial pasture height = %d, want %d", got, want)
	}
}

func TestRemovingTargetClearsPastureLifecycleState(t *testing.T) {
	targets := []target.Target{{Name: "local"}, {Name: "keeper"}}
	m := newASCIIModel(targets, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "idle"}}}
	m.statuses["keeper"] = poll.TargetStatus{State: poll.OK}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.openPasture()
	if view := m.View(); !strings.Contains(view, "(____)") {
		t.Fatal("test setup did not render the departing herd")
	}
	m.reconcile([]target.Target{{Name: "keeper"}})
	m.reconcile(targets)
	if view := m.View(); strings.Contains(view, "(____)") {
		t.Fatal("removed target retained its herd after being re-added")
	}
}

func TestTemporaryTargetFailurePreservesLastKnownHerd(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "idle"}}}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 50})
	m.openPasture()
	sendPollStatus(m, "local", poll.TargetStatus{State: poll.Unreachable})
	if view := m.View(); !strings.Contains(view, "(____)") {
		t.Fatal("temporary target failure hid the last known herd")
	}
}

func TestChangingTargetEndpointClearsOldPastureState(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "idle"}}}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.openPasture()
	if view := m.View(); !strings.Contains(view, "(____)") {
		t.Fatal("test setup did not render the old herd")
	}
	m.reconcile([]target.Target{{Name: "local", Prefix: []string{"ssh", "other", "--"}}})
	if view := m.View(); strings.Contains(view, "(____)") {
		t.Fatal("changed polling endpoint retained the old herd")
	}
}

func TestRemovingLastTargetClosesPastureAndQueuesCleanup(t *testing.T) {
	m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, pasture.Kitty)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.openPasture()
	_ = m.View()
	m.Update(configMsg{})
	if m.pasture.Visible() {
		t.Fatal("removing the last target left pasture mode open")
	}
	view := m.View()
	if !strings.Contains(view, "a=d,d=I") || !strings.Contains(view, "No targets configured") {
		t.Fatal("last-target removal omitted cleanup or the empty state")
	}
}

func TestPastureCleanupIsEmittedBehindTargetManager(t *testing.T) {
	m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, pasture.Kitty)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.openPasture()
	_ = m.View()
	m.overlay = overlayState{kind: overlayTargets}
	m.Update(configMsg{})
	if view := m.View(); !strings.Contains(view, "a=d,d=I") {
		t.Fatal("target manager discarded queued Kitty cleanup")
	}
	if view := m.View(); !strings.Contains(view, "a=d,d=I") {
		t.Fatal("repeated view discarded Kitty cleanup before a flush interval")
	}
}

func TestPastureUsesCompactContentHeight(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 80})
	m.openPasture()
	frame := m.measureFrame()
	if height := lipgloss.Height(m.pasture.Display(frame.pasture).View); height != frame.pasture.Viewport.Height {
		t.Fatalf("pasture height = %d, want %d", height, frame.pasture.Viewport.Height)
	}
	if height := lipgloss.Height(testDashboardView(m)); height != m.height {
		t.Fatalf("dashboard height = %d, want %d", height, m.height)
	}
}

func TestDemoTargetsFitInPastureGrid(t *testing.T) {
	m := newASCIIDemo(time.Second, time.Second)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	display := testPastureDisplay(m)
	if display.PageLabel != "1–2/3" {
		t.Fatalf("demo page label = %q", display.PageLabel)
	}
	for _, name := range []string{"agi", "rsi"} {
		if !strings.Contains(display.View, name) {
			t.Fatalf("demo pasture missing %q", name)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if view := testPastureDisplay(m).View; !strings.Contains(view, "asi") {
		t.Fatal("overflow demo target was not reachable by scrolling")
	}
}

func TestPastureNavigationScrollsOverflowRows(t *testing.T) {
	targets := make([]target.Target, 5)
	for i := range targets {
		targets[i] = target.Target{Name: fmt.Sprintf("field-%d", i)}
	}
	m := newASCIIModel(targets, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.openPasture()
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if label := testPastureDisplay(m).PageLabel; label != "3–4/5" {
		t.Fatalf("down key page label = %q", label)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if label := testPastureDisplay(m).PageLabel; label != "5–5/5" {
		t.Fatalf("end key page label = %q", label)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if label := testPastureDisplay(m).PageLabel; label != "1–2/5" {
		t.Fatalf("home key page label = %q", label)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if label := testPastureDisplay(m).PageLabel; label != "3–4/5" {
		t.Fatalf("page-down key page label = %q", label)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if label := testPastureDisplay(m).PageLabel; label != "5–5/5" {
		t.Fatalf("G key page label = %q", label)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if label := testPastureDisplay(m).PageLabel; label != "1–2/5" {
		t.Fatalf("g key page label = %q", label)
	}
	if view := m.View(); !strings.Contains(view, "+- field-0 -+") {
		t.Fatal("g key switched the renderer instead of scrolling")
	}
}

func TestPastureSupportsHalfPageNavigation(t *testing.T) {
	targets := make([]target.Target, 10)
	for i := range targets {
		targets[i] = target.Target{Name: fmt.Sprintf("field-%d", i)}
	}
	m := newASCIIModel(targets, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 70})
	m.openPasture()
	first := testPastureDisplay(m).PageLabel
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if next := testPastureDisplay(m).PageLabel; next == first {
		t.Fatal("Ctrl-D did not scroll the pasture")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if back := testPastureDisplay(m).PageLabel; back != first {
		t.Fatalf("Ctrl-U returned to %q, want %q", back, first)
	}
	if help := m.helpOverlay(); !strings.Contains(help, "C-u") || !strings.Contains(help, "C-d") {
		t.Fatal("pasture help omitted half-page navigation")
	}
}

func TestQuitKeysCloseKittyPasture(t *testing.T) {
	for name, msg := range map[string]tea.KeyMsg{
		"q":      {Type: tea.KeyRunes, Runes: []rune{'q'}},
		"Ctrl-C": {Type: tea.KeyCtrlC},
	} {
		t.Run(name, func(t *testing.T) {
			m := New([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{}, pasture.Kitty)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
			m.openPasture()
			_, cmd := m.Update(msg)
			if cmd == nil {
				t.Fatal("quit did not return a command")
			}
			if m.pasture.Visible() || !strings.Contains(m.View(), "a=d,d=I") {
				t.Fatal("quit did not close and purge the Kitty pasture")
			}
		})
	}
}

func TestPastureDefersHiddenTableRendering(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "idle"}}}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
	m.openPasture()
	beforeRows := fmt.Sprint(m.table.Rows())
	beforeHeight := m.table.Height()
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "blocked"}}}
	m.rebuildRows()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if got := fmt.Sprint(m.table.Rows()); got != beforeRows {
		t.Fatal("hidden table rows were rendered while pasture was visible")
	}
	if got := m.table.Height(); got != beforeHeight {
		t.Fatal("hidden table height was updated while pasture was visible")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if got := fmt.Sprint(m.table.Rows()); got == beforeRows || !strings.Contains(got, "needs input") {
		t.Fatal("deferred table rows were not rendered after leaving the pasture")
	}
}

func deferredTableModel(t *testing.T, agents []herdr.Agent) *Model {
	t.Helper()
	m := newASCIIModel([]target.Target{{Name: "local"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: agents}
	m.rebuildRows()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
	return m
}

func TestDeferredTableKeepsFocusedAgentAcrossReorder(t *testing.T) {
	m := deferredTableModel(t, []herdr.Agent{
		{PaneID: "p1", Agent: "aaa", Status: "idle"},
		{PaneID: "p2", Agent: "bbb", Status: "idle"},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.focused(); got == nil || got.agent.PaneID != "p2" {
		t.Fatalf("test setup did not focus p2: %+v", got)
	}
	m.openPasture()
	// Blocked sorts ahead of idle, so p2 moves to the first row while the table is hidden.
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "aaa", Status: "idle"},
		{PaneID: "p2", Agent: "bbb", Status: "blocked"},
	}}
	m.rebuildRows()
	// A second hidden rebuild re-reads the focus identity, so the tracked row has to survive the first one.
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "aaa", Status: "working"},
		{PaneID: "p2", Agent: "bbb", Status: "blocked"},
	}}
	m.rebuildRows()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if got := m.focused(); got == nil || got.agent.PaneID != "p2" {
		t.Fatalf("focus drifted off p2: %+v", got)
	}
}

func TestDeferredTableMarksCursorAfterHerdShrinks(t *testing.T) {
	m := deferredTableModel(t, []herdr.Agent{
		{PaneID: "p1", Agent: "aaa", Status: "idle"},
		{PaneID: "p2", Agent: "bbb", Status: "idle"},
		{PaneID: "p3", Agent: "ccc", Status: "idle"},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.openPasture()
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "aaa", Status: "idle"}}}
	m.rebuildRows()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	marked := 0
	for _, values := range m.table.Rows() {
		if len(values) > 0 && strings.HasPrefix(values[0], ">") {
			marked++
		}
	}
	if marked != 1 {
		t.Fatalf("%d rows carry the cursor marker, want 1 (cursor %d of %d rows)", marked, m.table.Cursor(), len(m.table.Rows()))
	}
}
