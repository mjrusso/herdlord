package pasture

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func TestKittyGraphicsAvailableInGhostty(t *testing.T) {
	t.Setenv("HERDLORD_KITTY", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-ghostty")
	if !kittyGraphicsAvailable() {
		t.Fatal("xterm-ghostty was not detected")
	}

	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "ghostty")
	if !kittyGraphicsAvailable() {
		t.Fatal("TERM_PROGRAM=ghostty was not detected")
	}
}

func TestPastureSheepBounceOffFence(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	key := fleet.NewAgentKey("local", "p1")
	m.state.sheep[key] = sheep{x: sheepAreaOrigin, y: top + fenceRows, dx: -1, dy: 0}
	moving := m.state.sheep[key]
	moving.ambientSteps = 1
	m.state.sheep[key] = moving
	m.advanceState(m.snapshot())
	got := m.state.sheep[key]
	if got.x != sheepAreaOrigin+1 || got.dx != 1 {
		t.Fatalf("fence bounce = %+v", got)
	}
}

func TestPastureSheepReverseWhenTheyCollide(t *testing.T) {
	agents := []herdr.Agent{{PaneID: "p1", Status: "idle"}, {PaneID: "p2", Status: "idle"}}
	m := physicsModel(t, agents)
	m.state.sheep[fleet.NewAgentKey("local", "p1")] = sheep{x: sheepAreaOrigin, y: top + fenceRows, dx: 1, dy: 0, ambientSteps: 1}
	m.state.sheep[fleet.NewAgentKey("local", "p2")] = sheep{x: sheepAreaOrigin + sheepColumns, y: top + fenceRows, dx: -1, dy: 0, ambientSteps: 1}
	m.advanceState(m.snapshot())
	first, second := m.state.sheep[fleet.NewAgentKey("local", "p1")], m.state.sheep[fleet.NewAgentKey("local", "p2")]
	if first.dx != -1 || second.dx != 1 {
		t.Fatalf("collision directions = %d, %d", first.dx, second.dx)
	}
	if first.ambientSteps != 0 || second.ambientSteps != 0 {
		t.Fatalf("collision left ambient steps active: %d, %d", first.ambientSteps, second.ambientSteps)
	}
}

func TestOnlyScheduledIdleAgentsWalk(t *testing.T) {
	agents := []herdr.Agent{{PaneID: "idle", Status: "idle"}, {PaneID: "working", Status: "working"}}
	m := physicsModel(t, agents)
	idleKey, workingKey := fleet.NewAgentKey("local", "idle"), fleet.NewAgentKey("local", "working")
	m.state.sheep[idleKey] = sheep{x: sheepAreaOrigin, y: top + fenceRows, dx: 1, dy: 0, ambientSteps: 1}
	workingSheep := m.state.sheep[workingKey]
	workingSheep.x, workingSheep.y = sheepAreaOrigin+20, top+fenceRows
	m.state.sheep[workingKey] = workingSheep
	m.advanceState(m.snapshot())
	if m.state.sheep[idleKey].x != sheepAreaOrigin+1 {
		t.Fatalf("idle agent did not walk: %+v", m.state.sheep[idleKey])
	}
	if m.state.sheep[workingKey].x != sheepAreaOrigin+20 {
		t.Fatalf("working agent moved: %+v", m.state.sheep[workingKey])
	}
}

func TestIdleAgentUsuallyStaysPut(t *testing.T) {
	agent := herdr.Agent{PaneID: "idle", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", agent.PaneID)
	before := m.state.sheep[key]
	m.state.step = (50 - agentSeed(key)%50 + 1) % 50
	m.advanceState(m.snapshot())
	after := m.state.sheep[key]
	if after.x != before.x || after.y != before.y || after.ambientSteps != 0 {
		t.Fatalf("unscheduled idle agent moved: before=%+v after=%+v", before, after)
	}
}

func TestNewAgentEntersThroughGate(t *testing.T) {
	agent := herdr.Agent{PaneID: "new", Status: "idle"}
	m := newTestModel([]target.Target{{Name: "local"}})
	m.width = 80
	m.syncState(contentWidth(m.width))
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{agent}}
	m.syncState(contentWidth(m.width))
	sheep := m.state.sheep[fleet.NewAgentKey("local", agent.PaneID)]
	grid := m.layout(contentWidth(m.width))
	if sheep.phase != entering || sheep.x != gateCenter(grid.penWidth)-sheepColumns/2 {
		t.Fatalf("new agent did not enter at gate: %+v", sheep)
	}
	first := sheepPose(agent, sheep, 0)
	second := sheepPose(agent, sheep, 2)
	if first != poseSheepRestLeft && first != poseSheepRestRight {
		t.Fatalf("entry pose = %d, want a direction-matched sheep", first)
	}
	if second != poseSheepWalkLeft && second != poseSheepWalkRight {
		t.Fatalf("entry walking pose = %d", second)
	}
}

