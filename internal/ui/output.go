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
	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
)

type outputPane struct {
	key     fleet.AgentKey
	text    string
	loading bool
	loadKey fleet.AgentKey
	loadRev int64
}

func (p *outputPane) clear() {
	*p = outputPane{}
}

func (p *outputPane) show(key fleet.AgentKey, text string) {
	*p = outputPane{key: key, text: text}
}

func (p *outputPane) load(key fleet.AgentKey, revision int64) {
	*p = outputPane{loading: true, loadKey: key, loadRev: revision}
}

func (p *outputPane) await(key fleet.AgentKey, revision int64, text string) {
	*p = outputPane{key: key, text: text, loadKey: key, loadRev: revision}
}

func (p *outputPane) clearLoading() (wasInitialLoad bool) {
	wasInitialLoad = p.loading
	p.loading = false
	p.loadKey, p.loadRev = fleet.AgentKey{}, 0
	if wasInitialLoad {
		p.key, p.text = fleet.AgentKey{}, ""
	}
	return wasInitialLoad
}

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

func (m *Model) inspectorView(healthHeight int) string {
	r := m.focused()
	if r == nil || r.agent == nil {
		return ""
	}
	width := m.viewportWidth()
	contentWidth := max(1, width-4)
	compact := m.compactInspector()
	details := m.detailsView()
	if compact {
		a := r.agent
		details = ansi.Truncate(fmt.Sprintf("Workspace %s  Tab %s  Pane %s",
			relationship(a.Workspace, a.WorkspaceID), relationship(a.Tab, a.TabID), displayLabel(a.PaneID)), contentWidth, "…")
	}
	parts := []string{
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(
			fmt.Sprintf("Agent inspector  %s / %s / %s", displayLabel(r.target), displayLabel(r.agent.Agent), displayLabel(agentStatusLabel(r.agent.Status))),
		),
		ansi.Wrap(details, contentWidth, ""),
	}
	if m.output.loading {
		if compact {
			parts = append(parts, "Loading recent output…")
		} else {
			parts = append(parts, "", lipgloss.NewStyle().Bold(true).Render("Recent output"), "Loading recent output…")
		}
	} else if m.output.key != (fleet.AgentKey{}) {
		output := truncateLines(m.output.text, m.outputLineLimit(healthHeight))
		if output == "" {
			empty := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("No recent output")
			if compact {
				output = lipgloss.NewStyle().Bold(true).Render("Recent output") + "  " + empty
			} else {
				output = empty
			}
		}
		if compact {
			parts = append(parts, truncateBlock(output, contentWidth))
		} else {
			parts = append(parts, "", lipgloss.NewStyle().Bold(true).Render("Recent output"), truncateBlock(output, contentWidth))
		}
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
	key := m.focusKey()
	revision := int64(0)
	if key != (fleet.AgentKey{}) {
		revision = m.focused().agent.Revision
	}
	m.output = m.cachedFocusedOutput(key, revision)
	m.closeOutputOverlayWithoutFocus(key)
}

func (m *Model) clearMissingAgentOutputs(targetName string, status poll.TargetStatus) {
	if !status.State.Usable() {
		return
	}
	present := make(map[string]bool, len(status.Agents))
	for _, agent := range status.Agents {
		present[agent.PaneID] = true
	}
	m.deleteOutputs(func(key fleet.AgentKey) bool {
		return key.Target == targetName && !present[key.Pane]
	})
}

func (m *Model) deleteOutputs(matches func(fleet.AgentKey) bool) {
	for key := range m.outputs {
		if matches(key) {
			delete(m.outputs, key)
		}
	}
	for key := range m.inflight {
		if matches(key) {
			delete(m.inflight, key)
		}
	}
}

func (m *Model) focused() *row {
	cursor := m.table.Cursor()
	if m.tableDirty {
		cursor = m.tableCursor
	}
	if cursor < 0 || cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[cursor]
}

func (m *Model) focusKey() fleet.AgentKey {
	r := m.focused()
	if r == nil || r.agent == nil {
		return fleet.AgentKey{}
	}
	return fleet.NewAgentKey(r.target, r.agent.PaneID)
}

func (m *Model) readFocused() tea.Cmd {
	key := m.focusKey()
	if key == (fleet.AgentKey{}) {
		m.output.clear()
		m.closeOutputOverlayWithoutFocus(key)
		return nil
	}
	r := m.focused()
	revision := r.agent.Revision
	live := r.agent.Status == "working"
	if output := m.cachedFocusedOutput(key, revision); output.key != (fleet.AgentKey{}) && !live {
		m.output = output
		return nil
	}
	cached, hasCached := m.outputs[key]
	if live && hasCached {
		m.output.await(key, revision, cached.text)
	} else {
		m.output.load(key, revision)
	}
	m.closeOutputOverlayWithoutFocus(key)
	if inflightRevision, ok := m.inflight[key]; ok && inflightRevision == revision {
		return nil
	}
	t, ok := m.findTarget(r.target)
	if !ok {
		m.output.clear()
		return nil
	}
	s := m.statuses[r.target]
	paneID := r.agent.PaneID
	generation := m.pollers[r.target].generation
	m.inflight[key] = revision
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), m.manager.EffectiveTimeout())
		defer cancel()
		out, err := m.manager.Client.Read(ctx, t, s.HerdrPath, paneID, 120)
		return outputMsg{key: key, target: t.Name, generation: generation, revision: revision, text: out, err: err}
	}
}

func (m *Model) cachedFocusedOutput(key fleet.AgentKey, revision int64) outputPane {
	cached, ok := m.outputs[key]
	if !ok || cached.revision != revision {
		return outputPane{}
	}
	return outputPane{key: key, text: cached.text}
}

func (m *Model) closeOutputOverlayWithoutFocus(key fleet.AgentKey) {
	if key == (fleet.AgentKey{}) && m.overlay.kind == overlayOutput {
		m.overlay = overlayState{}
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
	content := m.output.text
	if content == "" && !m.output.loading {
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
		return m.quit()
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
	if m.output.loading {
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

func attachResult(key fleet.AgentKey) func(error) tea.Msg {
	return func(err error) tea.Msg {
		if err != nil {
			return outputMsg{key: key, err: fmt.Errorf("attach: %w", err)}
		}
		return nil
	}
}

func (m *Model) isCurrentOutputRequest(key fleet.AgentKey, revision int64) bool {
	return m.output.loadKey == key && m.output.loadRev == revision && m.focusKey() == key
}
