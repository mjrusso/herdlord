package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

type tableLayout int

const (
	narrowLayout tableLayout = iota
	mediumLayout
	wideLayout
)

func (m *Model) configureColumns() {
	// SetColumns renders immediately, so rows from the previous layout must not
	// remain when the new layout has fewer columns.
	m.table.SetRows(nil)
	switch {
	case m.width >= 120:
		m.layout = wideLayout
		workspaceWidth, tabWidth, terminalWidth := 18, 14, 24
		spare := max(0, m.width-108)
		workspaceWidth += spare / 3
		tabWidth += spare / 6
		terminalWidth += spare - spare/3 - spare/6
		m.table.SetColumns([]table.Column{{Title: "", Width: 4}, {Title: "TARGET", Width: 12}, {Title: "WORKSPACE", Width: workspaceWidth}, {Title: "TAB", Width: tabWidth}, {Title: "AGENT", Width: 8}, {Title: "STATUS", Width: 14}, {Title: "TERMINAL", Width: terminalWidth}})
	case m.width >= 80:
		m.layout = mediumLayout
		workspaceWidth, tabWidth := 16, 12
		spare := max(0, m.width-78)
		workspaceWidth += spare * 2 / 3
		tabWidth += spare - spare*2/3
		m.table.SetColumns([]table.Column{{Title: "", Width: 4}, {Title: "TARGET", Width: 12}, {Title: "WORKSPACE", Width: workspaceWidth}, {Title: "TAB", Width: tabWidth}, {Title: "AGENT", Width: 8}, {Title: "STATUS", Width: 14}})
	default:
		m.layout = narrowLayout
		targetWidth, workspaceWidth := 10, 12
		spare := max(0, m.width-57)
		targetWidth += spare / 3
		workspaceWidth += spare - spare/3
		m.table.SetColumns([]table.Column{{Title: "", Width: 4}, {Title: "TARGET", Width: targetWidth}, {Title: "WORKSPACE", Width: workspaceWidth}, {Title: "AGENT", Width: 7}, {Title: "STATUS", Width: 14}})
	}
	if m.width > 0 {
		m.table.SetWidth(m.width)
	}
}

func (m *Model) rebuildRows() {
	focusedTarget, focusedPane := m.focusIdentity()
	rows := make([]row, 0)
	for _, t := range m.targets {
		s, ok := m.statuses[t.Name]
		if !ok {
			s = poll.TargetStatus{State: poll.Checking}
		}
		if !s.State.Usable() || len(s.Agents) == 0 {
			status := label(s)
			if s.State.Usable() {
				status = "no agents"
			}
			rows = append(rows, row{target: t.Name, status: status, detail: s.Err})
			continue
		}
		for i := range s.Agents {
			a := s.Agents[i]
			rows = append(rows, row{target: t.Name, agent: &a, status: a.Status})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := rank(rows[i]), rank(rows[j])
		if ri != rj {
			return ri < rj
		}
		if rows[i].target != rows[j].target {
			return rows[i].target < rows[j].target
		}
		if rows[i].agent == nil || rows[j].agent == nil {
			return rows[i].agent != nil
		}
		return rows[i].agent.Agent < rows[j].agent.Agent
	})
	m.rows = rows
	cursor := m.table.Cursor()
	cursor = min(max(0, cursor), max(0, len(rows)-1))
	if focusedTarget != "" {
		for i := range rows {
			if rows[i].target != focusedTarget {
				continue
			}
			if focusedPane == "" || (rows[i].agent != nil && rows[i].agent.PaneID == focusedPane) {
				cursor = i
				break
			}
		}
	}
	// The pasture defers the repaint, so tableCursor carries the tracked row until the table is shown again.
	m.tableCursor = cursor
	if m.pasture.Visible() {
		m.tableDirty = true
	} else {
		m.setTableRows(cursor)
	}
	if len(rows) == 0 {
		m.output.clear()
	}
}

func (m *Model) focusIdentity() (string, string) {
	focused := m.focused()
	if focused == nil {
		return "", ""
	}
	if focused.agent == nil {
		return focused.target, ""
	}
	return focused.target, focused.agent.PaneID
}