func TestRemovedAgentDiesAfterTwoSuccessfulSnapshots(t *testing.T) {
	agent := herdr.Agent{PaneID: "gone", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", agent.PaneID)
	empty := poll.TargetStatus{State: poll.OK}
	m.state.ReconcileAgents("local", empty)
	if m.state.missing[key] != 1 || m.state.sheep[key].phase == dying {
		t.Fatalf("first missing snapshot changed lifecycle: missing=%d sheep=%+v", m.state.missing[key], m.state.sheep[key])
	}
	m.statuses["local"] = empty
	m.state.ReconcileAgents("local", empty)
	if m.state.sheep[key].phase != dying {
		t.Fatalf("second missing snapshot did not start death: %+v", m.state.sheep[key])
	}
	wantFrames := []pose{poseSheepWobble, poseSheepWobble, poseSheepFallen, poseSheepFallen, poseSheepSpirit, poseSheepSpirit}
	for step, want := range wantFrames {
		sheep := m.state.sheep[key]
		sheep.phaseStep = step
		if got := sheepPose(agent, sheep, 0); got != want {
			t.Fatalf("death frame %d = %d, want %d", step, got, want)
		}
	}
	for step := 0; step < len(wantFrames); step++ {
		before := m.state.sheep[key].phaseStep
		m.state.ReconcileAgents("local", empty)
		if got := m.state.sheep[key].phaseStep; got != before {
			t.Fatalf("missing snapshot reset death animation from step %d to %d", before, got)
		}
		m.advanceState(m.snapshot())
	}
	if _, exists := m.state.sheep[key]; exists {
		t.Fatal("removed sheep remained after death animation completed")
	}
}

func TestCompletedRemovalDeletesFinalKittyPlacement(t *testing.T) {
	agent := herdr.Agent{PaneID: "gone", Status: "done"}
	m := physicsModel(t, []herdr.Agent{agent})
	testView(m)
	empty := poll.TargetStatus{State: poll.OK}
	m.statuses["local"] = empty
	m.state.ReconcileAgents("local", empty)
	m.state.ReconcileAgents("local", empty)
	for range 6 {
		m.advanceState(m.snapshot())
		view := testView(m)
		if _, exists := m.state.sheep[fleet.NewAgentKey("local", agent.PaneID)]; !exists {
			if !strings.Contains(view, fmt.Sprintf("a=d,d=i,i=%d,p=", kittySpiritID)) {
				t.Fatal("completed removal did not delete the final Kitty placement")
			}
			return
		}
	}
	t.Fatal("removed sheep never completed its lifecycle")
}

func TestRemovedWorkingAgentRemainsInsideShrinkingPen(t *testing.T) {
	agents := make([]herdr.Agent, 9)
	for i := range agents {
		agents[i] = herdr.Agent{PaneID: fmt.Sprintf("worker-%d", i), Status: "working"}
	}
	m := physicsModel(t, agents)
	const width = 47
	m.state.width = 0
	m.syncState(width)

	remaining := append([]herdr.Agent(nil), agents[:len(agents)-1]...)
	status := poll.TargetStatus{State: poll.OK, Agents: remaining}
	m.statuses["local"] = status
	m.state.ReconcileAgents("local", status)
	m.state.ReconcileAgents("local", status)
	height := penHeight(len(remaining), width)
	m.state.syncLayout(m.snapshot(), width, []int{height})
	removed := m.state.sheep[fleet.NewAgentKey("local", agents[len(agents)-1].PaneID)]
	if removed.y < 0 || removed.y >= height {
		t.Fatalf("removed sheep row %d is outside pen height %d", removed.y, height)
	}

	renderer := newKittyFrameRenderer()
	view := renderer.renderPen(m, "local", remaining, width, height)
	removedLoomKey := loomAnimationKey(fleet.NewAgentKey("local", agents[len(agents)-1].PaneID))
	for _, imageID := range []int{kittyLoomID, kittyLoomAltID, kittyLoomJammedID} {
		if strings.Contains(view, fmt.Sprintf("a=p,i=%d,p=%d", imageID, renderer.animatedPlacementID(removedLoomKey, imageID))) {
			t.Fatalf("removed working agent retained loom frame %d", imageID)
		}
	}
}

func TestDepartedAgentsHaveStableOrder(t *testing.T) {
	p := New(ASCII)
	for _, pane := range []string{"p3", "p1", "p2"} {
		key := fleet.NewAgentKey("local", pane)
		p.sheep[key] = sheep{agent: herdr.Agent{PaneID: pane}, phase: dying}
	}
	agents := p.agents("local", nil, false)
	got := []string{agents[0].PaneID, agents[1].PaneID, agents[2].PaneID}
	if fmt.Sprint(got) != "[p1 p2 p3]" {
		t.Fatalf("departed agent order = %v", got)
	}
}

func TestSheepFacesItsDirection(t *testing.T) {
	if got := walkingPose(-1, 2); got != poseSheepWalkLeft {
		t.Fatalf("left-facing frame = %d", got)
	}
	if got := walkingPose(1, 2); got != poseSheepWalkRight {
		t.Fatalf("right-facing frame = %d", got)
	}
}

func TestSheepStatesSelectPurposefulPoses(t *testing.T) {
	tests := []struct {
		name     string
		agent    herdr.Agent
		position sheep
		first    pose
		second   pose
		secondAt uint64
	}{
		{
			name:     "working",
			agent:    herdr.Agent{PaneID: "working", Status: "working"},
			position: sheep{phase: present},
			first:    poseSheepWorking,
			second:   poseSheepWorkingAlt,
		},
		{
			name:     "blocked",
			agent:    herdr.Agent{PaneID: "blocked", Status: "blocked"},
			position: sheep{phase: present},
			first:    poseSheepBlocked,
			second:   poseSheepStomp,
		},
		{
			name:     "done",
			agent:    herdr.Agent{PaneID: "done", Status: "done"},
			position: sheep{phase: present},
			first:    poseSheepDone,
			second:   poseSheepDoneAlt,
			secondAt: 3,
		},
		{
			name:     "walking right",
			agent:    herdr.Agent{PaneID: "walking", Status: "idle"},
			position: sheep{phase: present, dx: 1, ambientSteps: 2},
			first:    poseSheepRestRight,
			second:   poseSheepWalkRight,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secondAt := test.secondAt
			if secondAt == 0 {
				secondAt = 2
			}
			if first, second := sheepPose(test.agent, test.position, 0), sheepPose(test.agent, test.position, secondAt); first != test.first || second != test.second {
				t.Fatalf("frames = %d, %d, want %d, %d", first, second, test.first, test.second)
			}
		})
	}
}

