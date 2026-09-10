package pasture

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func (r *kittyFrameRenderer) renderPen(m *testModel, targetName string, agents []herdr.Agent, width, height int) string {
	pen := testPenScene(m, targetName, agents, width, height)
	return r.placementLayerScene(pen) + renderPenCanvasScene(pen, m.state.step)
}

func (r *kittyFrameRenderer) placementLayer(m *testModel, targetName string, agents []herdr.Agent, width, height int) string {
	return r.placementLayerScene(testPenScene(m, targetName, agents, width, height))
}

func testPenScene(m *testModel, targetName string, agents []herdr.Agent, width, height int) penScene {
	status := m.statuses[targetName]
	status.Agents = agents
	return m.state.buildPenScene(target.Target{Name: targetName}, status, sceneRect{width: width, height: height})
}

type countingRenderer struct {
	renders int
}

func (*countingRenderer) enter() (string, error)  { return "", nil }
func (*countingRenderer) leave() string           { return "" }
func (*countingRenderer) clearPlacements() string { return "" }
func (r *countingRenderer) render(Scene) string {
	r.renders++
	return "frame"
}

func TestKittyLeavePurgesEveryReservedSprite(t *testing.T) {
	control := newKittyRenderer().leave()
	for _, sprite := range kittySprites {
		command := fmt.Sprintf("a=d,d=I,i=%d,q=2", sprite.id)
		if !strings.Contains(control, command) {
			t.Fatalf("Kitty leave omitted image ID %d", sprite.id)
		}
	}
}

func TestEmbeddedKittyAssetsAreRegistered(t *testing.T) {
	paths, err := fs.Glob(assets, "assets/*.png")
	if err != nil {
		t.Fatal(err)
	}
	registered := make(map[string]bool, len(kittySprites))
	for _, sprite := range kittySprites {
		if sprite.active() {
			registered[sprite.path] = true
		}
	}
	for _, path := range paths {
		if !registered[path] {
			t.Fatalf("embedded Kitty asset %q is not registered", path)
		}
	}
}

func TestKittyUploadUsesChunkedPNG(t *testing.T) {
	upload, err := kittyUploadSprites()
	if err != nil {
		t.Fatal(err)
	}
	for _, sprite := range kittySprites {
		if !sprite.active() {
			continue
		}
		want := fmt.Sprintf("\x1b_Ga=t,f=100,i=%d,q=2,m=", sprite.id)
		if !strings.Contains(upload, want) {
			t.Fatalf("upload missing %q", want)
		}
	}
	if !strings.Contains(upload, "\x1b_Gm=0,q=2;") {
		t.Fatal("upload did not terminate its chunks")
	}
	if strings.Count(upload, "\x1b_G") < 3 {
		t.Fatalf("upload was not chunked")
	}
	if strings.Contains(upload, "a=p,U=1") {
		t.Fatal("sprites were registered as virtual placements")
	}
}

