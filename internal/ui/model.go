package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/demo"
	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/pasture"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
	"github.com/mjrusso/herdlord/internal/targetmgr"
)

type row struct {
	target string
	agent  *herdr.Agent
	status string
	detail string
}

type validationMsg struct {
	generation uint64
	target     target.Target
	original   string
	status     poll.TargetStatus
	err        error
}

type overlayKind uint8

const (
	overlayNone overlayKind = iota
	overlayHelp
	overlayOutput
	overlayTargets
	overlayAdd
	overlayEdit
	overlayDelete
	overlayAttach
)

type overlayState struct {
	kind   overlayKind
	target string
	pane   string
}

type configMsg struct {
	targets []target.Target
	err     error
}

type pollMsg struct {
	result     poll.Result
	generation uint64
}

type pollSender struct {
	program    *tea.Program
	generation uint64
}

type poller struct {
	cancel     context.CancelFunc
	refresh    chan struct{}
	generation uint64
}

func (s pollSender) Send(msg tea.Msg) {
	result, ok := msg.(poll.Result)
	if ok {
		s.program.Send(pollMsg{result: result, generation: s.generation})
	}
}

type outputMsg struct {
	key        fleet.AgentKey
	target     string
	generation uint64
	revision   int64
	text       string
	err        error
}

type cachedOutput struct {
	revision int64
	text     string
}

type noticeKind int

const (
	noticeInfo noticeKind = iota
	noticeSuccess
	noticeError
)

type Model struct {
	table                table.Model
	addInputs            [3]textinput.Model
	addFocus             int
	targets              []target.Target
	statuses             map[string]poll.TargetStatus
	rows                 []row
	manager              poll.Manager
	session              session
	program              *tea.Program
	pollers              map[string]poller
	pollerGeneration     uint64
	overlay              overlayState
	validationGeneration uint64
	message              string
	messageKind          noticeKind
	refreshPending       map[string]bool
	refreshTotal         int
	width, height        int
	frame                frame
	output               outputPane
	outputViewport       viewport.Model
	inflight             map[fleet.AgentKey]int64
	outputs              map[fleet.AgentKey]cachedOutput
	showInspector        bool
	pasture              *pasture.State
	tableCursor          int
	tableDirty           bool
	targetCursor         int
	editTarget           string
	layout               tableLayout
	activityLog
}

func (m *Model) viewportWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func New(targets []target.Target, configPath string, manager poll.Manager, renderer pasture.Renderer) *Model {
	return newModel(targets, manager, liveSession(fileTargetStore{path: configPath, manager: manager}), renderer)
}

func newModel(targets []target.Target, manager poll.Manager, currentSession session, renderer pasture.Renderer) *Model {
	columns := []table.Column{
		{Title: "TARGET", Width: 18},
		{Title: "AGENT", Width: 12},
		{Title: "STATUS", Width: 13},
		{Title: "TITLE", Width: 56},
	}
	t := table.New(table.WithColumns(columns), table.WithFocused(true), table.WithHeight(12))
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).Foreground(lipgloss.Color("6"))
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6")).Bold(false)
	t.SetStyles(styles)
	inputs := [3]textinput.Model{textinput.New(), textinput.New(), textinput.New()}
	for i := range inputs {
		inputs[i].CharLimit = 512
		inputs[i].Prompt = ""
		inputs[i].Width = 56
	}
	inputs[0].Placeholder = "workbox"
	inputs[1].Placeholder = "ssh workbox --"
	inputs[2].Placeholder = "ssh -t workbox --"
	t.KeyMap = navigationKeyMap()
	m := &Model{table: t, addInputs: inputs, targets: targets, manager: manager, session: currentSession, statuses: map[string]poll.TargetStatus{}, pollers: map[string]poller{}, refreshPending: map[string]bool{}, outputs: map[fleet.AgentKey]cachedOutput{}, inflight: map[fleet.AgentKey]int64{}, pasture: pasture.New(renderer), tableCursor: t.Cursor()}
	m.outputViewport = viewport.New(1, 1)
	m.configureColumns()
	m.refreshFrame()
	return m
}

func NewDemo(interval, timeout time.Duration, renderer pasture.Renderer) *Model {
	client := demo.New()
	manager := poll.Manager{Client: client, Interval: interval, Timeout: timeout}
	m := newModel(client.Targets(), manager, demoSession(client), renderer)
	m.recordActivity("demo ready")
	m.openPasture()
	return m
}

func (m *Model) SetProgram(p *tea.Program) { m.program = p }

func (m *Model) Init() tea.Cmd {
	for _, t := range m.targets {
		if t.Paused {
			m.statuses[t.Name] = poll.TargetStatus{State: poll.Paused}
		} else {
			m.statuses[t.Name] = poll.TargetStatus{State: poll.Checking}
			m.start(t)
		}
	}
	m.rebuildRows()
	m.refreshFrame()
	m.pasture.Sync(m.frame.pasture)
	var commands []tea.Cmd
	if m.session.store.watchable() {
		commands = append(commands, m.watchConfig())
	}
	if m.pasture.Visible() {
		commands = append(commands, pasture.Tick(m.pasture.Generation()))
	}
	if expiry := m.pasture.ControlExpiry(); expiry != nil {
		commands = append(commands, expiry)
	}
	if m.session.demoRunning() {
		commands = append(commands, demoTick(m.session.demoGeneration()))
	}
	return tea.Batch(commands...)
}

