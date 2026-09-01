package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/target"
	"github.com/mjrusso/herdlord/internal/targetmgr"
)

func (m *Model) beginDelete() {
	if m.overlay.kind == overlayTargets {
		if configured, ok := m.managedTarget(); ok {
			m.overlay = overlayState{kind: overlayDelete, target: configured.Name}
		}
		return
	}
	r := m.focused()
	if r == nil {
		return
	}
	m.overlay = overlayState{kind: overlayDelete, target: r.target}
}

func (m *Model) openTargetManager() {
	m.overlay = overlayState{kind: overlayTargets}
	m.clearNotice()
	if m.targetCursor >= len(m.targets) {
		m.targetCursor = max(0, len(m.targets)-1)
	}
}

func (m *Model) managedTarget() (target.Target, bool) {
	if len(m.targets) == 0 || m.targetCursor < 0 || m.targetCursor >= len(m.targets) {
		return target.Target{}, false
	}
	return m.targets[m.targetCursor], true
}

func (m *Model) updateTargetManager(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.stopAll()
		return m, tea.Quit
	case "q", "esc", "t":
		m.overlay = overlayState{}
		m.clearNotice()
	case "up", "k":
		m.targetCursor = max(0, m.targetCursor-1)
	case "down", "j":
		m.targetCursor = min(max(0, len(m.targets)-1), m.targetCursor+1)
	case "home", "g":
		m.targetCursor = 0
	case "end", "G":
		m.targetCursor = max(0, len(m.targets)-1)
	case "a":
		m.openAddForm()
		return m, textinput.Blink
	case "e":
		if configured, ok := m.managedTarget(); ok {
			m.openEditForm(configured)
			return m, textinput.Blink
		}
	case "d":
		m.beginDelete()
	case " ":
		m.toggleFocused()
	}
	return m, nil
}

func (m *Model) targetManagerView() string {
	lines := []string{lipgloss.NewStyle().Bold(true).Render("Targets"), ""}
	if len(m.targets) == 0 {
		lines = append(lines, "No targets configured.", "", "Press a to add a local or remote Herdr session.")
	} else {
		lines = append(lines, "   NAME           STATE        COMMAND")
		limit := len(m.targets)
		if m.height > 0 {
			limit = min(limit, max(1, m.height-12))
		}
		start := max(0, min(m.targetCursor-limit/2, len(m.targets)-limit))
		for i := start; i < start+limit; i++ {
			configured := m.targets[i]
			cursor := " "
			if i == m.targetCursor {
				cursor = ">"
			}
			state := label(m.statuses[configured.Name])
			if configured.Paused {
				state = "paused"
			}
			command := formatPrefix(configured.Prefix)
			if command == "" {
				command = "local"
			}
			line := fmt.Sprintf("%s  %-14s %-12s %s", cursor, display.Text(configured.Name), state, display.Text(command))
			width := 76
			if m.width > 0 {
				width = max(20, min(76, m.width-12))
			}
			lines = append(lines, ansi.Truncate(line, width, "…"))
		}
	}
	if m.message != "" {
		lines = append(lines, "", m.noticeView())
	}
	actionLines := []string{hints(hint("a", "add"), hint("e", "edit"), hint("d", "delete"), hint("space", "pause/resume"), hint("q/Esc", "back"), hint("Ctrl-C", "quit"))}
	if m.width > 0 && m.width < 70 {
		actionLines = []string{
			hints(hint("a", "add"), hint("e", "edit"), hint("d", "delete"), hint("space", "pause/resume")),
			hints(hint("q/Esc", "back"), hint("Ctrl-C", "quit")),
		}
	}
	lines = append(lines, "", strings.Join(actionLines, "\n"))
	return strings.Join(lines, "\n")
}

func formatPrefix(prefix []string) string {
	parts := make([]string, len(prefix))
	for i, part := range prefix {
		if part != "" && strings.IndexFunc(part, func(r rune) bool {
			return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-", r)
		}) == -1 {
			parts[i] = part
			continue
		}
		parts[i] = "'" + strings.ReplaceAll(part, "'", "'\"'\"'") + "'"
	}
	return strings.Join(parts, " ")
}

