package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/herdr"
)

func (m *Model) detailsView() string {
	r := m.focused()
	if r == nil || r.agent == nil {
		return ""
	}
	a := r.agent
	terminal := a.Title()
	var lines []string
	if m.width >= 80 || m.width <= 0 {
		lines = []string{
			fmt.Sprintf("Workspace %s  Tab %s  Pane %s", relationship(a.Workspace, a.WorkspaceID), relationship(a.Tab, a.TabID), displayLabel(a.PaneID)),
			fmt.Sprintf("Directory %s  Terminal %s", compactHome(a.CWD), displayLabel(terminal)),
		}
	} else {
		lines = []string{
			fmt.Sprintf("Workspace %s  Tab %s", relationship(a.Workspace, a.WorkspaceID), relationship(a.Tab, a.TabID)),
			fmt.Sprintf("Pane %s  Directory %s", displayLabel(a.PaneID), compactHome(a.CWD)),
			fmt.Sprintf("Terminal %s", displayLabel(terminal)),
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) inspectorView() string {
	if focused := m.focused(); focused == nil || focused.agent == nil {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	contentWidth := max(1, width-4)
	r := m.focused()
	parts := []string{
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(
			fmt.Sprintf("Agent inspector  %s / %s / %s", displayLabel(r.target), displayLabel(r.agent.Agent), displayLabel(agentStatusLabel(r.agent.Status))),
		),
		ansi.Wrap(m.detailsView(), contentWidth, ""),
	}
	if m.outputLoading {
		parts = append(parts, "", lipgloss.NewStyle().Bold(true).Render("Recent output"), "Loading recent output…")
	} else if m.outputKey != "" {
		output := truncateLines(m.output, m.outputLineLimit())
		if output == "" {
			output = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No recent output")
		}
		parts = append(parts, "", lipgloss.NewStyle().Bold(true).Render("Recent output"), truncateBlock(output, contentWidth))
	}
	body := strings.Join(parts, "\n")
	return lipgloss.NewStyle().
		Width(max(1, width-2)).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Render(body)
}

func truncateBlock(block string, width int) string {
	lines := strings.Split(block, "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return strings.Join(lines, "\n")
}

func relationship(label, id string) string {
	label, id = display.Text(label), display.Text(id)
	switch {
	case label != "" && id != "":
		return fmt.Sprintf("%s (%s)", label, id)
	case label != "":
		return label
	case id != "":
		return id
	default:
		return "—"
	}
}

func compactHome(path string) string {
	path = display.Text(path)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return displayLabel(path)
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return displayLabel(path)
}

func (m *Model) syncFocusedOutput() {
	focused := m.focused()
	key := m.focusKey()
	if key == "" || focused == nil || focused.agent == nil {
		m.outputKey, m.output, m.outputLoading = "", "", false
		if m.overlay.kind == overlayOutput {
			m.overlay = overlayState{}
		}
		m.loadingKey, m.loadingRev = "", 0
		return
	}
	if cached, ok := m.outputs[key]; ok && cached.revision == focused.agent.Revision {
		m.outputKey, m.output, m.outputLoading = key, cached.text, false
		m.loadingKey, m.loadingRev = "", 0
		return
	}
	m.outputKey, m.output, m.outputLoading = "", "", false
	m.loadingKey, m.loadingRev = "", 0
}

func (m *Model) focused() *row {
	if m.table.Cursor() < 0 || m.table.Cursor() >= len(m.rows) {
		return nil
	}
	return &m.rows[m.table.Cursor()]
}

func (m *Model) focusKey() string {
	r := m.focused()
	if r == nil || r.agent == nil {
		return ""
	}
	return r.target + "\x00" + r.agent.PaneID
}

func (m *Model) readFocused() tea.Cmd {
	r := m.focused()
	if r == nil || r.agent == nil {
		m.outputKey, m.output, m.outputLoading = "", "", false
		if m.overlay.kind == overlayOutput {
			m.overlay = overlayState{}
		}
		m.loadingKey, m.loadingRev = "", 0
		return nil
	}
	key, revision := m.focusKey(), r.agent.Revision
	live := r.agent.Status == "working"
	generation := m.generations[r.target]
	cached, hasCached := m.outputs[key]
	if hasCached && revision == cached.revision && !live {
		m.outputKey, m.output, m.outputLoading = key, cached.text, false
		m.loadingKey, m.loadingRev = "", 0
		return nil
	}
	if inflightRevision, ok := m.inflight[key]; ok && inflightRevision == revision {
		if live && hasCached {
			m.outputKey, m.output, m.outputLoading = key, cached.text, false
		} else {
			m.outputKey, m.output, m.outputLoading = "", "", true
		}
		m.loadingKey, m.loadingRev = key, revision
		m.updateTableHeight()
		return nil
	}
	t, ok := m.findTarget(r.target)
	if !ok {
		return nil
	}
	s := m.statuses[r.target]
	pane := r.agent.PaneID
	if live && hasCached {
		m.outputKey, m.output, m.outputLoading = key, cached.text, false
	} else {
		m.outputKey, m.output, m.outputLoading = "", "", true
	}
	m.loadingKey, m.loadingRev = key, revision
	m.inflight[key] = revision
	m.updateTableHeight()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), m.manager.EffectiveTimeout())
		defer cancel()
		out, err := m.manager.Client.Read(ctx, t, s.HerdrPath, pane, 120)
		return outputMsg{key: key, target: t.Name, generation: generation, revision: revision, text: out, err: err}
	}
}

func (m *Model) openExpandedOutput() {
	if focused := m.focused(); focused == nil || focused.agent == nil {
		return
	}
	m.overlay = overlayState{kind: overlayOutput}
	m.configureOutputViewport(true)
}

func (m *Model) configureOutputViewport(followBottom bool) {
	width := max(1, m.outputModalWidth()-6)
	height := max(1, m.height-11)
	m.outputViewport.Width = width
	m.outputViewport.Height = height
	content := m.output
	if content == "" && !m.outputLoading {
		content = "No recent output"
	}
	m.outputViewport.SetContent(ansi.Wrap(content, width, " "))
	if followBottom {
		m.outputViewport.GotoBottom()
	}
}

func (m *Model) updateExpandedOutput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.stopAll()
		return m, tea.Quit
	case "q", "o", "esc":
		m.overlay = overlayState{}
		return m, nil
	case "home", "g":
		m.outputViewport.GotoTop()
		return m, nil
	case "end", "G":
		m.outputViewport.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.outputViewport, cmd = m.outputViewport.Update(msg)
	return m, cmd
}

