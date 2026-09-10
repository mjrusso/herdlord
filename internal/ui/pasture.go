package ui

import (
	"errors"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mjrusso/herdlord/internal/pasture"
)

const pastureTooShortNotice = "Terminal is too short for pasture view."
const pastureTooNarrowNotice = "Terminal is too narrow for pasture view."

func (m *Model) pastureFitNotice(frame pasture.Frame) string {
	if m.width > 0 && frame.Viewport.Width < pasture.MinimumViewportWidth {
		return pastureTooNarrowNotice
	}
	if m.height > 0 && frame.Viewport.Height < pasture.MinimumViewportHeight {
		return pastureTooShortNotice
	}
	return ""
}

func (m *Model) openPasture() {
	// The pasture hides the inspector. Measure with those rows free.
	frame := m.frameWithViews(m.frame.health, "").pasture
	if notice := m.pastureFitNotice(frame); notice != "" {
		m.setNotice(noticeInfo, notice)
		return
	}
	if err := m.pasture.Open(frame); err != nil {
		switch {
		case errors.Is(err, pasture.ErrNoTargets):
			m.setNotice(noticeInfo, "No targets configured.")
		case errors.Is(err, pasture.ErrViewportTooNarrow):
			m.setNotice(noticeInfo, pastureTooNarrowNotice)
		default:
			m.setNotice(noticeError, "Could not load Kitty pasture assets: "+err.Error())
		}
	}
}

func (m *Model) togglePastureRenderer() {
	err := m.pasture.ToggleRenderer()
	switch {
	case errors.Is(err, pasture.ErrKittyUnavailable):
		m.setNotice(noticeError, "Kitty graphics are not available. Set HERDLORD_KITTY=1 to force them.")
	case err != nil:
		m.setNotice(noticeError, "Could not load Kitty pasture assets: "+err.Error())
	}
}

func (m *Model) updatePastureKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "R" {
		m.togglePastureRenderer()
		return m, nil
	}
	frame := m.frame.pasture
	keys := navigationKeyMap()
	switch {
	case key.Matches(msg, keys.LineUp):
		m.pasture.Scroll(frame, -1)
	case key.Matches(msg, keys.LineDown):
		m.pasture.Scroll(frame, 1)
	case key.Matches(msg, keys.PageUp):
		m.pasture.ScrollPage(frame, -1)
	case key.Matches(msg, keys.PageDown):
		m.pasture.ScrollPage(frame, 1)
	case key.Matches(msg, keys.HalfPageUp):
		m.pasture.ScrollHalfPage(frame, -1)
	case key.Matches(msg, keys.HalfPageDown):
		m.pasture.ScrollHalfPage(frame, 1)
	case key.Matches(msg, keys.GotoTop):
		m.pasture.ScrollHome(frame)
	case key.Matches(msg, keys.GotoBottom):
		m.pasture.ScrollEnd(frame)
	}
	return m, nil
}
