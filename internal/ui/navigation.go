package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
)

func navigationKeyMap() table.KeyMap {
	return table.KeyMap{
		LineUp:       key.NewBinding(key.WithKeys("up", "k", "ctrl+p"), key.WithHelp("↑, k, C-p", "")),
		LineDown:     key.NewBinding(key.WithKeys("down", "j", "ctrl+n"), key.WithHelp("↓, j, C-n", "")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "b", "alt+v"), key.WithHelp("Page Up, b, M-v", "")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", "f", "ctrl+v"), key.WithHelp("Page Down, f, C-v", "")),
		HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("C-u", "")),
		HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("C-d", "")),
		GotoTop:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("Home, g", "")),
		GotoBottom:   key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("End, G", "")),
	}
}

func navigationHelpRow(action string, bindings ...key.Binding) string {
	keys := make([]string, len(bindings))
	for i, binding := range bindings {
		keys[i] = binding.Help().Key
	}
	return fmt.Sprintf("  %-22s %s", action, strings.Join(keys, " / "))
}

func (m *Model) helpOverlay() string {
	if len(m.targets) == 0 {
		return strings.Join([]string{
			"Getting started",
			helpRow("Manage targets", "t"),
			"",
			hints(hint("?/Esc/q", "close"), hint("Ctrl-C", "quit")),
		}, "\n")
	}
	keys := navigationKeyMap()
	lines := []string{
		"Rows  ! blocked   ● done   * working   ◇ target status",
		"",
		"Navigation",
	}
	unit := "row"
	if m.pasture.Visible() {
		unit = "pen"
	}
	lines = append(lines,
		navigationHelpRow("Previous / next", keys.LineUp, keys.LineDown),
		navigationHelpRow("Page up / down", keys.PageUp, keys.PageDown),
		navigationHelpRow("Half-page up/down", keys.HalfPageUp, keys.HalfPageDown),
		navigationHelpRow("First / last "+unit, keys.GotoTop, keys.GotoBottom),
	)
	lines = append(lines, "", "Actions")
	if m.session.isDemo() {
		lines = append(lines,
			helpRow("Start / pause demo", "p"),
		)
	}
	if m.hasAgents() && !m.pasture.Visible() {
		if !m.session.isDemo() {
			lines = append(lines, helpRow("Attach agent", "Enter"))
		}
		lines = append(lines,
			helpRow("Toggle inspector", "i"),
			helpRow("Expand output", "o"),
		)
	}
	lines = append(lines,
		helpRow("Switch table / pasture", "v"),
	)
	if m.pasture.Visible() {
		lines = append(lines,
			helpRow("Switch renderer", "R"),
		)
	}
	lines = append(lines, helpRow("Manage targets", "t"))
	if !m.session.isDemo() {
		lines = append(lines, helpRow("Refresh targets", "r"))
	}
	lines = append(lines, "", hints(hint("?/Esc/q", "close"), hint("Ctrl-C", "quit")))
	return strings.Join(lines, "\n")
}

func helpRow(action, keys string) string {
	return fmt.Sprintf("  %-22s %s", action, keys)
}