func (m *Model) expandedOutputView() string {
	context := "Selected agent"
	if focused := m.focused(); focused != nil && focused.agent != nil {
		context = fmt.Sprintf("%s / %s / %s", displayLabel(focused.target), displayLabel(focused.agent.Workspace), displayLabel(focused.agent.Agent))
	}
	content := m.outputViewport.View()
	if m.outputLoading {
		content = "Loading recent output…"
	}
	position := fmt.Sprintf("%d%%", int(m.outputViewport.ScrollPercent()*100))
	footer := hints(
		hint("↑/↓", "scroll"),
		hint("PgUp/PgDn", "page"),
		hint("q/o/Esc", "close"),
		hint("Ctrl-C", "quit"),
	) + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(position)
	return strings.Join([]string{
		lipgloss.NewStyle().Bold(true).Render("Recent terminal output"),
		context,
		"",
		content,
		"",
		footer,
	}, "\n")
}

func (m *Model) attachFocused() tea.Cmd {
	r := m.focused()
	if r == nil || r.agent == nil {
		return nil
	}
	t, ok := m.findTarget(r.target)
	if !ok {
		return nil
	}
	key := m.focusKey()
	herdrPath := m.statuses[r.target].HerdrPath
	cmd, err := herdr.AttachCommand(t, herdrPath, r.agent.TerminalID)
	if err != nil {
		return func() tea.Msg { return attachResult(key)(err) }
	}
	return tea.ExecProcess(cmd, attachResult(key))
}

func (m *Model) attachView() string {
	r := m.focused()
	if r == nil || r.agent == nil {
		return "The selected agent is no longer available.\n\n" + hint("q/Esc", "close")
	}
	return fmt.Sprintf(
		"Attach to %s on %s?\n\nOnce attached, to return to Herdlord: Ctrl-b, then q\n\n%s",
		displayLabel(r.agent.Agent), displayLabel(r.target), hints(hint("Enter", "attach"), hint("q/Esc", "cancel")),
	)
}

func attachResult(key string) func(error) tea.Msg {
	return func(err error) tea.Msg {
		if err != nil {
			return outputMsg{key: key, err: fmt.Errorf("attach: %w", err)}
		}
		return nil
	}
}