func TestKittySpriteUploadIsCached(t *testing.T) {
	if _, err := kittyUploadSprites(); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(5, func() {
		if _, err := kittyUploadSprites(); err != nil {
			t.Fatal(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("cached sprite upload allocated %.0f objects per call", allocations)
	}
}

func TestKittySpriteRegistryIsComplete(t *testing.T) {
	registered := make(map[int]bool, len(kittySprites))
	poses := make(map[pose]int, int(poseCount))
	for _, sprite := range kittySprites {
		if sprite.id == 0 || (sprite.active() && sprite.class == 0) || (!sprite.active() && (sprite.class != 0 || len(sprite.poses) != 0)) {
			t.Fatalf("incomplete sprite registration: %#v", sprite)
		}
		if registered[sprite.id] {
			t.Fatalf("duplicate sprite image ID %d", sprite.id)
		}
		registered[sprite.id] = true
		for _, pose := range sprite.poses {
			if previous, exists := poses[pose]; exists {
				t.Fatalf("pose %d maps to image IDs %d and %d", pose, previous, sprite.id)
			}
			poses[pose] = sprite.id
		}
	}
	for pose := pose(0); pose < poseCount; pose++ {
		imageID, exists := kittyPoseImageIDs[pose]
		if !exists {
			t.Fatalf("pose %d has no registered image", pose)
		}
		if !registered[imageID] {
			t.Fatalf("pose %d uses unregistered image ID %d", pose, imageID)
		}
		if poses[pose] != imageID {
			t.Fatalf("pose %d lookup has image ID %d, registry has %d", pose, imageID, poses[pose])
		}
	}
}

func TestKittyEnterPurgesRetiredImagesBeforeUploading(t *testing.T) {
	renderer := newKittyRenderer()
	commands, err := renderer.enter()
	if err != nil {
		t.Fatal(err)
	}
	retired := fmt.Sprintf("a=d,d=I,i=%d", kittyRetiredImageID)
	upload := fmt.Sprintf("a=t,f=100,i=%d", kittySheepImageID)
	retiredAt, uploadAt := strings.Index(commands, retired), strings.Index(commands, upload)
	if retiredAt < 0 || uploadAt < 0 || retiredAt > uploadAt {
		t.Fatalf("startup did not purge retired image before upload: retired=%d upload=%d", retiredAt, uploadAt)
	}
}

func TestKittyControlExpiresOnDedicatedTimer(t *testing.T) {
	state := New(Kitty)
	frame := Frame{
		Snapshot: fleet.Snapshot{Targets: []target.Target{{Name: "local"}}, Statuses: map[string]poll.TargetStatus{"local": {State: poll.OK}}},
		Viewport: Viewport{Width: 80, Height: 40},
	}
	if err := state.Open(frame); err != nil {
		t.Fatal(err)
	}
	cmd := state.ControlExpiry()
	if state.Control() == "" || cmd == nil {
		t.Fatal("Kitty setup did not schedule control expiry")
	}
	msg, ok := cmd().(ControlExpiryMsg)
	if !ok {
		t.Fatal("control expiry command returned the wrong message")
	}
	state.ExpireControl(msg)
	if state.Control() != "" {
		t.Fatal("Kitty setup remained after its control expiry")
	}
}

func TestKittyControlCommandsAccumulateUntilExpiry(t *testing.T) {
	state := New(Kitty)
	frame := Frame{
		Snapshot: fleet.Snapshot{Targets: []target.Target{{Name: "local"}}, Statuses: map[string]poll.TargetStatus{"local": {State: poll.OK}}},
		Viewport: Viewport{Width: 80, Height: 40},
	}
	if err := state.Open(frame); err != nil {
		t.Fatal(err)
	}
	state.Close()
	control := state.Control()
	if !strings.Contains(control, "a=t,f=100") || !strings.Contains(control, "a=d,d=I") {
		t.Fatal("closing before control expiry overwrote a queued Kitty command")
	}
}

func TestOpenWithoutTargetsReturnsErrorAndCloses(t *testing.T) {
	state := New(ASCII)
	frame := Frame{
		Snapshot: fleet.Snapshot{Targets: []target.Target{{Name: "local"}}, Statuses: map[string]poll.TargetStatus{"local": {State: poll.OK}}},
		Viewport: Viewport{Width: 80, Height: 40},
	}
	if err := state.Open(frame); err != nil {
		t.Fatal(err)
	}
	frame.Snapshot.Targets = nil
	if err := state.Open(frame); err == nil {
		t.Fatal("opening without targets returned success")
	}
	if state.Visible() {
		t.Fatal("opening without targets left the pasture visible")
	}
}

func TestOpenRejectsNarrowViewport(t *testing.T) {
	state := New(ASCII)
	frame := Frame{
		Snapshot: fleet.Snapshot{Targets: []target.Target{{Name: "local"}}},
		Viewport: Viewport{Width: MinimumViewportWidth - 1, Height: MinimumViewportHeight},
	}
	if err := state.Open(frame); !errors.Is(err, ErrViewportTooNarrow) {
		t.Fatalf("Open() error = %v, want %v", err, ErrViewportTooNarrow)
	}
	if state.Visible() {
		t.Fatal("narrow viewport left the pasture visible")
	}
}

func TestPastureViewIsIdempotent(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	first := testView(m)
	second := testView(m)
	if first != second {
		t.Fatal("unchanged pasture produced different frames")
	}
}

func TestPastureDisplayIsCachedUntilStateChanges(t *testing.T) {
	targets := []target.Target{{Name: "local"}}
	frame := Frame{
		Snapshot: fleet.Snapshot{Targets: targets, Statuses: map[string]poll.TargetStatus{"local": {State: poll.OK}}},
		Viewport: Viewport{Width: 80, Height: 40},
	}
	state := New(ASCII)
	renderer := &countingRenderer{}
	state.renderer = renderer
	if err := state.Open(frame); err != nil {
		t.Fatal(err)
	}
	state.Display(frame)
	state.Sync(frame)
	state.Display(frame)
	if renderer.renders != 1 {
		t.Fatalf("unchanged pasture rendered %d times, want 1", renderer.renders)
	}
	state.Tick(frame)
	state.Display(frame)
	if renderer.renders != 2 {
		t.Fatalf("ticked pasture rendered %d times, want 2", renderer.renders)
	}
}

func TestPastureFrameReplaysStaticTiles(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	view := testView(m)

	for _, imageID := range []int{kittyGrassImageID, kittyDirtID, kittyFenceHorizID, kittyFenceVertID} {
		if !strings.Contains(view, fmt.Sprintf("a=p,i=%d", imageID)) {
			t.Fatalf("complete frame did not clear and redraw static image %d", imageID)
		}
	}
}

func TestKittyFrameKeepsAllGraphicsCommandsOnFirstLine(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	testView(m)
	// A pose change produces a delete, which must share its line with the replacement.
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "working"}}}
	m.state.ReconcileAgents("local", m.statuses["local"])
	view := testView(m)
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "a=d,d=i,i=") || !strings.Contains(lines[0], "a=p,i=") {
		t.Fatal("first line does not contain both the Kitty deletion and replacement")
	}
	for row, line := range lines[1:] {
		if strings.Contains(line, "\x1b_G") {
			t.Fatalf("view row %d contains a Kitty command", row+1)
		}
	}
}