func TestKittyAnimationFramesUseDistinctPlacements(t *testing.T) {
	agent := herdr.Agent{PaneID: "working", Status: "working"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", agent.PaneID)
	position := m.state.sheep[key]
	position.phase = present
	m.state.sheep[key] = position
	renderer := newKittyFrameRenderer()
	m.state.step = 2

	view := renderer.renderPen(m, "local", []herdr.Agent{agent}, 74, penHeight(1, 74))
	currentPlacement := renderer.animatedPlacementID(sheepAnimationKey(key), kittyWorkingAltID)
	place := fmt.Sprintf("a=p,i=%d,p=%d", kittyWorkingAltID, currentPlacement)
	if !strings.Contains(view, place) {
		t.Fatalf("frame transition did not place image %d at %d", kittyWorkingAltID, currentPlacement)
	}
}

func TestKittyFrameClearsTheSupersededSheepPose(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	testView(m)
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "working"}}}
	m.state.ReconcileAgents("local", m.statuses["local"])
	view := testView(m)
	cleared := false
	for _, sprite := range kittySprites {
		if sprite.active() && sprite.class&kittySheep != 0 && strings.Contains(view, fmt.Sprintf("a=d,d=i,i=%d,p=", sprite.id)) {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("pose change did not clear the sheep's previous placement")
	}
}

