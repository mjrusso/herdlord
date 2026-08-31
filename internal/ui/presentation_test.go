package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func TestTargetStateColorsAreDistinct(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	values := []string{
		colorLabel(poll.Paused, "state"),
		colorLabel(poll.BackingOff, "state"),
		colorLabel(poll.Unreachable, "state"),
	}
	if values[0] == values[1] || values[1] == values[2] || values[0] == values[2] {
		t.Fatalf("state styles are not distinct: %#v", values)
	}
}

func TestTargetHealthShowsLastSuccessAndError(t *testing.T) {
	now := time.Now()
	if got := relativeAge(now.Add(-18*time.Second), now); got != "18s ago" {
		t.Fatalf("relative age = %q", got)
	}
	m := New([]target.Target{{Name: "workbox"}, {Name: "paused"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["workbox"] = poll.TargetStatus{State: poll.BackingOff, Err: "SSH timeout", LastSuccess: now.Add(-2 * time.Minute)}
	m.statuses["paused"] = poll.TargetStatus{State: poll.Paused}
	view := m.healthView()
	for _, want := range []string{"Target health", "TARGET", "STATE", "LAST SUCCESS", "workbox", "backing off", "2m ago", "SSH timeout"} {
		if !strings.Contains(view, want) {
			t.Fatalf("health view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "paused") {
		t.Fatalf("paused target shown as unhealthy:\n%s", view)
	}
}

func TestResponsiveAgentTableAndDetails(t *testing.T) {
	agent := herdr.Agent{WorkspaceID: "w1", Workspace: "herdlord", TabID: "w1:t1", Tab: "dashboard", PaneID: "w1:p1", Agent: "codex", Status: "working", CWD: "/project", TerminalTitle: "◑ Adding metadata", TerminalTitleStripped: "Adding metadata", Revision: 1}
	m := New([]target.Target{{Name: "workbox"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["workbox"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{agent}}

	tests := []struct {
		width int
		want  []string
	}{
		{60, []string{"", "TARGET", "WORKSPACE", "AGENT", "STATUS"}},
		{90, []string{"", "TARGET", "WORKSPACE", "TAB", "AGENT", "STATUS"}},
		{140, []string{"", "TARGET", "WORKSPACE", "TAB", "AGENT", "STATUS", "TERMINAL"}},
		{90, []string{"", "TARGET", "WORKSPACE", "TAB", "AGENT", "STATUS"}},
		{60, []string{"", "TARGET", "WORKSPACE", "AGENT", "STATUS"}},
		{140, []string{"", "TARGET", "WORKSPACE", "TAB", "AGENT", "STATUS", "TERMINAL"}},
	}
	for _, tt := range tests {
		_, _ = m.Update(tea.WindowSizeMsg{Width: tt.width, Height: 30})
		columns := m.table.Columns()
		got := make([]string, len(columns))
		for i := range columns {
			got[i] = columns[i].Title
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("width %d columns = %#v", tt.width, got)
		}
		for _, column := range columns {
			if column.Title == "STATUS" && column.Width < 14 {
				t.Fatalf("width %d status column = %d", tt.width, column.Width)
			}
		}
	}
	if got := m.rows[0].values[6]; got != "Adding metadata" {
		t.Fatalf("TERMINAL = %q", got)
	}
	details := m.detailsView()
	for _, want := range []string{"Workspace herdlord (w1)", "Tab dashboard (w1:t1)", "Pane w1:p1", "Directory /project", "Terminal Adding metadata"} {
		if !strings.Contains(details, want) {
			t.Fatalf("details missing %q:\n%s", want, details)
		}
	}
}

func TestInspectorSanitizesEveryAgentField(t *testing.T) {
	const unsafe = "\x1b[31mvalue\x1b[0m\tspoof\nnext"
	agent := herdr.Agent{
		WorkspaceID: unsafe, Workspace: unsafe, TabID: unsafe, Tab: unsafe,
		PaneID: unsafe, Agent: unsafe, Status: unsafe, CWD: "/tmp/" + unsafe,
		TerminalTitle: unsafe, TerminalTitleStripped: "safe title",
	}
	m := New([]target.Target{{Name: unsafe}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses[unsafe] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{agent}}
	_, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m.showInspector = true
	view := m.View()
	if strings.Contains(view, "\x1b[31m") || strings.Contains(view, "\tspoof") || strings.Contains(view, "\nnext") {
		t.Fatalf("inspector contains unsafe controls: %q", view)
	}
	if !strings.Contains(ansi.Strip(view), "safe title") || strings.Contains(ansi.Strip(view), "Terminal value") {
		t.Fatalf("inspector did not prefer stripped title:\n%s", ansi.Strip(view))
	}
}

func TestInspectorDefaultOffAndToggle(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Workspace: "project", Agent: "codex", Status: "idle"}}}
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.outputKey, m.output = m.focusKey(), "recent agent output"
	view := ansi.Strip(m.View())
	if m.showInspector || strings.Contains(view, "Agent inspector") || strings.Contains(view, "recent agent output") || !strings.Contains(view, "i inspect") {
		t.Fatalf("default inspector state: shown=%v\n%s", m.showInspector, view)
	}
	if strings.HasSuffix(m.View(), "\n") {
		t.Fatal("dashboard rendered an extra trailing row")
	}
	assertFooterOnLastRow(t, view, 24)
	heightWithoutDetails := m.table.Height()
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	view = ansi.Strip(m.View())
	if !m.showInspector || !strings.Contains(view, "Agent inspector  box / codex / idle") || !strings.Contains(view, "recent agent output") || !strings.Contains(view, "╭") || !strings.Contains(view, "╯") || m.table.Height() >= heightWithoutDetails {
		t.Fatalf("inspector did not open: shown=%v, table=%d/%d\n%s", m.showInspector, m.table.Height(), heightWithoutDetails, view)
	}
	if width := ansi.StringWidth(strings.Split(ansi.Strip(m.inspectorView()), "\n")[0]); width != 80 {
		t.Fatalf("inspector width = %d, want 80", width)
	}
	assertFooterOnLastRow(t, view, 24)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.showInspector || strings.Contains(ansi.Strip(m.View()), "Agent inspector") || strings.Contains(ansi.Strip(m.View()), "recent agent output") {
		t.Fatal("inspector did not close")
	}
}

func TestInspectorUsesAvailableHeightForOutput(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Workspace: "project", Agent: "codex", Status: "idle"}}}
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("output-%02d", i+1)
	}
	m.outputKey, m.output, m.showInspector = "box\x00p1", strings.Join(lines, "\n"), true
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if limit := m.outputLineLimit(); limit < 8 {
		t.Fatalf("24-line output limit = %d, want at least 8", limit)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "output-13") || !strings.Contains(view, "output-20") || strings.Contains(view, "output-12") {
		t.Fatalf("inspector output allocation is wrong:\n%s", view)
	}
	assertFooterOnLastRow(t, view, 24)

	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	if limit := m.outputLineLimit(); limit < 16 {
		t.Fatalf("32-line output limit = %d, want at least 16", limit)
	}
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	if limit := m.outputLineLimit(); limit != 36 {
		t.Fatalf("60-line output limit = %d, want 36", limit)
	}
}

func assertFooterOnLastRow(t *testing.T, view string, height int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) != height || !strings.Contains(lines[height-1], "q quit") {
		t.Fatalf("footer is not on row %d (got %d rows):\n%s", height, len(lines), view)
	}
}

func TestTableUsesAvailableWidth(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Workspace: "a long workspace name", Agent: "codex", Status: "working"}}}
	_, _ = m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	width90 := m.table.Columns()[2].Width
	_, _ = m.Update(tea.WindowSizeMsg{Width: 110, Height: 24})
	columns := m.table.Columns()
	if columns[2].Width <= width90 {
		t.Fatalf("workspace width did not grow: 90 cols=%d, 110 cols=%d", width90, columns[2].Width)
	}
	header := strings.Split(ansi.Strip(m.table.View()), "\n")[0]
	if got := ansi.StringWidth(header); got != 110 {
		t.Fatalf("table header width = %d, want 110: %q", got, header)
	}
}