func TestKittyPlacementCommandsDoNotAffectPenLayoutWidth(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	renderer := newKittyFrameRenderer()
	width := 40
	height := penHeight(1, width)
	first := renderer.renderPen(m, "local", m.statuses["local"].Agents, width, height)
	second := renderer.renderPen(m, "local", m.statuses["local"].Agents, width, height)

	if firstWidth, secondWidth := lipgloss.Width(first), lipgloss.Width(second); firstWidth != secondWidth || firstWidth != width {
		t.Fatalf("pen widths changed with placement traffic: first=%d second=%d want=%d", firstWidth, secondWidth, width)
	}
}

func TestKittyPlacementIDsDoNotAliasHashCollisions(t *testing.T) {
	renderer := newKittyFrameRenderer()
	first := renderer.placementID("9IbBfNY28DYZpRDS")
	second := renderer.placementID("FYlIWjXS8BKKOehu")
	if first == second || renderer.placementID("9IbBfNY28DYZpRDS") != first {
		t.Fatalf("placement IDs are not unique and stable: %d, %d", first, second)
	}
}

func TestKittyPenCanvasContainsNoGraphicsCommands(t *testing.T) {
	agent := herdr.Agent{PaneID: "p1", Status: "idle", TerminalTitleStripped: "café ☕ résumé"}
	m := physicsModel(t, []herdr.Agent{agent})
	m.state.visible = true
	m.state.emitSheepBubble("local", agent.PaneID, agentBubbleLabel(agent), bubbleLow)
	width := 40
	pen := testPenScene(m, "local", m.statuses["local"].Agents, width, penHeight(1, width))
	canvas := renderPenCanvasScene(pen, m.state.step)
	if strings.Contains(canvas, "\x1b_G") {
		t.Fatal("pen canvas contains Kitty commands")
	}
	for row, line := range strings.Split(canvas, "\n") {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("row %d width = %d, want %d", row, got, width)
		}
	}
}