func (m *Model) rowValues(targetName string, agent *herdr.Agent, status, detail string) []string {
	targetName, status, detail = display.Text(targetName), display.Text(status), display.Text(detail)
	workspace, tab, agentName, title := "—", "—", "—", detail
	if agent != nil {
		workspace, tab, agentName, title = displayLabel(agent.Workspace), displayLabel(agent.Tab), displayLabel(agent.Agent), displayLabel(agent.Title())
		status = agentStatusLabel(status)
	}
	switch m.layout {
	case wideLayout:
		return []string{"", targetName, workspace, tab, agentName, status, title}
	case mediumLayout:
		return []string{"", targetName, workspace, tab, agentName, status}
	default:
		return []string{"", targetName, workspace, agentName, status}
	}
}

func (m *Model) updateSelectionMarkers() {
	m.setTableRows(m.table.Cursor())
}

func (m *Model) setTableRows(cursor int) {
	m.tableCursor = cursor
	values := make([]table.Row, len(m.rows))
	for i := range m.rows {
		rowValues := m.rowValues(m.rows[i].target, m.rows[i].agent, m.rows[i].status, m.rows[i].detail)
		marker := attentionMarker(m.rows[i].agent)
		if m.rows[i].agent == nil {
			marker = "◇"
		}
		cursorMarker := " "
		if i == cursor {
			cursorMarker = ">"
		}
		rowValues[0] = cursorMarker + " " + marker
		if i != cursor && m.rows[i].agent != nil && m.rows[i].agent.Status == "working" {
			rowValues[m.statusColumnIndex()] = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("working")
		}
		values[i] = rowValues
	}
	m.table.SetRows(values)
	if m.table.Cursor() != cursor {
		m.table.SetCursor(cursor)
	}
	m.tableDirty = false
}

func (m *Model) statusColumnIndex() int {
	for i, column := range m.table.Columns() {
		if column.Title == "STATUS" {
			return i
		}
	}
	return len(m.table.Columns()) - 1
}

func agentStatusLabel(status string) string {
	switch status {
	case "blocked":
		return "needs input"
	case "done":
		return "done"
	default:
		return status
	}
}

func attentionMarker(agent *herdr.Agent) string {
	if agent == nil {
		return ""
	}
	switch agent.Status {
	case "blocked":
		return "!"
	case "done":
		return "●"
	case "working":
		return "*"
	default:
		return ""
	}
}

func displayLabel(value string) string {
	value = display.Text(value)
	if value == "" {
		return "—"
	}
	return value
}

func rank(r row) int {
	if r.agent == nil {
		return 5
	}
	switch r.agent.Status {
	case "blocked":
		return 0
	case "done":
		return 1
	case "working":
		return 2
	case "idle":
		return 3
	default:
		return 4
	}
}

func label(s poll.TargetStatus) string {
	switch s.State {
	case poll.OK:
		return "ok"
	case poll.Unreachable:
		return "unreachable"
	case poll.NoHerdr:
		return "no herdr"
	case poll.Skewed:
		return "skewed"
	case poll.Newer:
		return "newer"
	case poll.Paused:
		return "paused"
	case poll.BackingOff:
		return "backing off"
	case poll.Checking:
		return "checking"
	default:
		return "unknown"
	}
}

func (m *Model) findTarget(name string) (target.Target, bool) {
	i := m.targetIndex(name)
	if i < 0 {
		return target.Target{}, false
	}
	return m.targets[i], true
}

func (m *Model) targetIndex(name string) int {
	for i := range m.targets {
		if m.targets[i].Name == name {
			return i
		}
	}
	return -1
}

func truncateLines(s string, limit int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) updateTableKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "i":
		if focused := m.focused(); focused != nil && focused.agent != nil {
			m.showInspector = !m.showInspector
		}
	case "o":
		m.openExpandedOutput()
		return m, nil
	case "enter":
		if !m.session.isDemo() {
			if focused := m.focused(); focused != nil && focused.agent != nil {
				m.overlay = overlayState{kind: overlayAttach, target: focused.target, pane: focused.agent.PaneID}
				return m, nil
			}
		}
	}
	old := m.table.Cursor()
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	if old != m.table.Cursor() {
		m.tableCursor = m.table.Cursor()
		m.updateSelectionMarkers()
		return m, tea.Batch(cmd, m.readFocused())
	}
	return m, cmd
}
