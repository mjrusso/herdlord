package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
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
			rows = append(rows, row{target: t.Name, values: m.rowValues(t.Name, nil, status, s.Err)})
			continue
		}
		for i := range s.Agents {
			a := s.Agents[i]
			rows = append(rows, row{target: t.Name, agent: &a, values: m.rowValues(t.Name, &a, a.Status, "")})
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
	values := make([]table.Row, len(rows))
	for i := range rows {
		values[i] = rows[i].values
	}
	m.table.SetRows(values)
	if len(rows) > 0 && m.table.Cursor() < 0 {
		m.table.SetCursor(0)
	}
	m.restoreFocus(focusedTarget, focusedPane)
	m.updateSelectionMarkers()
	m.updateTableHeight()
	if len(rows) == 0 {
		m.outputKey, m.output, m.outputLoading = "", "", false
		m.loadingKey, m.loadingRev = "", 0
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

func (m *Model) restoreFocus(targetName, paneID string) {
	if targetName == "" {
		return
	}
	for i := range m.rows {
		if m.rows[i].target != targetName {
			continue
		}
		if paneID == "" || (m.rows[i].agent != nil && m.rows[i].agent.PaneID == paneID) {
			m.table.SetCursor(i)
			return
		}
	}
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
	values := make([]table.Row, len(m.rows))
	for i := range m.rows {
		rowValues := append([]string(nil), m.rows[i].values...)
		marker := attentionMarker(m.rows[i].agent)
		if m.rows[i].agent == nil {
			marker = "◇"
		}
		cursor := " "
		if i == m.table.Cursor() {
			cursor = ">"
		}
		rowValues[0] = cursor + " " + marker
		if i != m.table.Cursor() && m.rows[i].agent != nil && m.rows[i].agent.Status == "working" {
			rowValues[m.statusColumnIndex()] = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("working")
		}
		values[i] = rowValues
	}
	m.table.SetRows(values)
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

func colorLabel(state poll.State, value string) string {
	var color lipgloss.Color
	switch state {
	case poll.Paused:
		color = lipgloss.Color("8")
	case poll.BackingOff:
		color = lipgloss.Color("3")
	case poll.Unreachable:
		color = lipgloss.Color("1")
	case poll.NoHerdr:
		color = lipgloss.Color("5")
	case poll.Skewed:
		color = lipgloss.Color("4")
	case poll.Newer:
		color = lipgloss.Color("3")
	case poll.Checking:
		color = lipgloss.Color("8")
	default:
		return value
	}
	return lipgloss.NewStyle().Foreground(color).Render(value)
}

func colorAgentStatus(value string) string {
	style := lipgloss.NewStyle()
	switch value {
	case "blocked":
		style = style.Foreground(lipgloss.Color("1")).Bold(true)
	case "done":
		style = style.Foreground(lipgloss.Color("3")).Bold(true)
	case "working":
		style = style.Foreground(lipgloss.Color("6"))
	case "idle", "unknown":
		style = style.Foreground(lipgloss.Color("8"))
	default:
		return agentStatusLabel(value)
	}
	return style.Render(agentStatusLabel(value))
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

func (m *Model) resize() {
	m.resizeAddInputs()
	m.configureColumns()
	m.rebuildRows()
}

func truncateLines(s string, limit int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n")
}