func TestBubbleTeaLineTruncationPreservesKittyCommands(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "working"}})
	m.width, m.height = 80, 40
	view := testView(m)
	before := strings.Count(view, "\x1b_G")
	var after int
	for _, line := range strings.Split(view, "\n") {
		after += strings.Count(ansi.Truncate(line, m.width, ""), "\x1b_G")
	}
	if after != before {
		t.Fatalf("Bubble Tea truncation retained %d of %d Kitty commands", after, before)
	}
	firstLine := strings.Split(view, "\n")[0]
	truncated := ansi.Truncate(firstLine, m.width, "")
	castleColumn, _ := commandStationColumns(contentWidth(m.width), castleColumns, lordColumns, 2)
	castlePlacement := fmt.Sprintf("\x1b[%dB\x1b[%dC\x1b_Ga=p,i=%d", royalSceneRows-royalRoadRows-castleRows, castleColumn, kittyCastleID)
	if !strings.Contains(firstLine, castlePlacement) || !strings.Contains(truncated, castlePlacement) {
		t.Fatal("Bubble Tea truncation separated a Kitty placement from its cursor coordinates")
	}
}

func TestKittyPlacementRestoresItsOriginWithoutSavedCursorState(t *testing.T) {
	var rendered strings.Builder
	writeImagePlacement(&rendered, kittyCastleID, 123, placement{column: 17, row: 4, columns: 8, rows: 5, z: 1})
	want := fmt.Sprintf("\x1b[4B\x1b[17C\x1b_Ga=p,i=%d,p=123,c=8,r=5,z=1,C=1,q=2;\x1b\\\x1b[17D\x1b[4A", kittyCastleID)
	if rendered.String() != want {
		t.Fatalf("placement = %q, want self-restoring sequence %q", rendered.String(), want)
	}
}

func TestPositionedKittyLayerRestoresRowOrigin(t *testing.T) {
	positioned := positionKittyLayer("layer", 23, 3)
	if want := "\x1b[3B\x1b[23Clayer\x1b[23D\x1b[3A"; positioned != want {
		t.Fatalf("positioned layer = %q, want %q", positioned, want)
	}
}

func TestKittyPenPlacementsPrecedeVisibleRow(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	m.targets = append(m.targets, target.Target{Name: "second-field"})
	m.statuses["second-field"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p2", Status: "idle"}}}
	m.state.ReconcileAgents("second-field", m.statuses["second-field"])

	view := testView(m)
	nameAt := strings.Index(view, "local")
	lastPlacementAt := -1
	for _, imageID := range []int{kittyDirtID, kittyTargetSignID, kittyFenceGateID, kittyFenceHorizID, kittyFenceVertID, kittyShepherdID, kittySheepImageID} {
		lastPlacementAt = max(lastPlacementAt, strings.LastIndex(view, fmt.Sprintf("\x1b_Ga=p,i=%d,", imageID)))
	}
	if nameAt < 0 || lastPlacementAt < 0 || lastPlacementAt > nameAt {
		t.Fatalf("Kitty placements were interleaved with visible pen cells: last placement=%d first pen=%d", lastPlacementAt, nameAt)
	}
}

func TestAgentCountChangeInvalidatesStaticPenGeometry(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	testView(m)
	agents := make([]herdr.Agent, 20)
	for i := range agents {
		agents[i] = herdr.Agent{PaneID: fmt.Sprintf("p%d", i+1), Status: "idle"}
	}
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: agents}

	changed := testView(m)
	if !strings.Contains(changed, fmt.Sprintf("a=p,i=%d", kittyDirtID)) ||
		!strings.Contains(changed, fmt.Sprintf("a=p,i=%d", kittyFenceHorizID)) {
		t.Fatal("changed pen geometry did not rebuild its static placements")
	}
}

