package pasture

import (
	"fmt"
	"testing"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func TestPastureSceneContainsOnlyVisiblePens(t *testing.T) {
	targets := make([]target.Target, 5)
	m := newTestModel(targets)
	for i := range targets {
		targets[i].Name = fmt.Sprintf("field-%d", i)
		m.statuses[targets[i].Name] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "idle"}}}
	}
	m.targets = targets
	m.width, m.height = 80, 40
	width := contentWidth(m.width)
	grid := m.layout(width)
	if grid.visibleRows >= len(grid.rowHeights) {
		t.Fatal("test setup did not create an off-screen row")
	}
	scene := m.buildSceneForTest(width, grid)
	penCount := 0
	for _, row := range scene.rows {
		penCount += len(row.pens)
	}
	want := min(len(targets), grid.visibleRows*grid.columns)
	if penCount != want {
		t.Fatalf("scene contains %d pens, want %d visible pens", penCount, want)
	}
}

func TestSceneObjectIDsDoNotAliasDelimitedNames(t *testing.T) {
	first := sceneObjectID("agent", "a:b", "c")
	second := sceneObjectID("agent", "a", "b:c")
	if first == second {
		t.Fatal("scene object IDs alias names containing delimiters")
	}
}

func TestPastureSceneGeometryIsInternallyConsistent(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{
		{PaneID: "idle-a", Status: "idle"},
		{PaneID: "idle-b", Status: "idle"},
		{PaneID: "working", Status: "working"},
		{PaneID: "blocked", Status: "blocked"},
	})
	m.height = 50
	width := contentWidth(m.width)
	m.state.width = 0
	grid := m.layout(width)
	m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
	scene := m.buildSceneForTest(width, grid)
	ground := royalSceneRows - royalRoadRows
	if scene.castle.y+scene.castle.height != ground || scene.lord.position.y+lordRows != ground || scene.castle.x+scene.castle.width > scene.lord.position.x {
		t.Fatal("castle and lord are not grounded at the command station")
	}
	for _, row := range scene.rows {
		for index, pen := range row.pens {
			if pen.bounds.x < 0 || pen.bounds.x+pen.bounds.width > scene.width || pen.bounds.y < row.top || pen.bounds.y+pen.bounds.height > row.top+row.height {
				t.Fatalf("pen %q is outside its row", pen.name)
			}
			if row.roadCenters[index] != pen.bounds.x+gateCenter(pen.bounds.width) {
				t.Fatalf("pen %q gate does not meet its road", pen.name)
			}
			for _, sheep := range pen.sheep {
				if sheep.position.x < 0 || sheep.position.x+sheepColumns > pen.bounds.width || sheep.position.y < 0 || sheep.position.y+sheepRows > pen.bounds.height {
					t.Fatalf("actor %q is outside pen %q", sheep.agent.PaneID, pen.name)
				}
			}
			for i, sheep := range pen.sheep {
				if sheep.transition != present {
					continue
				}
				for _, other := range pen.sheep[i+1:] {
					if other.transition != present {
						continue
					}
					if sheep.position.x < other.position.x+sheepColumns && sheep.position.x+sheepColumns > other.position.x &&
						sheep.position.y < other.position.y+sheepBodyRows && sheep.position.y+sheepBodyRows > other.position.y {
						t.Fatalf("stationary actors %q and %q overlap", sheep.agent.PaneID, other.agent.PaneID)
					}
				}
			}
			for _, loom := range pen.looms {
				if loom.position.x < pen.workyard.x || loom.position.y < pen.workyard.y || loom.position.x+sheepColumns > pen.workyard.x+pen.workyard.width {
					t.Fatalf("loom %q is outside the workyard", loom.agent.PaneID)
				}
			}
		}
	}
}

func TestPastureRenderersDoNotReadStateAfterSceneBuilt(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "worker", Status: "working"}})
	width := contentWidth(m.width)
	grid := m.layout(width)
	m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
	scene := m.buildSceneForTest(width, grid)
	asciiBefore := (asciiRenderer{}).render(scene)
	kittyBefore := newKittyRenderer().render(scene)
	m.state.step += 20
	delete(m.state.sheep, fleet.NewAgentKey("local", "worker"))
	if asciiAfter := (asciiRenderer{}).render(scene); asciiAfter != asciiBefore {
		t.Fatal("ASCII renderer read mutable model state outside the scene")
	}
	if kittyAfter := newKittyRenderer().render(scene); kittyAfter != kittyBefore {
		t.Fatal("Kitty renderer read mutable model state outside the scene")
	}
}

func TestPastureSceneReflowsImmediatelyAtEachWidth(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "a", Status: "idle"}, {PaneID: "b", Status: "done"}})
	for _, width := range []int{60, 100, 145} {
		grid := m.layout(width)
		m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
		scene := m.buildSceneForTest(width, grid)
		for _, row := range scene.rows {
			for _, pen := range row.pens {
				if pen.bounds.x < 0 || pen.bounds.x+pen.bounds.width > width {
					t.Fatalf("width %d left pen %q outside the viewport", width, pen.name)
				}
			}
		}
	}
}
