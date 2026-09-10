package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/pasture"
)

const (
	activityRailRows        = 3
	minimumContentRows      = 3
	dashboardGapRows        = 2
	minimumDashboardGapRows = 1
	footerRows              = 1
	panelGapRows            = 1
)

type frame struct {
	pasture      pasture.Frame
	health       string
	inspector    string
	showActivity bool
}

func (m *Model) measureFrame() frame {
	health := m.healthView()
	healthHeight := renderedHeight(health)
	inspector := ""
	if m.showInspector && !m.pasture.Visible() {
		inspector = m.inspectorView(healthHeight)
	}
	return m.frameWithViews(health, inspector)
}

func (m *Model) frameWithViews(health, inspector string) frame {
	contentHeight, activity := m.measureContentHeight(renderedHeight(health), renderedHeight(inspector))
	return frame{
		pasture: pasture.Frame{
			Snapshot: fleet.Snapshot{Targets: m.targets, Statuses: m.statuses},
			Viewport: pasture.Viewport{
				Width:  m.viewportWidth(),
				Height: contentHeight,
			},
		},
		health: health, inspector: inspector, showActivity: activity,
	}
}

func (m *Model) refreshFrame() {
	m.frame = m.measureFrame()
	if notice := m.pastureFitNotice(m.frame.pasture); m.pasture.Visible() && notice != "" {
		m.pasture.Close()
		m.setNotice(noticeInfo, notice)
		m.frame = m.measureFrame()
	}
	if m.pasture.Visible() {
		return
	}
	m.table.SetHeight(m.frame.pasture.Viewport.Height)
	if m.tableDirty {
		m.setTableRows(m.tableCursor)
	}
}

func hint(keys, action string) string {
	return lipgloss.NewStyle().Bold(true).Render(keys) + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(action)
}

func hints(items ...string) string {
	return strings.Join(items, "  ")
}

func (m *Model) footerView(pasturePageLabel string) string {
	if len(m.targets) == 0 {
		return hints(hint("t", "targets"), hint("?", "help"), hint("q", "quit"))
	}
	width := m.width
	if width <= 0 {
		width = math.MaxInt
	}
	var items []string
	focusedAgent := false
	if m.pasture.Visible() {
		if pasturePageLabel != "" && width >= 80 {
			items = append(items, pasturePageLabel)
		}
		items = append(items, hint("↑/↓", "pens"))
	} else {
		navigation := "navigate"
		if width < 100 {
			navigation = "nav"
		}
		items = append(items, hint("↑/↓", navigation))
		if focused := m.focused(); focused != nil && focused.agent != nil {
			focusedAgent = true
			if !m.session.isDemo() && width >= 60 {
				items = append(items, hint("enter", "attach"))
			}
			if width >= 80 {
				items = append(items, hint("o", "output"), hint("i", "inspect"))
			}
		}
	}
	if m.session.isDemo() {
		items = append(items, hint("p", m.demoControl()))
	}
	view := "pasture"
	if m.pasture.Visible() {
		view = "table"
	}
	items = append(items, hint("v", view))
	if m.pasture.Visible() && width >= 60 {
		items = append(items, hint("R", "renderer"))
	}
	if !m.pasture.Visible() || width >= 50 {
		items = append(items, hint("t", "targets"))
	}
	if !m.session.isDemo() && (!focusedAgent || width >= 100) && (!m.pasture.Visible() || width >= 80) {
		items = append(items, hint("r", "refresh"))
	}
	footer := hints(append(items, hint("?", "help"), hint("q", "quit"))...)
	return ansi.Truncate(footer, width, "")
}

func (m *Model) outputLineLimit(healthHeight int) int {
	limit := m.height - 16
	if m.width > 0 && m.width < 80 {
		limit--
	}
	limit -= healthHeight
	if m.compactInspector() {
		return min(4, max(3, limit))
	}
	return min(36, max(3, limit))
}

func (m *Model) compactInspector() bool {
	return m.height > 0 && m.height < 28
}

func renderedHeight(view string) int {
	if view == "" {
		return 0
	}
	return lipgloss.Height(view)
}

func (m *Model) measureContentHeight(healthHeight, inspectorHeight int) (int, bool) {
	reserved := dashboardGapRows + footerRows
	if healthHeight > 0 {
		reserved += healthHeight + panelGapRows
	}
	if inspectorHeight > 0 {
		reserved += inspectorHeight + panelGapRows
	}
	if m.overlay.kind != overlayNone {
		reserved += activityRailRows
	} else if m.message != "" {
		reserved += lipgloss.Height(m.dashboardNoticeView()) + panelGapRows
	}
	activity := m.height <= 0 || m.height+dashboardGapRows-minimumDashboardGapRows >= reserved+activityRailRows+minimumContentRows
	if activity {
		reserved += activityRailRows
	}
	return max(minimumContentRows, m.height-reserved), activity
}