func TestKittyRepeatsDeletesForSeveralFrames(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}, {PaneID: "p2", Status: "idle"}})
	testView(m)
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "idle"}}}
	for range 2 {
		m.state.ReconcileAgents("local", m.statuses["local"])
	}
	for range deathAnimationSteps + 1 {
		m.advanceState(m.snapshot())
	}
	first := testView(m)
	gone := regexp.MustCompile(`a=d,d=i,i=\d+,p=(\d+)`).FindAllStringSubmatch(first, -1)
	if len(gone) == 0 {
		t.Fatal("removing an agent produced no delete")
	}
	repeated := testView(m)
	for _, match := range gone {
		if !strings.Contains(repeated, "p="+match[1]+",q=2") {
			t.Fatalf("delete of placement %s was not repeated on the next frame", match[1])
		}
	}
	for range kittyPlacementDeleteRetries {
		testView(m)
	}
	if settled := testView(m); strings.Contains(settled, "a=d,d=i") {
		t.Fatal("deletes repeated past their retry budget")
	}
}

// A steady scene must delete nothing. Per-frame deletes made the pasture flicker.
func TestKittySteadyFrameDeletesNothing(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	testView(m)
	if view := testView(m); strings.Contains(view, "a=d,d=i") {
		t.Fatal("unchanged frame deleted placements")
	}
}

func TestHeightOnlyResizeRedrawsCenteredKittyLayout(t *testing.T) {
	targets := []target.Target{{Name: "north-field"}, {Name: "south-field"}, {Name: "orchard"}}
	m := newTestModel(targets)
	for _, configured := range targets {
		m.statuses[configured.Name] = poll.TargetStatus{State: poll.OK}
	}
	m.width = 160
	m.height = 70
	renderer := newKittyFrameRenderer()
	width := contentWidth(m.width)
	firstGrid := m.layout(width)
	firstScene := m.buildSceneForTest(width, firstGrid)
	renderer.render(firstScene)

	m.height = 74
	secondGrid := m.layout(width)
	secondScene := m.buildSceneForTest(width, secondGrid)
	if firstScene.gridTop == secondScene.gridTop {
		t.Fatalf("test setup did not move the centered grid: top remained %d", firstScene.gridTop)
	}
	if firstGrid.penWidth != secondGrid.penWidth || firstGrid.visibleRows != secondGrid.visibleRows {
		t.Fatal("test setup changed the grid rather than only its vertical placement")
	}

	view := renderer.render(secondScene)
	for _, imageID := range []int{kittyFenceVertID, kittyTargetSignID, kittyCastleID, kittyRoadVertID, kittyFenceGateID, kittyShepherdID, kittyLordID} {
		if !strings.Contains(view, fmt.Sprintf("a=p,i=%d", imageID)) {
			t.Fatalf("height-only resize did not redraw Kitty image %d at its new coordinates", imageID)
		}
	}
}