func TestAgentStatusSortUsesHerdrOrder(t *testing.T) {
	statuses := []string{"unknown", "idle", "working", "done", "blocked"}
	agents := make([]herdr.Agent, len(statuses))
	for i, status := range statuses {
		agents[i] = herdr.Agent{PaneID: status, Agent: "codex", Status: status}
	}
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: agents}
	m.rebuildRows()
	got := make([]string, len(m.rows))
	for i := range m.rows {
		got[i] = m.rows[i].agent.Status
	}
	want := []string{"blocked", "done", "working", "idle", "unknown"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status order = %#v", got)
	}
}

func TestPollReorderingPreservesFocusedAgent(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "one", Status: "working"},
		{PaneID: "p2", Agent: "two", Status: "idle"},
	}}
	m.rebuildRows()
	m.table.SetCursor(1)
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "one", Status: "idle"},
		{PaneID: "p2", Agent: "two", Status: "blocked"},
	}}
	m.rebuildRows()
	if focused := m.focused(); focused == nil || focused.agent == nil || focused.agent.PaneID != "p2" {
		t.Fatalf("focus moved after sort: %#v", focused)
	}
}

func TestUncachedSelectionClearsOutputWhileLoading(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "one", Status: "working", Revision: 1},
		{PaneID: "p2", Agent: "two", Status: "idle", Revision: 1},
	}}
	m.rebuildRows()
	m.outputKey, m.output = "box\x00p1", "old output"
	m.showInspector = true
	m.table.SetCursor(1)
	if cmd := m.readFocused(); cmd == nil {
		t.Fatal("uncached pane did not schedule a read")
	}
	if m.output != "" || !m.outputLoading || !strings.Contains(m.View(), "Loading recent output…") {
		t.Fatalf("loading state = output %q, loading %v\n%s", m.output, m.outputLoading, m.View())
	}
}

