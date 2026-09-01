package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
	"github.com/mjrusso/herdlord/internal/targetmgr"
)

type row struct {
	target string
	agent  *herdr.Agent
	values []string
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

func (s pollSender) Send(msg tea.Msg) {
	result, ok := msg.(poll.Result)
	if ok {
		s.program.Send(pollMsg{result: result, generation: s.generation})
	}
}

type outputMsg struct {
	key        string
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
	configPath           string
	manager              poll.Manager
	program              *tea.Program
	cancels              map[string]context.CancelFunc
	refresh              map[string]chan struct{}
	generations          map[string]uint64
	overlay              overlayState
	validationGeneration uint64
	message              string
	messageKind          noticeKind
	refreshPending       map[string]bool
	refreshTotal         int
	width                int
	height               int
	output               string
	outputKey            string
	outputLoading        bool
	outputViewport       viewport.Model
	loadingKey           string
	loadingRev           int64
	inflight             map[string]int64
	outputs              map[string]cachedOutput
	showInspector        bool
	targetCursor         int
	editTarget           string
	layout               tableLayout
}

func New(targets []target.Target, configPath string, manager poll.Manager) *Model {
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
	m := &Model{table: t, addInputs: inputs, targets: targets, configPath: configPath, manager: manager, statuses: map[string]poll.TargetStatus{}, cancels: map[string]context.CancelFunc{}, refresh: map[string]chan struct{}{}, refreshPending: map[string]bool{}, generations: map[string]uint64{}, outputs: map[string]cachedOutput{}, inflight: map[string]int64{}}
	m.outputViewport = viewport.New(1, 1)
	m.configureColumns()
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
	return m.watchConfig()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		outputWasAtBottom := m.outputViewport.AtBottom()
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		m.configureOutputViewport(m.overlay.kind == overlayOutput && outputWasAtBottom)
	case poll.Result:
		m.statuses[msg.Name] = msg.Status
		m.updateRefreshProgress(msg.Name)
		m.rebuildRows()
		return m, m.readFocused()
	case pollMsg:
		if m.generations[msg.result.Name] != msg.generation || m.targetIndex(msg.result.Name) < 0 {
			return m, nil
		}
		m.statuses[msg.result.Name] = msg.result.Status
		m.updateRefreshProgress(msg.result.Name)
		m.rebuildRows()
		return m, m.readFocused()
	case configMsg:
		if msg.err != nil {
			m.setNotice(noticeError, "Could not reload targets: "+msg.err.Error())
		} else {
			m.reconcile(msg.targets)
			if strings.HasPrefix(m.message, "Could not reload targets: ") {
				m.clearNotice()
			}
		}
		return m, tea.Batch(m.watchConfig(), m.readFocused())
	case validationMsg:
		if msg.generation != m.validationGeneration {
			return m, nil
		}
		if msg.err != nil {
			m.setNotice(noticeError, "Could not validate target: "+msg.err.Error())
			return m, nil
		}
		manager := targetmgr.Manager{Poller: m.manager}
		verb := "Added"
		if msg.original == "" {
			if err := manager.Add(m.configPath, msg.target); err != nil {
				m.setNotice(noticeError, "Could not add target: "+err.Error())
				return m, nil
			}
		} else {
			_, _, err := manager.Update(context.Background(), m.configPath, msg.original, func(configured *target.Target) error {
				paused := configured.Paused
				*configured = msg.target
				configured.Paused = paused
				return nil
			}, false)
			if err != nil {
				m.setNotice(noticeError, "Could not edit target: "+err.Error())
				return m, nil
			}
			verb = "Updated"
		}
		latest, err := target.Load(m.configPath)
		if err != nil {
			m.setNotice(noticeError, "Could not reload targets: "+err.Error())
			return m, nil
		}
		m.reconcile(latest)
		if msg.status.State == poll.OK {
			m.setNotice(noticeSuccess, fmt.Sprintf("%s %s · Herdr %s", verb, msg.target.Name, msg.status.Version))
		} else {
			m.setNotice(noticeSuccess, fmt.Sprintf("%s %s · %s", verb, msg.target.Name, label(msg.status)))
		}
		m.statuses[msg.target.Name] = msg.status
		m.rebuildRows()
	case outputMsg:
		if msg.target != "" && (m.generations[msg.target] != msg.generation || m.targetIndex(msg.target) < 0) {
			return m, nil
		}
		if revision, ok := m.inflight[msg.key]; ok && revision == msg.revision {
			delete(m.inflight, msg.key)
		}
		if msg.err != nil {
			if msg.target != "" && !m.isCurrentOutputRequest(msg.key, msg.revision) {
				return m, nil
			}
			if m.isCurrentOutputRequest(msg.key, msg.revision) {
				initialLoad := m.outputLoading
				m.outputLoading = false
				m.loadingKey, m.loadingRev = "", 0
				if initialLoad {
					if m.overlay.kind == overlayOutput {
						m.overlay = overlayState{}
					}
					m.outputKey, m.output = "", ""
				}
				m.updateTableHeight()
			}
			m.setNotice(noticeError, "Could not read recent output: "+msg.err.Error())
			return m, nil
		}
		if cached, ok := m.outputs[msg.key]; ok && cached.revision > msg.revision {
			return m, nil
		}
		text := strings.TrimSpace(display.Block(msg.text))
		m.outputs[msg.key] = cachedOutput{revision: msg.revision, text: text}
		if m.isCurrentOutputRequest(msg.key, msg.revision) {
			wasAtBottom := m.outputViewport.AtBottom()
			m.outputLoading = false
			m.loadingKey, m.loadingRev = "", 0
			m.outputKey, m.output = msg.key, text
			m.updateTableHeight()
			m.configureOutputViewport(wasAtBottom)
		}
	case tea.KeyMsg:
		if m.overlay.kind == overlayOutput {
			return m.updateExpandedOutput(msg)
		}
		if m.overlay.kind == overlayAttach {
			switch msg.String() {
			case "enter":
				targetName, paneID := m.overlay.target, m.overlay.pane
				m.overlay = overlayState{}
				focused := m.focused()
				if focused == nil || focused.agent == nil || focused.target != targetName || focused.agent.PaneID != paneID {
					m.setNotice(noticeError, "The selected agent is no longer available")
					return m, nil
				}
				return m, m.attachFocused()
			case "q", "esc":
				m.overlay = overlayState{}
			case "ctrl+c":
				m.stopAll()
				return m, tea.Quit
			}
			return m, nil
		}
		if m.overlay.kind == overlayHelp {
			switch msg.String() {
			case "?", "esc", "q":
				m.overlay = overlayState{}
			case "ctrl+c":
				m.stopAll()
				return m, tea.Quit
			}
			return m, nil
		}
		if m.overlay.kind == overlayDelete {
			return m.updateDeleteConfirmation(msg)
		}
		if m.overlay.kind == overlayAdd || m.overlay.kind == overlayEdit {
			return m.updateInput(msg)
		}
		if m.overlay.kind == overlayTargets {
			return m.updateTargetManager(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.stopAll()
			return m, tea.Quit
		case "t":
			m.openTargetManager()
			return m, nil
		case "?":
			m.overlay = overlayState{kind: overlayHelp}
			return m, nil
		case "r":
			m.refreshAll()
		case "i":
			if focused := m.focused(); focused != nil && focused.agent != nil {
				m.showInspector = !m.showInspector
				m.updateTableHeight()
			}
		case "o":
			m.openExpandedOutput()
			return m, nil
		case "enter":
			if focused := m.focused(); focused != nil && focused.agent != nil {
				m.overlay = overlayState{kind: overlayAttach, target: focused.target, pane: focused.agent.PaneID}
				return m, nil
			}
		}
		old := m.table.Cursor()
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		if old != m.table.Cursor() {
			m.updateSelectionMarkers()
			return m, tea.Batch(cmd, m.readFocused())
		}
		return m, cmd
	}
	return m, nil
}

func (m *Model) isCurrentOutputRequest(key string, revision int64) bool {
	return m.loadingKey == key && m.loadingRev == revision && m.focusKey() == key
}

func (m *Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.stopAll()
		return m, tea.Quit
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
	background := m.dashboardView()
	if m.overlay.kind == overlayHelp {
		return m.modalView(background, helpOverlay(len(m.targets) > 0, m.hasAgents()), 68)
	}
	if m.overlay.kind == overlayOutput {
		return m.modalView(background, m.expandedOutputView(), m.outputModalWidth())
	}
	if m.overlay.kind == overlayAdd || m.overlay.kind == overlayEdit {
		return m.modalView(background, m.addFormView(), 84)
	}
	if m.overlay.kind == overlayDelete {
		return m.modalView(background, m.deleteView(), 84)
	}
	if m.overlay.kind == overlayAttach {
		return m.modalView(background, m.attachView(), 64)
	}
	if m.overlay.kind == overlayTargets {
		return m.modalView(background, m.targetManagerView(), 84)
	}
	return background
}

func (m *Model) dashboardView() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render("herdlord")
	parts := []string{title}
	if len(m.targets) == 0 {
		parts = append(parts, "No targets configured\n\nAdd a local or remote Herdr session to begin.")
	} else {
		parts = append(parts, m.table.View())
	}
	if health := m.healthView(); health != "" {
		parts = append(parts, health)
	}
	if m.showInspector {
		if inspector := m.inspectorView(); inspector != "" {
			parts = append(parts, inspector)
		}
	}
	if m.message != "" {
		parts = append(parts, m.noticeView())
	}
	body := strings.Join(parts, "\n\n")
	footer := m.footerView()
	gap := 2
	if m.height > 0 && lipgloss.Height(body) < m.height {
		gap = m.height - lipgloss.Height(body)
	}
	return body + strings.Repeat("\n", gap) + footer
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

func (m *Model) hasAgents() bool {
	for i := range m.rows {
		if m.rows[i].agent != nil {
			return true
		}
	}
	return false
}