func (m *Model) updateDeleteConfirmation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := m.overlay.target
	switch msg.String() {
	case "ctrl+c":
		m.stopAll()
		return m, tea.Quit
	case "esc", "q", "Q", "n", "N":
		m.overlay = overlayState{kind: overlayTargets}
		return m, nil
	case "y", "Y":
		m.overlay = overlayState{kind: overlayTargets}
	default:
		return m, nil
	}
	if _, err := (targetmgr.Manager{Poller: m.manager}).Remove(m.configPath, name); err != nil {
		m.setNotice(noticeError, "Could not delete "+name+": "+err.Error())
		return m, nil
	}
	latest, err := target.Load(m.configPath)
	if err != nil {
		m.setNotice(noticeError, "Deleted "+name+", but could not reload targets: "+err.Error())
		return m, nil
	}
	m.reconcile(latest)
	m.setNotice(noticeSuccess, "Deleted "+name)
	return m, nil
}

func (m *Model) deleteView() string {
	t, _ := m.findTarget(m.overlay.target)
	prefix := "local"
	if len(t.Prefix) > 0 {
		prefix = strings.Join(t.Prefix, " ")
	}
	contentWidth := 78
	if m.width > 0 {
		contentWidth = max(12, min(78, m.width-8))
		prefix = ansi.Truncate(prefix, max(12, contentWidth-9), "…")
	}
	agents := 0
	for i := range m.rows {
		if m.rows[i].target == t.Name && m.rows[i].agent != nil {
			agents++
		}
	}
	count := fmt.Sprintf("%d agents", agents)
	if agents == 1 {
		count = "1 agent"
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render("Delete target"),
		"",
		fmt.Sprintf("Remove %q from Herdlord?", display.Text(t.Name)),
		"",
		"Command  " + display.Text(prefix),
		"Agents   " + count,
		"",
		ansi.Wrap("This removes the target configuration. It does not stop Herdr or its agents.", contentWidth, " "),
		"",
		hints(hint("y", "delete"), hint("q/n/Esc", "cancel"), hint("Ctrl-C", "quit")),
	}
	return strings.Join(lines, "\n")
}

func (m *Model) toggleFocused() {
	name := ""
	if m.overlay.kind == overlayTargets {
		if configured, ok := m.managedTarget(); ok {
			name = configured.Name
		}
	} else if r := m.focused(); r != nil {
		name = r.target
	}
	if name == "" {
		return
	}
	if _, err := (targetmgr.Manager{Poller: m.manager}).TogglePaused(m.configPath, name); err != nil {
		m.setNotice(noticeError, "Could not update "+name+": "+err.Error())
		return
	}
	latest, err := target.Load(m.configPath)
	if err != nil {
		m.setNotice(noticeError, "Updated "+name+", but could not reload targets: "+err.Error())
		return
	}
	m.reconcile(latest)
	if configured, ok := m.findTarget(name); ok && configured.Paused {
		m.setNotice(noticeSuccess, "Paused "+name)
	} else {
		m.setNotice(noticeSuccess, "Resumed "+name)
	}
}

func (m *Model) refreshAll() {
	if len(m.refresh) == 0 {
		m.setNotice(noticeInfo, "No active targets to refresh")
		return
	}
	m.refreshPending = make(map[string]bool, len(m.refresh))
	m.refreshTotal = len(m.refresh)
	for name, ch := range m.refresh {
		m.refreshPending[name] = true
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	m.setNotice(noticeInfo, fmt.Sprintf("Refreshing 0 of %d targets…", m.refreshTotal))
}

func (m *Model) updateRefreshProgress(name string) {
	if !m.refreshPending[name] {
		return
	}
	delete(m.refreshPending, name)
	done := m.refreshTotal - len(m.refreshPending)
	if len(m.refreshPending) == 0 {
		m.refreshTotal = 0
		if m.messageKind == noticeInfo && strings.HasPrefix(m.message, "Refreshing ") {
			m.clearNotice()
		}
		return
	}
	if m.messageKind != noticeError {
		m.setNotice(noticeInfo, fmt.Sprintf("Refreshing %d of %d targets…", done, m.refreshTotal))
	}
}