func (m *Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.overlay, m.editTarget = overlayState{kind: overlayTargets}, ""
		m.clearNotice()
		for i := range m.addInputs {
			m.addInputs[i].Blur()
		}
		return m, nil
	case "tab", "down":
		m.focusAddInput((m.addFocus + 1) % len(m.addInputs))
		return m, textinput.Blink
	case "shift+tab", "up":
		m.focusAddInput((m.addFocus + len(m.addInputs) - 1) % len(m.addInputs))
		return m, textinput.Blink
	case "enter":
		return m.finishAdd()
	}
	var cmd tea.Cmd
	m.addInputs[m.addFocus], cmd = m.addInputs[m.addFocus].Update(msg)
	return m, cmd
}

func (m *Model) openAddForm() {
	m.overlay, m.editTarget, m.addFocus = overlayState{kind: overlayAdd}, "", 0
	m.clearNotice()
	for i := range m.addInputs {
		m.addInputs[i].SetValue("")
		m.addInputs[i].Blur()
	}
	m.addInputs[0].Focus()
}

func (m *Model) openEditForm(configured target.Target) {
	m.overlay, m.editTarget, m.addFocus = overlayState{kind: overlayEdit}, configured.Name, 0
	m.clearNotice()
	values := []string{configured.Name, formatPrefix(configured.Prefix), formatPrefix(configured.Interactive)}
	for i := range m.addInputs {
		m.addInputs[i].SetValue(values[i])
		m.addInputs[i].Blur()
	}
	m.addInputs[0].Focus()
}

func (m *Model) focusAddInput(index int) {
	for i := range m.addInputs {
		m.addInputs[i].Blur()
	}
	m.addFocus = index
	m.addInputs[index].Focus()
}

func (m *Model) addFormView() string {
	labels := []string{"Name", "Command prefix", "Attach prefix"}
	help := [][]string{
		{"A unique label for this Herdr session."},
		{
			"Runs before non-interactive Herdr commands. Empty is local.",
			"SSH example:   `ssh workbox --`",
			"Voom example:  `voom ssh play --`",
		},
		{
			"Empty uses command prefix. Add transport TTY flags when needed.",
			"SSH example:   `ssh -t workbox --`",
			"Voom example:  `voom ssh play --`",
		},
	}
	var body strings.Builder
	title := "Add target"
	if m.overlay.kind == overlayEdit {
		title = "Edit target"
	}
	body.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
	body.WriteString("\n\n")
	for i := range m.addInputs {
		if i > 0 {
			body.WriteString("\n")
		}
		marker := "  "
		if i == m.addFocus {
			marker = "> "
		}
		body.WriteString(marker + lipgloss.NewStyle().Bold(true).Render(labels[i]) + "\n")
		body.WriteString("  " + m.addInputs[i].View() + "\n")
		for _, line := range help[i] {
			width := 74
			if m.width > 0 {
				width = max(12, min(74, m.width-12))
			}
			wrapped := strings.ReplaceAll(ansi.Wrap(line, width, " "), "\n", "\n    ")
			body.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(wrapped) + "\n")
		}
	}
	if m.message != "" {
		body.WriteString("\n" + m.noticeView() + "\n")
	}
	shortcuts := hints(
		hint("Tab/Shift-Tab", "move"),
		hint("Enter", "validate and save"),
		hint("Esc", "cancel"),
		hint("Ctrl-C", "quit"),
	)
	body.WriteString("\n" + shortcuts)
	return body.String()
}

func (m *Model) resizeAddInputs() {
	width := 56
	if m.width > 0 {
		width = max(12, min(64, m.width-4))
	}
	for i := range m.addInputs {
		m.addInputs[i].Width = width
	}
}

func (m *Model) finishAdd() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.addInputs[0].Value())
	if name == "" || (name != m.editTarget && m.targetIndex(name) >= 0) {
		m.setNotice(noticeError, "Enter a unique target name.")
		m.focusAddInput(0)
		return m, nil
	}
	prefix, err := target.ParsePrefix(m.addInputs[1].Value())
	if err != nil {
		m.setNotice(noticeError, "Command prefix: "+err.Error())
		m.focusAddInput(1)
		return m, nil
	}
	interactive, err := target.ParsePrefix(m.addInputs[2].Value())
	if err != nil {
		m.setNotice(noticeError, "Attach prefix: "+err.Error())
		m.focusAddInput(2)
		return m, nil
	}
	if strings.TrimSpace(m.addInputs[2].Value()) == "" {
		interactive = nil
	}
	t := target.Target{Name: name, Prefix: prefix, Interactive: interactive}
	m.overlay = overlayState{kind: overlayTargets}
	m.validationGeneration++
	generation := m.validationGeneration
	for i := range m.addInputs {
		m.addInputs[i].Blur()
	}
	m.setNotice(noticeInfo, "Validating "+t.Name+"…")
	manager := targetmgr.Manager{Poller: m.manager}
	original := m.editTarget
	m.editTarget = ""
	return m, func() tea.Msg {
		status, err := manager.Check(context.Background(), t)
		return validationMsg{generation: generation, target: t, original: original, status: status, err: err}
	}
}

