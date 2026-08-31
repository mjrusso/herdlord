package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func hint(keys, action string) string {
	return lipgloss.NewStyle().Bold(true).Render(keys) + " " + action
}

func hints(items ...string) string {
	return strings.Join(items, "  ")
}

func (m *Model) footerView() string {
	if len(m.targets) == 0 {
		return hints(hint("t", "targets"), hint("?", "help"), hint("q", "quit"))
	}
	switch {
	case m.width > 0 && m.width < 80:
		if focused := m.focused(); focused != nil && focused.agent != nil {
			return hints(hint("↑/↓", "nav"), hint("enter", "attach"), hint("t", "targets"), hint("?", "help"), hint("q", "quit"))
		}
		return hints(hint("↑/↓", "navigate"), hint("t", "targets"), hint("?", "help"), hint("q", "quit"))
	case m.width > 0 && m.width < 100:
		if focused := m.focused(); focused != nil && focused.agent != nil {
			return hints(hint("↑/↓", "nav"), hint("enter", "attach"), hint("o", "output"), hint("i", "inspect"), hint("t", "targets"), hint("?", "help"), hint("q", "quit"))
		}
		return hints(hint("↑/↓", "navigate"), hint("t", "targets"), hint("r", "refresh"), hint("?", "help"), hint("q", "quit"))
	default:
		if focused := m.focused(); focused != nil && focused.agent != nil {
			return hints(hint("↑/↓", "navigate"), hint("enter", "attach"), hint("o", "output"), hint("i", "inspect"), hint("t", "targets"), hint("r", "refresh"), hint("?", "help"), hint("q", "quit"))
		}
		return hints(hint("↑/↓", "navigate"), hint("t", "targets"), hint("r", "refresh"), hint("?", "help"), hint("q", "quit"))
	}
}

func (m *Model) outputLineLimit() int {
	limit := m.height - 16
	if m.width > 0 && m.width < 80 {
		limit--
	}
	if m.healthView() != "" {
		limit -= 4 + m.healthRowLimit()
	}
	return min(36, max(3, limit))
}

func (m *Model) updateTableHeight() {
	if m.height <= 0 {
		return
	}
	reserved := 5
	if m.healthView() != "" {
		reserved += 4 + m.healthRowLimit()
	}
	if focused := m.focused(); focused != nil && focused.agent != nil {
		if m.showInspector {
			reserved += lipgloss.Height(m.inspectorView()) + 1
		}
	}
	if m.overlay.kind != overlayNone || m.message != "" {
		reserved += 3
	}
	height := m.height - reserved
	if height < 3 {
		height = 3
	}
	m.table.SetHeight(height)
}
