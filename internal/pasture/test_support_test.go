package pasture

import (
	"testing"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

type testModel struct {
	state    *State
	targets  []target.Target
	statuses map[string]poll.TargetStatus
	width    int
	height   int
}

func newTestModel(targets []target.Target) *testModel {
	return newTestModelWithRenderer(targets, Kitty)
}

func newTestModelWithRenderer(targets []target.Target, renderer Renderer) *testModel {
	return &testModel{
		state:    New(renderer),
		targets:  append([]target.Target(nil), targets...),
		statuses: make(map[string]poll.TargetStatus),
		width:    80,
		height:   40,
	}
}

func (m *testModel) snapshot() fleet.Snapshot {
	return fleet.Snapshot{Targets: m.targets, Statuses: m.statuses}
}

func (m *testModel) viewport() Viewport {
	return Viewport{Width: m.width, Height: m.height}
}

func (m *testModel) layout(width int) grid {
	return buildGrid(width, m.targets, m.statuses, m.viewport().Height, m.state.scrollRow)
}

func (m *testModel) buildSceneForTest(width int, layout grid) Scene {
	return m.state.buildScene(m.snapshot(), width, m.viewport().Height, layout)
}

func (m *testModel) syncState(width int) {
	layout := m.layout(width)
	m.state.syncLayout(m.snapshot(), layout.penWidth, layout.heights)
}

func (m *testModel) advanceState(snapshot fleet.Snapshot) {
	layout := m.layout(contentWidth(m.width))
	m.state.syncLayout(snapshot, layout.penWidth, layout.heights)
	m.state.advance(snapshot, layout)
}

func (m *testModel) toggleRenderer() error {
	return m.state.ToggleRenderer()
}

func testView(m *testModel) string {
	width := contentWidth(m.width)
	layout := m.layout(width)
	return m.state.renderer.render(m.buildSceneForTest(width, layout))
}

func physicsModel(t *testing.T, agents []herdr.Agent) *testModel {
	t.Helper()
	m := newTestModel([]target.Target{{Name: "local"}})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: agents}
	m.syncState(contentWidth(m.width))
	for key, current := range m.state.sheep {
		current.phase = present
		m.state.sheep[key] = current
	}
	return m
}