func TestGridOffsetChangeRedrawsStaticPenPlacements(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	renderer := newKittyFrameRenderer()
	width := contentWidth(m.width)
	grid := m.layout(width)
	renderer.render(m.buildSceneForTest(width, grid))

	grid.offsets = append([]int(nil), grid.offsets...)
	grid.offsets[0]++
	changed := renderer.render(m.buildSceneForTest(width, grid))

	if !strings.Contains(changed, fmt.Sprintf("a=p,i=%d", kittyFenceHorizID)) {
		t.Fatal("changed pen origin did not redraw static fence placements")
	}
}

func TestPastureFrameDrawsNewStateAfterChange(t *testing.T) {
	agent := herdr.Agent{PaneID: "p1", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", "p1")
	status := m.statuses["local"]
	status.Agents[0].Status = "working"
	m.statuses["local"] = status
	grid := m.layout(contentWidth(m.width))
	m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
	for range 100 {
		if m.state.sheep[key].phase == present {
			break
		}
		m.advanceState(m.snapshot())
	}
	if m.state.sheep[key].phase != present {
		t.Fatal("working sheep did not reach its station")
	}
	m.state.step = 1
	rendered := testView(m)
	replacementID := kittyPoseImageIDs[sheepPose(status.Agents[0], m.state.sheep[key], m.state.step)]
	replacement := fmt.Sprintf("a=p,i=%d,p=", replacementID)
	if !strings.Contains(rendered, replacement) {
		t.Fatal("state change did not place the current sprite")
	}
	next := testView(m)
	if next != rendered {
		t.Fatal("settled working state was not idempotent")
	}
}

func TestKittyStateSequenceKeepsOneSheepPlacement(t *testing.T) {
	agent := herdr.Agent{PaneID: "p1", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	m.state.visible = true
	key := fleet.NewAgentKey("local", agent.PaneID)
	for _, statusName := range []string{"working", "blocked", "done", "idle"} {
		status := m.statuses["local"]
		status.Agents[0].Status = statusName
		m.statuses["local"] = status
		for range 100 {
			m.advanceState(m.snapshot())
			view := testView(m)
			current := kittyPoseImageIDs[sheepPose(status.Agents[0], m.state.sheep[key], m.state.step)]
			placement := fmt.Sprintf("a=p,i=%d,p=", current)
			if count := strings.Count(view, placement); count != 1 {
				t.Fatalf("%s transition placed current frame %d times, want 1", statusName, count)
			}
			if m.state.sheep[key].phase == present {
				break
			}
		}
		if m.state.sheep[key].phase != present {
			t.Fatalf("%s transition did not settle", statusName)
		}
	}
}

func TestAgentCountChangeRendersCompletePasture(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	testView(m)
	status := m.statuses["local"]
	for i := 2; i <= 12; i++ {
		status.Agents = append(status.Agents, herdr.Agent{PaneID: fmt.Sprintf("p%d", i), Status: "idle"})
	}
	m.statuses["local"] = status
	m.state.ReconcileAgents("local", status)
	rendered := testView(m)
	if !strings.Contains(rendered, fmt.Sprintf("a=p,i=%d", kittySheepImageID)) {
		t.Fatal("agent count change did not produce a complete placement frame")
	}
}

func TestSyncSeparatesOverlappingDoneSheep(t *testing.T) {
	agents := []herdr.Agent{{PaneID: "p1", Status: "done"}, {PaneID: "p2", Status: "done"}}
	m := physicsModel(t, agents)
	firstKey, secondKey := fleet.NewAgentKey("local", "p1"), fleet.NewAgentKey("local", "p2")
	first := m.state.sheep[firstKey]
	second := m.state.sheep[secondKey]
	second.x, second.y = first.x, first.y
	m.state.sheep[secondKey] = second

	m.syncState(contentWidth(m.width))
	if sheepOverlap(m.state.sheep[firstKey], m.state.sheep[secondKey]) {
		t.Fatalf("done sheep still overlap after sync: first=%+v second=%+v", m.state.sheep[firstKey], m.state.sheep[secondKey])
	}
}

func TestBatchEntryDoesNotStackSheepAtGate(t *testing.T) {
	m := physicsModel(t, nil)
	m.height = 50
	m.state.width = 0
	m.syncState(contentWidth(m.width))
	agents := []herdr.Agent{{PaneID: "p1", Status: "done"}, {PaneID: "p2", Status: "done"}}
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: agents}
	m.syncState(contentWidth(m.width))
	if sheepOverlap(m.state.sheep[fleet.NewAgentKey("local", "p1")], m.state.sheep[fleet.NewAgentKey("local", "p2")]) {
		t.Fatal("agents added in one snapshot were stacked at the gate")
	}
}

func TestGateOverlapDoesNotCancelNewAgentEntrance(t *testing.T) {
	existing := herdr.Agent{PaneID: "existing", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{existing})
	gateX, gateY := gateCenter(contentWidth(m.width))-sheepColumns/2, top+fenceRows
	position := m.state.sheep[fleet.NewAgentKey("local", existing.PaneID)]
	position.x, position.y = gateX, gateY
	m.state.sheep[fleet.NewAgentKey("local", existing.PaneID)] = position
	newAgent := herdr.Agent{PaneID: "new", Status: "working"}
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{existing, newAgent}}
	m.syncState(contentWidth(m.width))
	if got := m.state.sheep[fleet.NewAgentKey("local", newAgent.PaneID)].phase; got != entering {
		t.Fatalf("gate overlap changed new agent phase to %d", got)
	}
}