func TestKittyAnimationPlacesCurrentFrame(t *testing.T) {
	agent := herdr.Agent{PaneID: "working", Status: "working"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", agent.PaneID)
	position := m.state.sheep[key]
	position.phase = present
	m.state.sheep[key] = position
	renderer := newKittyFrameRenderer()
	m.state.step = 0

	view := renderer.renderPen(m, "local", []herdr.Agent{agent}, 74, penHeight(1, 74))
	placementID := renderer.animatedPlacementID(sheepAnimationKey(key), kittyWorkingID)
	place := fmt.Sprintf("a=p,i=%d,p=%d", kittyWorkingID, placementID)
	if !strings.Contains(view, place) {
		t.Fatal("moving frame did not place its current image")
	}
}

func TestWorkSlotsKeepSpritesAboveBottomFence(t *testing.T) {
	for _, count := range []int{1, 4, 8, 12} {
		width := 30
		height := penHeight(count, width)
		for slot := range count {
			position := workSlot("local", fmt.Sprintf("p%d", slot), slot, width, height)
			if bottom := position.y + 1 + sheepRows; bottom > height-fenceRows {
				t.Fatalf("count %d slot %d ends at %d below fence at %d", count, slot, bottom, height-fenceRows)
			}
		}
	}
}

func TestEnteringSheepReachDestinationsInCompactPens(t *testing.T) {
	for _, test := range []struct {
		name   string
		status string
		count  int
	}{
		{name: "grazing", status: "done", count: 7},
		{name: "workyard", status: "working", count: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := newTestModel([]target.Target{{Name: "local"}})
			m.syncState(contentWidth(m.width))
			agents := make([]herdr.Agent, test.count)
			for i := range agents {
				agents[i] = herdr.Agent{PaneID: fmt.Sprintf("p%d", i), Status: test.status}
			}
			m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: agents}
			m.syncState(contentWidth(m.width))
			key := fleet.NewAgentKey("local", agents[len(agents)-1].PaneID)
			for range 100 {
				m.advanceState(m.snapshot())
				if m.state.sheep[key].phase == present {
					return
				}
			}
			t.Fatalf("last sheep never reached destination: %+v", m.state.sheep[key])
		})
	}
}

func TestIdleKittyWalkingFramesMatchDirection(t *testing.T) {
	agent := herdr.Agent{PaneID: "idle", Status: "idle"}
	for _, test := range []struct {
		direction  int
		rest, walk pose
	}{{-1, poseSheepRestLeft, poseSheepWalkLeft}, {1, poseSheepRestRight, poseSheepWalkRight}} {
		position := sheep{phase: present, dx: test.direction, ambientSteps: 8}
		if rest, walk := sheepPose(agent, position, 0), sheepPose(agent, position, 2); rest != test.rest || walk != test.walk {
			t.Fatalf("direction %d frames = %d, %d, want %d, %d", test.direction, rest, walk, test.rest, test.walk)
		}
	}
}

func TestLoomEligibilityMatchesAgentState(t *testing.T) {
	for _, test := range []struct {
		status   string
		wantLoom bool
	}{
		{status: "idle"},
		{status: "working", wantLoom: true},
		{status: "blocked", wantLoom: true},
		{status: "done"},
		{status: "unknown"},
	} {
		t.Run(test.status, func(t *testing.T) {
			agent := herdr.Agent{PaneID: "p1", Status: test.status}
			m := physicsModel(t, []herdr.Agent{agent})
			scene := m.buildSceneForTest(contentWidth(m.width), m.layout(contentWidth(m.width)))
			hasLoom := len(scene.rows[0].pens[0].looms) > 0
			if hasLoom != test.wantLoom {
				t.Fatalf("status %q loom=%v, want %v", test.status, hasLoom, test.wantLoom)
			}
		})
	}
}

func TestLeavingWorkRemovesLoomPlacement(t *testing.T) {
	agent := herdr.Agent{PaneID: "p1", Status: "working"}
	m := physicsModel(t, []herdr.Agent{agent})
	testView(m)
	status := m.statuses["local"]
	status.Agents[0].Status = "idle"
	m.statuses["local"] = status

	view := testView(m)
	want := fmt.Sprintf("a=d,d=i,i=%d,p=", kittyLoomID)
	if !strings.Contains(view, want) {
		t.Fatalf("leaving work did not remove loom placement %q", want)
	}
}