func (m *Model) View() string {
	frame := m.frame
	display := m.pasture.Display(frame.pasture)
	background := m.dashboardView(frame, display)
	view := background
	switch m.overlay.kind {
	case overlayHelp:
		view = m.modalView(background, m.helpOverlay(), 68)
	case overlayOutput:
		view = m.modalView(background, m.expandedOutputView(), m.outputModalWidth())
	case overlayAdd, overlayEdit:
		view = m.modalView(background, m.addFormView(), 84)
	case overlayDelete:
		view = m.modalView(background, m.deleteView(), 84)
	case overlayAttach:
		view = m.modalView(background, m.attachView(), 64)
	case overlayTargets:
		view = m.modalView(background, m.targetManagerView(), 84)
	}
	control := m.pasture.Control()
	return control + view
}

func (m *Model) dashboardView(frame frame, pastureDisplay pasture.Display) string {
	var parts []string
	if len(m.targets) == 0 {
		parts = append(parts, "No targets configured\n\nAdd a local or remote Herdr session to begin.")
	} else if m.pasture.Visible() {
		parts = append(parts, pastureDisplay.View)
	} else {
		parts = append(parts, m.table.View())
	}
	if frame.health != "" {
		parts = append(parts, frame.health)
	}
	if frame.inspector != "" {
		parts = append(parts, frame.inspector)
	}
	if m.message != "" {
		parts = append(parts, m.dashboardNoticeView())
	}
	if frame.showActivity {
		parts = append(parts, m.activityView())
	}
	body := strings.Join(parts, "\n\n")
	footer := m.footerView(pastureDisplay.PageLabel)
	gap := dashboardGapRows
	if m.height > 0 && lipgloss.Height(body) < m.height {
		gap = max(minimumDashboardGapRows, m.height-lipgloss.Height(body))
	}
	return body + strings.Repeat("\n", gap) + footer
}

func (m *Model) recordActivity(message string) {
	if message == "" {
		return
	}
	m.add(display.Text(message))
}

func (m *Model) activityLabel() string {
	if !m.session.isDemo() {
		return "Activity"
	}
	if m.session.demoRunning() {
		return "Activity  DEMO · RUNNING"
	}
	return "Activity  DEMO · PAUSED"
}

func (m *Model) demoControl() string {
	if m.session.demoRunning() {
		return "pause"
	}
	return "start"
}

func (m *Model) activityView() string {
	width := m.viewportWidth()
	label := m.activityLabel()
	label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(label)
	events := "No activity yet"
	first := m.activitySequence - len(m.activity) + 1
	if len(m.activity) > 0 {
		items := make([]string, 0, len(m.activity))
		for i := len(m.activity) - 1; i >= 0; i-- {
			items = append(items, fmt.Sprintf("#%03d %s", first+i, m.activity[i]))
		}
		events = strings.Join(items, "  ·  ")
	}
	line := ansi.Truncate(label+"  "+events, max(1, width-4), "…")
	return lipgloss.NewStyle().
		Width(max(1, width-2)).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Render(line)
}

func (m *Model) setNotice(kind noticeKind, message string) {
	m.messageKind, m.message = kind, display.Text(message)
}

func (m *Model) clearNotice() {
	m.messageKind, m.message = noticeInfo, ""
}

func (m *Model) noticeView() string {
	label, color := "Status", lipgloss.Color("6")
	switch m.messageKind {
	case noticeSuccess:
		label, color = "Success", lipgloss.Color("2")
	case noticeError:
		label, color = "Error", lipgloss.Color("1")
	}
	return lipgloss.NewStyle().Bold(true).Foreground(color).Render(label+":") + " " + m.message
}

func (m *Model) dashboardNoticeView() string {
	if m.messageKind != noticeError {
		return m.noticeView()
	}
	width := m.viewportWidth()
	color := lipgloss.Color("1")
	body := lipgloss.NewStyle().Bold(true).Foreground(color).Render("Error") + "\n" +
		wrappedDashboardError(m.message, max(1, width-4))
	return lipgloss.NewStyle().
		Width(max(1, width-2)).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Render(body)
}

func wrappedDashboardError(value string, width int) string {
	const limit = 3

	lines := strings.Split(ansi.Wrap(value, width, " "), "\n")
	if len(lines) <= limit {
		return strings.Join(lines, "\n")
	}
	lines = lines[:limit]
	if width == 1 {
		lines[limit-1] = "…"
	} else {
		lines[limit-1] = ansi.Truncate(lines[limit-1], width-1, "") + "…"
	}
	return strings.Join(lines, "\n")
}

func (m *Model) hasAgents() bool {
	for i := range m.rows {
		if m.rows[i].agent != nil {
			return true
		}
	}
	return false
}