func TestPastureFrameClearsAndRebuildsFences(t *testing.T) {
	m := physicsModel(t, []herdr.Agent{{PaneID: "p1", Status: "idle"}})
	rendered := testView(m)
	if !strings.Contains(rendered, fmt.Sprintf("a=p,i=%d", kittyFenceHorizID)) {
		t.Fatal("pasture frame did not clear and rebuild horizontal fences")
	}
}

func TestKittyPastureRendersWithoutTargets(t *testing.T) {
	m := newTestModel(nil)
	m.state.visible = true
	m.width, m.height = 80, 30
	view := testView(m)
	if !strings.Contains(view, fmt.Sprintf("i=%d", kittyCastleID)) {
		t.Fatal("empty pasture did not retain the castle scene")
	}
}

func TestCompatibleNewerTargetUsesNormalRemovalAnimation(t *testing.T) {
	agent := herdr.Agent{PaneID: "one", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", agent.PaneID)
	m.statuses["local"] = poll.TargetStatus{
		State:  poll.Newer,
		Agents: []herdr.Agent{agent},
	}
	testView(m)

	m.state.ReconcileAgents("local", poll.TargetStatus{State: poll.Newer})
	m.state.ReconcileAgents("local", poll.TargetStatus{State: poll.Newer})

	if sheep := m.state.sheep[key]; sheep.phase != dying {
		t.Fatalf("newer-protocol removal phase = %v, want dying", sheep.phase)
	}
}

func TestPastureGridUsesEqualPenHeights(t *testing.T) {
	targets := []target.Target{{Name: "small"}, {Name: "medium"}, {Name: "large"}}
	m := newTestModel(targets)
	for i, configured := range targets {
		agents := make([]herdr.Agent, 1+i*3)
		for j := range agents {
			agents[j] = herdr.Agent{PaneID: fmt.Sprintf("%s-%d", configured.Name, j), Status: "idle"}
		}
		m.statuses[configured.Name] = poll.TargetStatus{State: poll.OK, Agents: agents}
	}
	m.width, m.height = 150, 50
	grid := m.layout(contentWidth(m.width))
	for i := 1; i < len(grid.heights); i++ {
		if grid.heights[i] != grid.heights[0] {
			t.Fatalf("pen heights vary with herd size: %v", grid.heights)
		}
	}
}

func TestPastureGridUsesBoundedCompactPens(t *testing.T) {
	targets := make([]target.Target, 6)
	for i := range targets {
		targets[i] = target.Target{Name: fmt.Sprintf("field-%d", i)}
	}
	m := newTestModel(targets)
	for _, configured := range targets {
		m.statuses[configured.Name] = poll.TargetStatus{State: poll.OK}
	}
	m.width, m.height = 160, 70
	grid := m.layout(contentWidth(m.width))
	if grid.columns != 4 || grid.penWidth < 36 || grid.penWidth > maximumPenColumns {
		t.Fatalf("compact grid = %d columns at width %d", grid.columns, grid.penWidth)
	}
	if grid.heights[0] != preferredPenRows || grid.visibleRows != 2 {
		t.Fatalf("compact grid heights = %v, visible rows %d", grid.heights, grid.visibleRows)
	}
}

func TestPastureGridDoesNotAdmitRowsBeyondHeightBudget(t *testing.T) {
	targets := []target.Target{{Name: "local"}}
	statuses := map[string]poll.TargetStatus{"local": {State: poll.OK, Agents: make([]herdr.Agent, 24)}}
	const height = 24
	grid := buildGrid(80, targets, statuses, height, 0)
	if grid.heights[0] != preferredPenRows || grid.visibleRows != 0 {
		t.Fatalf("undersized grid has pen height %d and %d visible rows", grid.heights[0], grid.visibleRows)
	}
	tiny := buildGrid(80, targets, statuses, royalSceneRows+4, 0)
	if tiny.heights[0] != preferredPenRows || tiny.visibleRows != 0 {
		t.Fatalf("tiny grid pen height=%d visible rows=%d", tiny.heights[0], tiny.visibleRows)
	}
}

func TestPastureGridDoesNotCompressPensBelowTheirMinimumHeight(t *testing.T) {
	targets := []target.Target{{Name: "local"}}
	statuses := map[string]poll.TargetStatus{"local": {State: poll.OK}}
	grid := buildGrid(80, targets, statuses, royalSceneRows+penGapRows+top+fenceRows+2, 0)
	if grid.heights[0] < preferredPenRows || grid.visibleRows != 0 {
		t.Fatalf("undersized grid has pen height %d and %d visible rows", grid.heights[0], grid.visibleRows)
	}
}

func TestMinimumPasturePenKeepsWorkyardAndSheepBelowTopFence(t *testing.T) {
	targets := []target.Target{{Name: "local"}}
	statuses := map[string]poll.TargetStatus{"local": {State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "idle"}}}}
	grid := buildGrid(80, targets, statuses, MinimumViewportHeight, 0)
	if grid.visibleRows != 1 {
		t.Fatalf("minimum viewport shows %d pen rows, want 1", grid.visibleRows)
	}
	height := grid.heights[0]
	columns, slotWidth := grazingGrid(grid.penWidth)
	grazing := sheepSlot("local", "p1", 0, grid.penWidth, height, columns, slotWidth)
	if workyardTop(height) < top+fenceRows || grazing.y < top+fenceRows {
		t.Fatalf("minimum pen places workyard at %d and sheep at %d above fence row %d", workyardTop(height), grazing.y, top+fenceRows)
	}
}