func TestBlockedAgentStaysAtJammedLoom(t *testing.T) {
	agent := herdr.Agent{PaneID: "blocked", Status: "working"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", agent.PaneID)
	status := m.statuses["local"]
	status.Agents[0].Status = "blocked"
	m.statuses["local"] = status
	m.syncState(contentWidth(m.width))

	sheep := m.state.sheep[key]
	if sheep.phase == relocating {
		t.Fatalf("blocked sheep left its loom: %+v", sheep)
	}
	if pose, visible := loomPose(status.Agents[0], sheep, m.state.step); !visible || pose != poseLoomJammed {
		t.Fatalf("blocked agent loom pose = %d, visible=%v", pose, visible)
	}
}

func TestASCIIWorkshopIsDistinctFromPasture(t *testing.T) {
	agents := []herdr.Agent{{PaneID: "working", Status: "working"}, {PaneID: "blocked", Status: "blocked"}}
	m := physicsModel(t, agents)
	height := 18
	grid := grid{columns: 1, penWidth: 30, heights: []int{height}, offsets: []int{0}, rowHeights: []int{height}, visibleRows: 1}
	view := asciiPen(m.buildSceneForTest(30, grid).rows[0].pens[0])
	for _, want := range []string{"[ LOOM ]", ">>", "#X#"} {
		if !strings.Contains(view, want) {
			t.Fatalf("ASCII workshop missing %q:\n%s", want, view)
		}
	}
}

func TestWorkingAgentMovesToWorkyardAndReturnsToGrass(t *testing.T) {
	agent := herdr.Agent{PaneID: "working", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	m.height = 50
	m.state.width = 0
	m.syncState(contentWidth(m.width))
	key := fleet.NewAgentKey("local", agent.PaneID)
	home := m.state.sheep[key]
	status := m.statuses["local"]
	status.Agents[0].Status = "working"
	m.statuses["local"] = status
	m.syncState(contentWidth(m.width))
	working := m.state.sheep[key]
	workyardTop := workyardTop(penHeight(1, contentWidth(m.width)))
	if working.phase != relocating || working.destinationY < workyardTop {
		t.Fatalf("working sheep did not head to workyard: %+v", working)
	}
	for working.phase == relocating {
		m.advanceState(m.snapshot())
		working = m.state.sheep[key]
	}
	if pose := sheepPose(status.Agents[0], working, 0); pose != poseSheepWorking {
		t.Fatalf("arrived worker pose = %d, want %d", pose, poseSheepWorking)
	}
	status.Agents[0].Status = "idle"
	m.statuses["local"] = status
	m.syncState(contentWidth(m.width))
	returning := m.state.sheep[key]
	if returning.phase != relocating || returning.destinationX != home.destinationX || returning.destinationY != home.destinationY {
		t.Fatalf("idle sheep did not return to grass: %+v", returning)
	}
}

func TestWorkingAgentTracksLoomAfterResizeAndRendererToggle(t *testing.T) {
	agent := herdr.Agent{PaneID: "working", Status: "working"}
	m := physicsModel(t, []herdr.Agent{agent})
	m.state.visible = true
	m.height = 30
	testView(m)

	key := fleet.NewAgentKey("local", agent.PaneID)
	m.height = 50
	grid := m.layout(contentWidth(m.width))
	want := workSlot("local", agent.PaneID, 0, grid.penWidth, grid.heights[0])
	if err := m.toggleRenderer(); err != nil {
		t.Fatal(err)
	}
	m.syncState(contentWidth(m.width))
	got := m.state.sheep[key]
	if got.x != want.x || got.y != want.y {
		t.Fatalf("working sheep remained at stale loom position (%d,%d), want (%d,%d)", got.x, got.y, want.x, want.y)
	}
}

func TestShepherdWalksToActiveLoomsAndReturns(t *testing.T) {
	agent := herdr.Agent{PaneID: "worker", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	m.height = 50
	grid := m.layout(contentWidth(m.width))
	m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
	home := m.state.shepherds["local"]

	status := m.statuses["local"]
	status.Agents[0].Status = "working"
	m.statuses["local"] = status
	m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
	moving := m.state.shepherds["local"]
	if moving.destinationY <= home.y || moving.y != home.y {
		t.Fatalf("shepherd did not start toward workshop: home=%+v moving=%+v", home, moving)
	}
	for moving.y != moving.destinationY {
		m.advanceState(m.snapshot())
		moving = m.state.shepherds["local"]
	}
	if got := shepherdPose(status); got != poseShepherdWorking {
		t.Fatalf("settled shepherd pose = %d, want %d", got, poseShepherdWorking)
	}

	status.Agents[0].Status = "idle"
	m.statuses["local"] = status
	m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
	returning := m.state.shepherds["local"]
	if returning.destinationY != home.y || returning.y == home.y {
		t.Fatalf("shepherd did not start home: home=%+v returning=%+v", home, returning)
	}
	for returning.y != returning.destinationY {
		m.advanceState(m.snapshot())
		returning = m.state.shepherds["local"]
	}
	if returning.y != home.y {
		t.Fatalf("shepherd returned to row %d, want %d", returning.y, home.y)
	}
}

func TestShepherdRespondsToHerdWork(t *testing.T) {
	working := poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "working"}}}
	blocked := poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "blocked"}}}
	if got := shepherdPose(working); got != poseShepherdWorking {
		t.Fatalf("working herd selected shepherd pose %d", got)
	}
	if got := shepherdPose(blocked); got != poseShepherdAlert {
		t.Fatalf("blocked herd selected shepherd pose %d", got)
	}
}

