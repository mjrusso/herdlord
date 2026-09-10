package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) modalView(background, body string, maxWidth int) string {
	// Every path drops the background, so it must still carry the background's Kitty deletes or its sprites stay on screen.
	control := leadingKittyDeletes(background)
	if m.width <= 0 || m.height <= 0 {
		return control + body + "\n"
	}
	outerWidth := min(maxWidth, m.width-2)
	if outerWidth < 42 {
		return control + body + "\n"
	}
	panel := lipgloss.NewStyle().
		Width(outerWidth-2).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Render(body)
	panelWidth, panelHeight := lipgloss.Width(panel), lipgloss.Height(panel)
	if panelHeight > m.height {
		return control + body + "\n"
	}

	left := (m.width - panelWidth) / 2
	top := (m.height - panelHeight) / 2
	backgroundLines := strings.Split(ansi.Strip(background), "\n")
	panelLines := strings.Split(panel, "\n")
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	lines := make([]string, m.height)
	for y := range lines {
		line := ""
		if y < len(backgroundLines) {
			line = ansi.Truncate(backgroundLines[y], m.width, "")
		}
		line += strings.Repeat(" ", max(0, m.width-ansi.StringWidth(line)))
		if y < top || y >= top+panelHeight {
			lines[y] = dim.Render(line)
			continue
		}
		prefix := ansi.Cut(line, 0, left)
		suffix := ansi.Cut(line, left+panelWidth, m.width)
		lines[y] = dim.Render(prefix) + panelLines[y-top] + dim.Render(suffix)
	}
	return control + strings.Join(lines, "\n")
}

func leadingKittyDeletes(value string) string {
	const deletePrefix = "\x1b_Ga=d,"
	end := 0
	for strings.HasPrefix(value[end:], deletePrefix) {
		terminator := strings.Index(value[end:], "\x1b\\")
		if terminator < 0 {
			break
		}
		end += terminator + 2
	}
	return value[:end]
}

func (m *Model) outputModalWidth() int {
	return max(42, m.width-4)
}