func TestPastureGridDoesNotChargeTrailingGap(t *testing.T) {
	targets := []target.Target{{Name: "first"}, {Name: "second"}}
	statuses := map[string]poll.TargetStatus{"first": {State: poll.OK}, "second": {State: poll.OK}}
	availableRows := 2*preferredPenRows + 2 + penGapRows
	grid := buildGrid(30, targets, statuses, royalSceneRows+availableRows, 0)
	if grid.visibleRows != 2 {
		t.Fatalf("grid shows %d rows when both rows fit without a trailing gap", grid.visibleRows)
	}
}

func TestPastureScrollStateIsNormalizedAndReset(t *testing.T) {
	targets := make([]target.Target, 6)
	statuses := make(map[string]poll.TargetStatus, len(targets))
	for i := range targets {
		targets[i] = target.Target{Name: fmt.Sprintf("field-%d", i)}
		statuses[targets[i].Name] = poll.TargetStatus{State: poll.OK}
	}
	frame := Frame{Snapshot: fleet.Snapshot{Targets: targets, Statuses: statuses}, Viewport: Viewport{Width: 50, Height: 40}}
	state := New(ASCII)
	if err := state.Open(frame); err != nil {
		t.Fatal(err)
	}
	state.scrollRow = 100
	before := state.layout(frame).firstRow
	state.Scroll(frame, -1)
	if after := state.layout(frame).firstRow; after != before-1 {
		t.Fatalf("first upward scroll moved from row %d to %d", before, after)
	}
	state.ScrollEnd(frame)
	state.Close()
	if err := state.Open(frame); err != nil {
		t.Fatal(err)
	}
	if first := state.layout(frame).firstRow; first != 0 {
		t.Fatalf("reopened pasture retained scroll row %d", first)
	}
}