func TestLordReflectsFleetPriority(t *testing.T) {
	targets := []target.Target{{Name: "a"}, {Name: "b"}}
	tests := []struct {
		name     string
		statuses map[string]poll.TargetStatus
		want     pose
	}{
		{name: "calm", statuses: map[string]poll.TargetStatus{"a": {State: poll.OK}, "b": {State: poll.Paused}}, want: poseLordCalm},
		{name: "active", statuses: map[string]poll.TargetStatus{"a": {State: poll.OK, Agents: []herdr.Agent{{Status: "working"}}}, "b": {State: poll.OK}}, want: poseLordDirecting},
		{name: "alert overrides active", statuses: map[string]poll.TargetStatus{"a": {State: poll.OK, Agents: []herdr.Agent{{Status: "working"}}}, "b": {State: poll.Unreachable}}, want: poseLordAlert},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := lordPose(targets, test.statuses); got != test.want {
				t.Fatalf("lord pose = %d, want %d", got, test.want)
			}
		})
	}
}

func TestLordStandsBesideCastleAtCommandStation(t *testing.T) {
	for _, width := range []int{40, 72, 145} {
		castle, lord := commandStationColumns(width, castleColumns, lordColumns, 2)
		if lord != castle+castleColumns+2 {
			t.Fatalf("width %d: castle at %d and lord at %d", width, castle, lord)
		}
		asciiCastle, asciiLord := asciiCommandStationColumns(width)
		if asciiLord != asciiCastle+11 {
			t.Fatalf("width %d: ASCII castle at %d and lord at %d", width, asciiCastle, asciiLord)
		}
	}
}

func TestLordStatusBubblePointsTowardLord(t *testing.T) {
	for _, width := range []int{72, 145} {
		text := "Directing 3 active."
		lordX := lordColumn(width)
		line, x, y, pointerX, pointer := sideBubbleLayout(text, lordX, royalSceneRows-royalRoadRows-lordRows, lordColumns, lordRows, width, royalSceneRows)
		if line == "" || y < royalSceneRows-lordRows || (pointer != "<" && pointer != ">") {
			t.Fatalf("width %d: invalid side bubble %q at %d,%d with pointer %q at %d", width, line, x, y, pointer, pointerX)
		}
		if pointer == "<" && pointerX != lordX+lordColumns {
			t.Fatalf("width %d: right-side pointer is at %d", width, pointerX)
		}
		if pointer == ">" && pointerX != lordX-1 {
			t.Fatalf("width %d: left-side pointer is at %d", width, pointerX)
		}
	}
}

func TestEmptySideBubbleRendersNothing(t *testing.T) {
	line, _, _, _, pointer := sideBubbleLayout("", 20, 3, lordColumns, lordRows, 80, royalSceneRows)
	if line != "" || pointer != "" {
		t.Fatalf("empty bubble rendered line %q and pointer %q", line, pointer)
	}
}

