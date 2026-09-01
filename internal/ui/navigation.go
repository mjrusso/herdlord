package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
)

func navigationKeyMap() table.KeyMap {
	return table.KeyMap{
		LineUp:       key.NewBinding(key.WithKeys("up", "k", "ctrl+p")),
		LineDown:     key.NewBinding(key.WithKeys("down", "j", "ctrl+n")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "b", "alt+v")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", "f", "ctrl+v")),
		HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u")),
		HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d")),
		GotoTop:      key.NewBinding(key.WithKeys("home", "g")),
		GotoBottom:   key.NewBinding(key.WithKeys("end", "G")),
	}
}

func helpOverlay(hasTargets, hasAgents bool) string {
	if !hasTargets {
		return strings.Join([]string{
			"Getting started",
			helpRow("Manage targets", "t"),
			"",
			hints(hint("?/Esc/q", "close"), hint("Ctrl-C", "quit")),
		}, "\n")
	}
	lines := []string{
		"Rows  ! blocked   ● done   * working   ◇ target status",
		"",
		"Navigation",
		"  Previous / next    ↑/↓, k/j, C-p/C-n",
		"  Page up / down     Page Up/Page Down, b/f, M-v/C-v",
		"  Half-page up/down  C-u/C-d",
		"  First / last row   Home/End, g/G",
		"",
		"Actions",
	}
	if hasAgents {
		lines = append(lines,
			helpRow("Attach agent", "Enter"),
			helpRow("Toggle inspector", "i"),
			helpRow("Expand output", "o"),
		)
	}
	lines = append(lines,
		helpRow("Manage targets", "t"),
		helpRow("Refresh targets", "r"),
		"",
		hints(hint("?/Esc/q", "close"), hint("Ctrl-C", "quit")),
	)
	return strings.Join(lines, "\n")
}

func helpRow(action, keys string) string {
	return fmt.Sprintf("  %-18s %s", action, keys)
}