func TestSparsePastureDoesNotStretchToViewport(t *testing.T) {
	m := newTestModel([]target.Target{{Name: "local"}})
	m.statuses["local"] = poll.TargetStatus{State: poll.OK}
	m.width, m.height = 160, 80
	grid := m.layout(contentWidth(m.width))
	scene := m.buildSceneForTest(contentWidth(m.width), grid)
	if pen := scene.rows[0].pens[0]; pen.bounds.width != maximumPenColumns || pen.bounds.height != preferredPenRows || pen.bounds.x <= 0 || pen.bounds.y != royalSceneRows+scene.gridTop+grid.offsets[0] {
		t.Fatalf("sparse pen stretched instead of centering: %+v", pen.bounds)
	}
	if difference := abs(scene.gridTop - scene.gridBottom); difference > 1 {
		t.Fatalf("compact grid is not vertically centered: top=%d bottom=%d", scene.gridTop, scene.gridBottom)
	}
}

func TestKittyPageChangeImmediatelyClearsOffscreenActors(t *testing.T) {
	targets := []target.Target{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	m := newTestModel(targets)
	for _, configured := range targets {
		m.statuses[configured.Name] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "idle"}}}
	}
	m.width, m.height = 80, 40
	width := contentWidth(m.width)
	grid := m.layout(width)
	m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
	renderer := newKittyRenderer()
	renderer.render(m.buildSceneForTest(width, grid))
	m.state.scroll(grid, 1)
	grid = m.layout(width)
	m.state.syncLayout(m.snapshot(), grid.penWidth, grid.heights)
	changed := renderer.render(m.buildSceneForTest(width, grid))
	// Idle agents have no loom.
	for _, imageID := range []int{kittySheepImageID, kittyShepherdID} {
		if !strings.Contains(changed, fmt.Sprintf("a=d,d=i,i=%d", imageID)) {
			t.Fatalf("page change retained offscreen image family %d", imageID)
		}
	}
}

func TestExistingWorkerKeepsLoomSlotWhenAnotherAgentLeaves(t *testing.T) {
	agents := []herdr.Agent{{PaneID: "first", Status: "working"}, {PaneID: "keeper", Status: "working"}}
	m := physicsModel(t, agents)
	key := fleet.NewAgentKey("local", "keeper")
	before := m.state.sheep[key]

	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: agents[1:]}
	m.syncState(contentWidth(m.width))
	after := m.state.sheep[key]
	if after.destinationX != before.destinationX || after.destinationY != before.destinationY {
		t.Fatalf("keeper loom moved from (%d,%d) to (%d,%d)", before.destinationX, before.destinationY, after.destinationX, after.destinationY)
	}
}

func TestOverlapRecoveryKeepsWorkerAtLoom(t *testing.T) {
	agents := []herdr.Agent{{PaneID: "worker", Status: "working"}, {PaneID: "idle", Status: "idle"}}
	m := physicsModel(t, agents)
	workerKey := fleet.NewAgentKey("local", "worker")
	idleKey := fleet.NewAgentKey("local", "idle")
	worker := m.state.sheep[workerKey]
	idle := m.state.sheep[idleKey]
	idle.x, idle.y = worker.x, worker.y
	m.state.sheep[idleKey] = idle

	m.state.separateSheep("local", agents, m.state.width, penHeight(len(agents), m.state.width), 1, m.state.width-sheepAreaOrigin-fenceColumns)
	after := m.state.sheep[workerKey]
	if after.x != worker.x || after.y != worker.y || after.destinationX != worker.destinationX || after.destinationY != worker.destinationY {
		t.Fatalf("overlap recovery moved worker from %+v to %+v", worker, after)
	}
}