func TestRoleStateChangesReplaceOnlyTheirPlacements(t *testing.T) {
	m := physicsModel(t, nil)
	testView(m)
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "working"}}}
	m.state.ReconcileAgents("local", m.statuses["local"])
	rendered := testView(m)
	for _, transition := range []struct {
		key         animationKey
		previous    int
		replacement int
	}{
		{key: shepherdAnimationKey("local"), previous: kittyShepherdID, replacement: kittyShepherdWorkID},
		{key: lordAnimationKey(), previous: kittyLordID, replacement: kittyLordMapID},
	} {
		put := fmt.Sprintf("a=p,i=%d,p=", transition.replacement)
		remove := fmt.Sprintf("a=d,d=i,i=%d,p=", transition.previous)
		putAt, removeAt := strings.Index(rendered, put), strings.Index(rendered, remove)
		if putAt < 0 || removeAt < 0 || removeAt > putAt {
			t.Fatalf("role transition %q placed image %d before deleting image %d", transition.key, transition.replacement, transition.previous)
		}
	}
}

func TestRoyalSceneCropsCastleToVisibleBounds(t *testing.T) {
	m := newTestModel(nil)
	snapshot := m.buildSceneForTest(72, m.layout(72))
	snapshot.roadCenters = []int{36}
	scene := newKittyFrameRenderer().royalScene(snapshot)
	if !strings.Contains(scene, fmt.Sprintf("i=%d,p=", kittyCastleID)) || !strings.Contains(scene, "x=23,y=29,w=335,h=126") {
		t.Fatalf("castle placement did not crop transparent margins: %q", scene)
	}
}

func TestRoyalSceneOnlyExplainsLordStateAfterAChange(t *testing.T) {
	m := newTestModel([]target.Target{{Name: "local"}})
	m.state.visible = true
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "worker", Status: "working"}}}
	snapshot := m.buildSceneForTest(72, m.layout(72))
	if snapshot.lordBubble != "" {
		t.Fatalf("initial royal scene has persistent status bubble %q", snapshot.lordBubble)
	}
	initial := newKittyFrameRenderer().royalScene(snapshot)
	if strings.Contains(initial, "<[]") || strings.Contains(initial, "[]>") {
		t.Fatalf("initial royal scene rendered an empty lord bubble: %q", initial)
	}
	m.state.emitFleetSummary(fleetSummary(m.snapshot(), "", poll.TargetStatus{}))
	snapshot = m.buildSceneForTest(72, m.layout(72))
	snapshot.roadCenters = []int{36}
	scene := newKittyFrameRenderer().royalScene(snapshot)
	if !strings.Contains(scene, "Directing 1 active.") {
		t.Fatalf("royal scene did not explain changed lord state: %q", scene)
	}
}

func TestAgentPlacementUsesCurrentVariant(t *testing.T) {
	agent := herdr.Agent{PaneID: "p1", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", "p1")
	current := kittyPoseImageIDs[sheepPose(agent, m.state.sheep[key], m.state.step)]
	renderer := newKittyFrameRenderer()
	layer := renderer.placementLayer(m, "local", []herdr.Agent{agent}, 74, penHeight(1, 74))
	if !strings.Contains(layer, fmt.Sprintf("a=p,i=%d,p=%d", current, renderer.animatedPlacementID(sheepAnimationKey(key), current))) {
		t.Fatal("agent placement did not draw its current sprite")
	}
}

func TestPenHeightDoesNotFollowSheepPosition(t *testing.T) {
	agent := herdr.Agent{PaneID: "p1", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", agent.PaneID)
	height := penHeight(1, 74)

	m.state.sheep[key] = sheep{x: sheepAreaOrigin, y: top + fenceRows, dx: 1}
	topHeight := len(strings.Split(newKittyFrameRenderer().renderPen(m, "local", []herdr.Agent{agent}, 74, height), "\n"))
	m.state.sheep[key] = sheep{x: sheepAreaOrigin, y: height - fenceRows - sheepBodyRows, dx: 1}
	bottomHeight := len(strings.Split(newKittyFrameRenderer().renderPen(m, "local", []herdr.Agent{agent}, 74, height), "\n"))

	if topHeight != height || bottomHeight != height {
		t.Fatalf("pen heights = %d and %d, want %d", topHeight, bottomHeight, height)
	}
}
