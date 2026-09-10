package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/pasture"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := tea.Model(m), tea.Cmd(nil)
	pastureTick := false
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		model, cmd = m.updateWindowSize(msg)
	case pasture.TickMsg:
		if m.pasture.Visible() && msg.Generation == m.pasture.Generation() {
			pastureTick = true
			cmd = pasture.Tick(m.pasture.Generation())
		}
	case pasture.ControlExpiryMsg:
		m.pasture.ExpireControl(msg)
	case demoTickMsg:
		model, cmd = m.updateDemoTick(msg)
	case pollMsg:
		model, cmd = m.updatePoll(msg)
	case configMsg:
		model, cmd = m.updateConfig(msg)
	case validationMsg:
		model, cmd = m.updateValidation(msg)
	case outputMsg:
		model, cmd = m.updateOutput(msg)
	case tea.KeyMsg:
		model, cmd = m.updateKey(msg)
	}
	m.refreshFrame()
	if pastureTick {
		m.pasture.Tick(m.frame.pasture)
	}
	m.pasture.Sync(m.frame.pasture)
	if expiry := m.pasture.ControlExpiry(); expiry != nil {
		cmd = tea.Batch(cmd, expiry)
	}
	return model, cmd
}

func (m *Model) quit() (tea.Model, tea.Cmd) {
	m.stopAll()
	m.pasture.Close()
	return m, tea.Quit
}

func (m *Model) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	outputWasAtBottom := m.outputViewport.AtBottom()
	m.width, m.height = msg.Width, msg.Height
	m.resizeAddInputs()
	m.configureColumns()
	m.rebuildRows()
	m.configureOutputViewport(m.overlay.kind == overlayOutput && outputWasAtBottom)
	return m, nil
}

func (m *Model) updateDemoTick(msg demoTickMsg) (tea.Model, tea.Cmd) {
	message, ok := m.session.applyDemoTick(msg)
	if !ok {
		return m, nil
	}
	m.recordActivity(message)
	m.wakePollers()
	return m, demoTick(m.session.demoGeneration())
}

func (m *Model) updatePoll(msg pollMsg) (tea.Model, tea.Cmd) {
	if m.pollers[msg.result.Name].generation != msg.generation || m.targetIndex(msg.result.Name) < 0 {
		return m, nil
	}
	return m, m.applyStatus(msg.result.Name, msg.result.Status)
}

func (m *Model) updateConfig(msg configMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setNotice(noticeError, "Could not reload targets: "+msg.err.Error())
	} else {
		m.reconcile(msg.targets)
		if strings.HasPrefix(m.message, "Could not reload targets: ") {
			m.clearNotice()
		}
	}
	return m, tea.Batch(m.watchConfig(), m.readFocused())
}

func (m *Model) updateValidation(msg validationMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.validationGeneration {
		return m, nil
	}
	if msg.err != nil {
		m.setNotice(noticeError, "Could not validate target: "+msg.err.Error())
		return m, nil
	}
	verb, latest, err := "Added", []target.Target(nil), error(nil)
	if msg.original == "" {
		latest, err = m.session.store.add(msg.target)
	} else {
		verb = "Updated"
		latest, err = m.session.store.update(msg.original, msg.target)
	}
	if err != nil {
		m.setNotice(noticeError, "Could not save target: "+err.Error())
		return m, nil
	}
	m.reconcile(latest)
	result := label(msg.status)
	if msg.status.State == poll.OK {
		result = "Herdr " + msg.status.Version
	}
	m.setNotice(noticeSuccess, fmt.Sprintf("%s %s · %s", verb, msg.target.Name, result))
	m.statuses[msg.target.Name] = msg.status
	m.rebuildRows()
	return m, nil
}

func (m *Model) updateOutput(msg outputMsg) (tea.Model, tea.Cmd) {
	if msg.target != "" && (m.pollers[msg.target].generation != msg.generation || m.targetIndex(msg.target) < 0) {
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
			if m.output.clearLoading() && m.overlay.kind == overlayOutput {
				m.overlay = overlayState{}
			}
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
		m.output.show(msg.key, text)
		m.configureOutputViewport(wasAtBottom)
	}
	return m, nil
}

func (m *Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay.kind {
	case overlayOutput:
		return m.updateExpandedOutput(msg)
	case overlayAttach:
		return m.updateAttachOverlay(msg)
	case overlayHelp:
		return m.updateHelpOverlay(msg)
	case overlayDelete:
		return m.updateDeleteConfirmation(msg)
	case overlayAdd, overlayEdit:
		return m.updateInput(msg)
	case overlayTargets:
		return m.updateTargetManager(msg)
	case overlayNone:
		return m.updateDashboardKey(msg)
	}
	return m, nil
}

func (m *Model) updateAttachOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m.quit()
	}
	return m, nil
}

func (m *Model) updateHelpOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc", "q":
		m.overlay = overlayState{}
	case "ctrl+c":
		return m.quit()
	}
	return m, nil
}

func (m *Model) updateDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m.quit()
	case "t":
		m.openTargetManager()
		return m, nil
	case "?":
		m.overlay = overlayState{kind: overlayHelp}
		return m, nil
	case "r":
		if !m.session.isDemo() {
			m.refreshAll()
		}
		return m, nil
	case "p":
		return m.updateDemoControl()
	case "v":
		if m.pasture.Visible() {
			m.pasture.Close()
		} else {
			m.clearNotice()
			m.openPasture()
			if m.pasture.Visible() {
				return m, pasture.Tick(m.pasture.Generation())
			}
		}
		return m, nil
	}
	if m.pasture.Visible() {
		return m.updatePastureKey(msg)
	}
	return m.updateTableKey(msg)
}

func (m *Model) updateDemoControl() (tea.Model, tea.Cmd) {
	running, ok := m.session.toggleDemo()
	if !ok {
		return m, nil
	}
	if running {
		m.recordActivity("demo started")
		m.pasture.Announce("To work!")
		return m, demoTick(m.session.demoGeneration())
	}
	m.recordActivity("demo paused")
	m.pasture.Announce("Hold!")
	return m, nil
}