func TestHelpIsModal(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK}
	m.rebuildRows()
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.overlay.kind != overlayHelp {
		t.Fatalf("help allowed background action: delete %q, help %v", m.overlay.target, (m.overlay.kind == overlayHelp))
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay.kind == overlayHelp {
		t.Fatal("Esc did not close help")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.overlay.kind == overlayHelp {
		t.Fatal("q did not close help")
	}
}

func TestCheckingStateIsNeutral(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.Checking}
	m.rebuildRows()
	if label(m.statuses["box"]) != "checking" || m.healthView() != "" || !strings.Contains(ansi.Strip(m.rows[0].values[len(m.rows[0].values)-1]), "checking") {
		t.Fatalf("checking presentation: row %#v, health %q", m.rows[0].values, m.healthView())
	}
}

func TestHealthViewCapsRowsAndErrors(t *testing.T) {
	targets := make([]target.Target, 5)
	m := New(targets, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.width, m.height = 70, 24
	for i := range targets {
		name := fmt.Sprintf("box-%d", i)
		m.targets[i].Name = name
		m.statuses[name] = poll.TargetStatus{State: poll.Unreachable, Err: strings.Repeat("transport failure ", 20)}
	}
	view := m.healthView()
	if !strings.Contains(view, "… and 4 more") || !strings.Contains(view, "…") {
		t.Fatalf("uncapped health view:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("health line width = %d: %q", ansi.StringWidth(line), line)
		}
	}
}

func TestHealthViewAdaptsToNarrowWidth(t *testing.T) {
	name := strings.Repeat("remote-", 10)
	m := New([]target.Target{{Name: name}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.width, m.height = 52, 24
	m.statuses[name] = poll.TargetStatus{State: poll.Unreachable, Err: strings.Repeat("connection refused ", 20)}
	view := m.healthView()
	if strings.Contains(view, "LAST SUCCESS") {
		t.Fatalf("narrow health view retained secondary column:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("health line width = %d: %q", ansi.StringWidth(line), line)
		}
	}
}

func TestRefreshWithoutActiveTargetsDoesNotWaitForever(t *testing.T) {
	m := New([]target.Target{{Name: "paused", Paused: true}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.refreshAll()
	if m.message != "No active targets to refresh" {
		t.Fatalf("refresh message = %q", m.message)
	}
}

func TestFooterAdaptsToWidth(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.width = 50
	if got := ansi.Strip(m.footerView()); strings.Contains(got, "attach") || !strings.Contains(got, "? help") {
		t.Fatalf("narrow footer = %q", got)
	}
	m.width = 120
	if got := ansi.Strip(m.footerView()); !strings.Contains(got, "t targets") || !strings.Contains(got, "r refresh") || strings.Contains(got, "space pause") {
		t.Fatalf("wide footer = %q", got)
	}
	if got := m.footerView(); !strings.Contains(got, "\x1b[1mt") {
		t.Fatalf("dashboard shortcut is not bold: %q", got)
	}
}

func TestDashboardFitsShortTerminal(t *testing.T) {
	m := New([]target.Target{{Name: "box"}, {Name: "offline"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "working", Workspace: "herdlord", Tab: "dashboard", Revision: 1}}}
	m.statuses["offline"] = poll.TargetStatus{State: poll.BackingOff, Err: strings.Repeat("SSH timeout ", 20)}
	_, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	_ = m.readFocused()
	view := strings.TrimSuffix(m.View(), "\n")
	if lines := strings.Count(view, "\n") + 1; lines > m.height {
		t.Fatalf("dashboard uses %d lines in a %d-line terminal:\n%s", lines, m.height, view)
	}
	m.beginDelete()
	view = strings.TrimSuffix(m.View(), "\n")
	if lines := strings.Count(view, "\n") + 1; lines > m.height {
		t.Fatalf("delete confirmation uses %d lines in a %d-line terminal:\n%s", lines, m.height, view)
	}
}

func TestAgentStatusColorsAreSemanticAndDistinct(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	seen := map[string]bool{}
	for _, status := range []string{"blocked", "done", "working", "idle"} {
		seen[colorAgentStatus(status)] = true
	}
	if len(seen) != 4 {
		t.Fatalf("agent styles are not distinct: %#v", seen)
	}
}

func TestTableStatusesContainNoEmbeddedANSI(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "one", Status: "working"},
		{PaneID: "p2", Agent: "two", Status: "idle"},
	}}
	m.width, m.height = 100, 24
	m.resize()
	for _, renderedRow := range m.table.Rows() {
		for _, cell := range renderedRow {
			if strings.Contains(cell, "\x1b") {
				t.Fatalf("table cell contains ANSI: %q", cell)
			}
		}
	}
	view := ansi.Strip(m.table.View())
	if !strings.Contains(view, "working") || strings.ContainsRune(view, '\ufffd') {
		t.Fatalf("working status was corrupted:\n%s", view)
	}
}

func TestFreshPollClearsRefreshMessage(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.refreshPending["box"] = true
	m.refreshTotal = 1
	m.setNotice(noticeInfo, "Refreshing 0 of 1 targets…")
	_, _ = m.Update(poll.Result{Name: "box", Status: poll.TargetStatus{State: poll.OK}})
	if m.message != "" {
		t.Fatalf("refresh message = %q", m.message)
	}
}

func TestNavigationKeyMapAndHelp(t *testing.T) {
	km := navigationKeyMap()
	wants := [][]string{
		{"up", "k", "ctrl+p"}, {"down", "j", "ctrl+n"}, {"pgup", "b", "alt+v"}, {"pgdown", "f", "ctrl+v"},
		{"ctrl+u"}, {"ctrl+d"}, {"home", "g"}, {"end", "G"},
	}
	got := [][]string{km.LineUp.Keys(), km.LineDown.Keys(), km.PageUp.Keys(), km.PageDown.Keys(), km.HalfPageUp.Keys(), km.HalfPageDown.Keys(), km.GotoTop.Keys(), km.GotoBottom.Keys()}
	if !reflect.DeepEqual(got, wants) {
		t.Fatalf("navigation keymap = %#v", got)
	}
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1"}}}
	m.rebuildRows()
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	view := m.View()
	for _, want := range []string{"Previous / next", "C-p", "M-v", "Half-page up/down", "Toggle inspector", "Manage targets", "Close help"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Once attached") {
		t.Fatalf("generic help contains attach-only instructions:\n%s", view)
	}
	actions := []string{"Attach agent", "Toggle inspector", "Expand output", "Manage targets", "Refresh targets", "Close help", "Quit"}
	foundActions := 0
	for _, line := range strings.Split(helpOverlay(true, true), "\n") {
		for _, action := range actions {
			if !strings.Contains(line, action) {
				continue
			}
			if prefix := fmt.Sprintf("  %-18s ", action); !strings.HasPrefix(line, prefix) {
				t.Fatalf("%q is not aligned to the action grid: %q", action, line)
			}
			foundActions++
		}
	}
	if foundActions != len(actions) {
		t.Fatalf("checked %d aligned actions, want %d", foundActions, len(actions))
	}
	if lines := strings.Count(view, "\n"); lines > 24 {
		t.Fatalf("help is %d lines, want at most 24:\n%s", lines, view)
	}
}

func TestAttachExplainsHowToReturn(t *testing.T) {
	m := New([]target.Target{{Name: "workbox"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["workbox"] = poll.TargetStatus{State: poll.OK, HerdrPath: "/opt/herdr", Agents: []herdr.Agent{{PaneID: "p1", TerminalID: "term-1", Agent: "codex"}}}
	m.rebuildRows()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.overlay.kind != overlayAttach {
		t.Fatalf("first Enter = command %v, overlay %v", cmd != nil, m.overlay.kind)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Attach to codex on workbox?", "Once attached, to return to Herdlord: Ctrl-b, then q", "Enter attach", "q/Esc cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("attach panel missing %q:\n%s", want, view)
		}
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.overlay.kind != overlayNone {
		t.Fatalf("confirm Enter = command %v, overlay %v", cmd != nil, m.overlay.kind)
	}
}

func TestAttachConfirmationCannotSwitchAgents(t *testing.T) {
	m := New([]target.Target{{Name: "workbox"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["workbox"] = poll.TargetStatus{State: poll.OK, HerdrPath: "/opt/herdr", Agents: []herdr.Agent{
		{PaneID: "p1", TerminalID: "term-1", Agent: "one", Status: "working"},
		{PaneID: "p2", TerminalID: "term-2", Agent: "two", Status: "idle"},
	}}
	m.rebuildRows()
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.statuses["workbox"] = poll.TargetStatus{State: poll.OK, HerdrPath: "/opt/herdr", Agents: []herdr.Agent{{PaneID: "p2", TerminalID: "term-2", Agent: "two", Status: "idle"}}}
	m.rebuildRows()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !strings.Contains(m.message, "no longer available") {
		t.Fatalf("changed selection attached: command %v, message %q", cmd != nil, m.message)
	}
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	configured := []target.Target{{Name: "workbox"}}
	if err := target.Save(path, configured); err != nil {
		t.Fatal(err)
	}
	m := New(configured, path, poll.Manager{Client: &fakeClient{}})
	m.statuses["workbox"] = poll.TargetStatus{State: poll.OK}
	m.rebuildRows()
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if got, err := target.Load(path); err != nil || len(got) != 1 {
		t.Fatalf("C-d deleted target: %#v, %v", got, err)
	}
	for _, cancel := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune{'n'}}, {Type: tea.KeyRunes, Runes: []rune{'q'}}, {Type: tea.KeyEsc}} {
		m.openTargetManager()
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
		_, _ = m.Update(cancel)
		if got, err := target.Load(path); err != nil || len(got) != 1 {
			t.Fatalf("target deleted after cancellation: %#v, %v", got, err)
		}
	}
	m.openTargetManager()
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !strings.Contains(m.View(), `Remove "workbox" from Herdlord?`) || !strings.Contains(m.View(), "y delete  q/n/Esc cancel") {
		t.Fatalf("confirmation view:\n%s", m.View())
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if got, err := target.Load(path); err != nil || len(got) != 0 {
		t.Fatalf("confirmed target remains: %#v, %v", got, err)
	}
}

func TestDashboardGolden(t *testing.T) {
	tests := []struct {
		name     string
		targets  []target.Target
		statuses map[string]poll.TargetStatus
	}{
		{"empty", nil, map[string]poll.TargetStatus{}},
		{"skewed", []target.Target{{Name: "old-box"}}, map[string]poll.TargetStatus{"old-box": {State: poll.Skewed, Err: "protocol 18 is too old (minimum: 19)"}}},
		{"no-agents", []target.Target{{Name: "quiet-box"}}, map[string]poll.TargetStatus{"quiet-box": {State: poll.OK}}},
	}
	var views []string
	for _, tt := range tests {
		m := New(tt.targets, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
		m.statuses = tt.statuses
		m.width, m.height = 100, 24
		m.resize()
		m.rebuildRows()
		views = append(views, "== "+tt.name+" ==\n"+compactView(ansi.Strip(m.View())))
	}
	got := strings.Join(views, "\n\n") + "\n"
	want, err := os.ReadFile("testdata/dashboard.golden")
	if err != nil {
		t.Fatalf("read golden: %v\n%s", err, got)
	}
	if got != string(want) {
		t.Fatalf("dashboard mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func compactView(view string) string {
	lines := strings.Split(view, "\n")
	compact := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		if line == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		compact = append(compact, line)
	}
	return strings.TrimSpace(strings.Join(compact, "\n"))
}

func runOutputCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected output command")
	}
	msg := cmd()
	_, _ = m.Update(msg)
}
