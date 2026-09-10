package pasture

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/poll"
)

type kittyRenderer struct{}

type kittyFrameRenderer struct {
	placements    map[string]uint32
	nextPlacement uint32
}

type animationKey string

func newKittyRenderer() *kittyRenderer {
	return &kittyRenderer{}
}

func newKittyFrameRenderer() *kittyFrameRenderer {
	return &kittyFrameRenderer{placements: make(map[string]uint32)}
}

func (r *kittyFrameRenderer) placementID(key string) uint32 {
	if id := r.placements[key]; id != 0 {
		return id
	}
	r.nextPlacement++
	r.placements[key] = r.nextPlacement
	return r.nextPlacement
}

func (r *kittyFrameRenderer) grassPlacementID(key string, column, row int) uint32 {
	return r.placementID(fmt.Sprintf("%s\x00grass\x00%d\x00%d", key, column, row))
}

func (r *kittyFrameRenderer) dirtPlacementID(targetName string) uint32 {
	return r.placementID(targetName + "\x00dirt")
}

var cachedKittyUpload = sync.OnceValues(buildKittyUpload)

func kittyUploadSprites() (string, error) {
	return cachedKittyUpload()
}

func buildKittyUpload() (string, error) {
	var out strings.Builder
	for _, sprite := range kittySprites {
		if sprite.active() {
			data, err := assets.ReadFile(sprite.path)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", sprite.path, err)
			}
			out.WriteString(kittyUploadSprite(sprite.id, data))
		}
	}
	return out.String(), nil
}

func kittyPurgeReservedImages() string {
	var out strings.Builder
	for _, sprite := range kittySprites {
		fmt.Fprintf(&out, "\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", sprite.id)
	}
	return out.String()
}

func kittyDeletePlacements(class kittySpriteClass) string {
	var out strings.Builder
	for _, sprite := range kittySprites {
		if sprite.active() && sprite.class&class != 0 {
			fmt.Fprintf(&out, "\x1b_Ga=d,d=i,i=%d,q=2;\x1b\\", sprite.id)
		}
	}
	return out.String()
}

func (r *kittyRenderer) enter() (string, error) {
	uploads, err := kittyUploadSprites()
	if err != nil {
		return "", err
	}
	return kittyPurgeReservedImages() + uploads, nil
}

func (r *kittyRenderer) leave() string {
	return kittyPurgeReservedImages()
}

func (r *kittyRenderer) render(scene Scene) string {
	return newKittyFrameRenderer().render(scene)
}

func (r *kittyFrameRenderer) render(scene Scene) string {
	contentWidth := scene.width
	var placements strings.Builder
	placements.WriteString(kittyDeletePlacements(kittyLayout | kittyForeground))
	royalPlacements, royalCanvas := r.royalSceneLayers(scene)
	placements.WriteString(royalPlacements)
	sections := []string{royalCanvas}
	if scene.gridTop > 0 {
		layer := r.gridPaddingPlacements(contentWidth, scene.gridTop, "top", scene.roadCenters)
		placements.WriteString(positionKittyLayer(layer, 0, royalSceneRows))
		sections = append(sections, blankKittyCanvas(contentWidth, scene.gridTop))
	}
	rows := make([]string, 0, len(scene.rows))
	for rowIndex, row := range scene.rows {
		pens := make([]string, 0, len(row.pens)*2+1)
		rowLayer := r.gridRowBackground(contentWidth, rowIndex, row)
		placements.WriteString(positionKittyLayer(rowLayer, 0, row.top))
		left := 0
		if len(row.pens) > 0 {
			left = row.pens[0].bounds.x
		}
		if left > 0 {
			pens = append(pens, strings.Repeat(" ", left))
		}
		for i, penScene := range row.pens {
			if i > 0 {
				pens = append(pens, strings.Repeat(" ", penGapColumns))
			}
			penOffset := penScene.bounds.y - row.top
			placements.WriteString(positionKittyLayer(r.placementLayerScene(penScene), penScene.bounds.x, penScene.bounds.y))
			pens = append(pens, strings.Repeat("\n", penOffset)+renderPenCanvasScene(penScene, scene.tick))
		}
		rowView := lipgloss.JoinHorizontal(lipgloss.Top, pens...)
		if rowIndex < len(scene.rows)-1 {
			rowView += "\n" + strings.Repeat(" ", scene.width)
		}
		rows = append(rows, rowView)
	}
	sections = append(sections, rows...)
	if scene.gridBottom > 0 {
		bottomTop := royalSceneRows + scene.gridTop
		if len(scene.rows) > 0 {
			last := scene.rows[len(scene.rows)-1]
			bottomTop = last.top + last.height
		}
		layer := r.gridPaddingPlacements(contentWidth, scene.gridBottom, "bottom", nil)
		placements.WriteString(positionKittyLayer(layer, 0, bottomTop))
		sections = append(sections, blankKittyCanvas(contentWidth, scene.gridBottom))
	}
	return placements.String() + strings.Join(sections, "\n")
}

func (r *kittyFrameRenderer) royalScene(scene Scene) string {
	placements, canvas := r.royalSceneLayers(scene)
	return placements + canvas
}

func (r *kittyFrameRenderer) royalSceneLayers(scene Scene) (string, string) {
	width := scene.width
	roadCenters := scene.roadCenters
	var out strings.Builder
	writeImagePlacement(&out, kittyRoyalBgID, r.placementID("royal-background"), placement{columns: width, rows: royalSceneRows, z: -1})
	if len(roadCenters) > 0 {
		firstRoad := roadCenters[0] - roadColumns/2
		lastRoad := roadCenters[len(roadCenters)-1] - roadColumns/2
		writeImagePlacement(&out, kittyRoadHorizID, r.placementID("royal-road-horizontal"), placement{column: firstRoad, row: royalSceneRows - royalRoadRows, columns: lastRoad - firstRoad + roadColumns, rows: royalRoadRows})
		for column, center := range roadCenters {
			x := center - roadColumns/2
			writeImagePlacement(&out, kittyRoadVertID, r.placementID(fmt.Sprintf("royal-road-%d", column)), placement{column: x, row: royalSceneRows - royalRoadRows, columns: roadColumns, rows: royalRoadRows})
		}
	}
	castleWidth, castleColumn, castleRow := scene.castle.width, scene.castle.x, scene.castle.y
	writeImagePlacementSource(&out, kittyCastleID, r.placementID("royal-castle"), placement{column: castleColumn, row: castleRow, columns: castleWidth, rows: scene.castle.height}, sourceRect{x: castleSourceX, y: castleSourceY, width: castleSourceWidth, height: castleSourceHeight})
	lordWidth := min(lordColumns, width)
	lordColumn, lordRow := scene.lord.position.x, scene.lord.position.y
	r.writeAnimatedImagePlacement(&out, lordAnimationKey(), kittyPoseImageIDs[scene.lord.pose], lordColumn, lordRow, lordWidth, lordRows, 1)
	cells := make([][]string, royalSceneRows)
	for row := range cells {
		cells[row] = make([]string, width)
		for column := range cells[row] {
			cells[row][column] = " "
		}
	}
	lordText := scene.lordBubble
	drawKittySideBubble(cells, lordText, lordColumn, lordRow, lordWidth, lordRows, width, royalSceneRows)
	lines := make([]string, royalSceneRows)
	for row := range cells {
		lines[row] = strings.Join(cells[row], "")
	}
	return out.String(), strings.Join(lines, "\n")
}

func (r *kittyFrameRenderer) gridRowBackground(width, rowIndex int, row rowScene) string {
	var out strings.Builder
	r.writeGrassField(&out, fmt.Sprintf("grid-background-%d", rowIndex), width, row.height, -3)
	centers := row.roadCenters
	firstRoad := centers[0] - roadColumns/2
	lastRoad := centers[len(centers)-1] - roadColumns/2
	writeImagePlacement(&out, kittyRoadHorizID, r.placementID(fmt.Sprintf("grid-road-horizontal-%d", rowIndex)), placement{column: firstRoad, columns: lastRoad - firstRoad + roadColumns, rows: royalRoadRows, z: -1})
	for i, center := range centers {
		length := row.pens[i].bounds.y - row.top
		if length == 0 {
			continue
		}
		writeImagePlacement(&out, kittyRoadVertID, r.placementID(fmt.Sprintf("grid-road-%d-%d", rowIndex, i)), placement{column: center - roadColumns/2, columns: roadColumns, rows: length, z: -1})
	}
	return out.String()
}

func (r *kittyFrameRenderer) gridPaddingPlacements(width, height int, key string, roadCenters []int) string {
	if height <= 0 {
		return ""
	}
	var out strings.Builder
	r.writeGrassField(&out, "grid-padding-"+key, width, height, -3)
	for index, center := range roadCenters {
		writeImagePlacement(&out, kittyRoadVertID, r.placementID(fmt.Sprintf("grid-padding-road-%s-%d", key, index)), placement{column: center - roadColumns/2, columns: roadColumns, rows: height, z: -1})
	}
	return out.String()
}

func blankKittyCanvas(width, height int) string {
	if height <= 0 {
		return ""
	}
	return strings.Repeat(strings.Repeat(" ", width)+"\n", height-1) + strings.Repeat(" ", width)
}

func renderPenCanvasScene(pen penScene, tick uint64) string {
	cells := make([][]string, pen.bounds.height)
	for row := range cells {
		cells[row] = make([]string, pen.bounds.width)
		for column := range cells[row] {
			cells[row][column] = " "
		}
	}
	renderTargetSign(cells, pen.name, pen.bounds.width)
	renderShepherdMarkerAt(cells, pen.status.State, tick)
	for _, sheep := range pen.sheep {
		renderAgentStatePose(cells, sheep)
	}
	renderPenSceneBubbles(cells, pen)
	lines := make([]string, pen.bounds.height)
	for row := range cells {
		lines[row] = strings.Join(cells[row], "")
	}
	return strings.Join(lines, "\n")
}

func renderPenSceneBubbles(cells [][]string, pen penScene) {
	if bubble, exists := pen.bubbles[bubbleSheep]; exists {
		for _, sheep := range pen.sheep {
			if sheep.agent.PaneID == bubble.pane {
				drawKittyBubble(cells, bubble.text, sheep.position.x, max(top+fenceRows, sheep.position.y-2), pen.bounds.width, pen.bounds.height)
				break
			}
		}
	}
	if bubble, exists := pen.bubbles[bubbleShepherd]; exists {
		drawKittyBubble(cells, bubble.text, pen.shepherd.position.x+sheepColumns, max(top+fenceRows, pen.shepherd.position.y-1), pen.bounds.width, pen.bounds.height)
	}
}

func drawKittyBubble(cells [][]string, text string, x, y, width, height int) {
	line, x, y, tail := bubbleLayout(text, x, y, width, height)
	if line == "" {
		return
	}
	styled := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color("230")).Render(line)
	cells[y][x] = styled
	for column := 1; column < ansi.StringWidth(line); column++ {
		cells[y][x+column] = ""
	}
	cells[y+1][tail] = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Render("v")
}

func drawKittySideBubble(cells [][]string, text string, actorX, actorY, actorWidth, actorHeight, width, height int) {
	line, x, y, pointerX, pointer := sideBubbleLayout(text, actorX, actorY, actorWidth, actorHeight, width, height)
	if line == "" {
		return
	}
	styled := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(lipgloss.Color("230")).Render(line)
	cells[y][x] = styled
	for column := 1; column < ansi.StringWidth(line); column++ {
		cells[y][x+column] = ""
	}
	cells[y][pointerX] = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Render(pointer)
}

func targetSignLayout(width int) (int, int) {
	columns := min(targetSignColumns, max(8, gateColumn(width)-2))
	return max(1, (gateColumn(width)-columns)/2), columns
}

func renderTargetSign(cells [][]string, targetName string, width int) {
	column, columns := targetSignLayout(width)
	label := ansi.Truncate(display.Text(targetName), columns-4, "…")
	styledLabel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Render(label)
	labelWidth := ansi.StringWidth(styledLabel)
	start := column + max(0, (columns-labelWidth)/2)
	cells[targetSignRows-2][start] = styledLabel
	for column := 1; column < labelWidth; column++ {
		cells[targetSignRows-2][start+column] = ""
	}
}

func renderShepherdMarkerAt(cells [][]string, state poll.State, tick uint64) {
	marker, color := "+", lipgloss.Color("2")
	switch state {
	case poll.Checking:
		marker, color = strings.Repeat(".", int(tick%3)+1), lipgloss.Color("6")
	case poll.Paused:
		marker, color = "z", lipgloss.Color("3")
	case poll.OK:
	default:
		marker, color = "!", lipgloss.Color("1")
	}
	start := fenceColumns + 1 + max(0, (sheepColumns-len(marker))/2)
	for i, char := range marker {
		cells[top+fenceRows][start+i] = lipgloss.NewStyle().Bold(true).Foreground(color).Render(string(char))
	}
}

func renderAgentStatePose(cells [][]string, actor actorScene) {
	marker := ""
	style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))
	switch actor.pose {
	case poseSheepWobble:
		marker = "!"
		style = style.Foreground(lipgloss.Color("9"))
	case poseSheepFallen:
		marker = "xx"
	case poseSheepSpirit:
		marker = "^"
		style = style.Foreground(lipgloss.Color("15"))
	case poseSheepBlocked, poseSheepStomp:
		marker = "!!"
		if actor.pose == poseSheepStomp {
			marker = "XX"
		}
		style = style.Foreground(lipgloss.Color("9"))
	default:
		if actor.agent.Status == "unknown" {
			marker = "?"
		}
	}
	start := actor.position.x + max(0, (sheepColumns-len(marker))/2)
	if marker == "" {
		return
	}
	if actor.position.y < 0 || actor.position.y >= len(cells) || start < 0 || start >= len(cells[actor.position.y]) {
		return
	}
	marker = ansi.Truncate(marker, len(cells[actor.position.y])-start, "")
	if marker == "" {
		return
	}
	styled := style.Render(marker)
	cells[actor.position.y][start] = styled
	for column := 1; column < len(marker) && start+column < len(cells[actor.position.y]); column++ {
		cells[actor.position.y][start+column] = ""
	}
}

func (r *kittyFrameRenderer) placementLayerScene(pen penScene) string {
	var out strings.Builder
	width, height, targetName := pen.bounds.width, pen.bounds.height, pen.name
	r.writeGrassField(&out, targetName, width, height, -2)
	r.writeWorkyardPlacements(&out, targetName, pen.workyard)
	roadColumn := max(0, gateCenter(width)-roadColumns/2)
	writeImagePlacement(&out, kittyRoadVertID, r.placementID(targetName+"\x00approach-road"), placement{column: roadColumn, columns: min(roadColumns, width), rows: top, z: -1})
	signColumn, signColumns := targetSignLayout(width)
	writeImagePlacement(&out, kittyTargetSignID, r.placementID(targetName+"\x00sign"), placement{column: signColumn, row: 1, columns: signColumns, rows: targetSignRows, z: -1})
	r.writeFencePlacements(&out, targetName, width, height)
	writeImagePlacement(&out, kittyFenceGateID, r.placementID(targetName+"\x00gate"), placement{column: gateColumn(width), row: top, columns: gateColumns, rows: fenceRows, z: 2})
	shepherdKey := shepherdAnimationKey(targetName)
	r.writeAnimatedImagePlacement(&out, shepherdKey, kittyPoseImageIDs[pen.shepherd.pose], pen.shepherd.position.x, pen.shepherd.position.y, sheepColumns, shepherdRows, 1)
	for _, loom := range pen.looms {
		key := loomAnimationKey(fleet.NewAgentKey(targetName, loom.agent.PaneID))
		r.writeAnimatedImagePlacement(&out, key, kittyPoseImageIDs[loom.pose], loom.position.x, loom.position.y+1, sheepColumns, sheepRows, 0)
	}
	for _, sheep := range pen.sheep {
		key := sheepAnimationKey(fleet.NewAgentKey(targetName, sheep.agent.PaneID))
		r.writeAnimatedImagePlacement(&out, key, kittyPoseImageIDs[sheep.pose], sheep.position.x, sheep.position.y+1, sheepColumns, sheepRows, 1)
	}
	return out.String()
}

func (r *kittyFrameRenderer) writeGrassField(out *strings.Builder, key string, width, height, z int) {
	for row := 0; row < height; row += grassTileRows {
		rows := min(grassTileRows, height-row)
		for column := 0; column < width; column += grassTileColumns {
			columns := min(grassTileColumns, width-column)
			id := r.grassPlacementID(key, column, row)
			writeImagePlacementSource(out, kittyGrassImageID, id, placement{column: column, row: row, columns: columns, rows: rows, z: z}, sourceRect{width: grassTilePixels * columns / grassTileColumns, height: grassTilePixels * rows / grassTileRows})
		}
	}
}

func sheepAnimationKey(key fleet.AgentKey) animationKey {
	return animationKey(sceneObjectID("agent-placement", key.Target, key.Pane))
}

func loomAnimationKey(key fleet.AgentKey) animationKey {
	return animationKey(sceneObjectID("loom-placement", key.Target, key.Pane))
}

func shepherdAnimationKey(targetName string) animationKey {
	return animationKey(sceneObjectID("shepherd-placement", targetName))
}

func lordAnimationKey() animationKey {
	return animationKey("royal-lord")
}

func (r *kittyFrameRenderer) writeWorkyardPlacements(out *strings.Builder, targetName string, workyard sceneRect) {
	id := r.dirtPlacementID(targetName)
	writeImagePlacement(out, kittyDirtID, id, placement{column: workyard.x, row: workyard.y, columns: workyard.width, rows: workyard.height, z: -1})
}

func (r *kittyFrameRenderer) writeFencePlacements(out *strings.Builder, targetName string, width, height int) {
	gate := gateColumn(width)
	r.writeHorizontalFence(out, targetName, top, 0, gate)
	r.writeHorizontalFence(out, targetName, top, gate+gateColumns, width)
	r.writeHorizontalFence(out, targetName, height-fenceRows, 0, width)
	for row := top; row < height; row += fenceTileRows {
		rows := min(fenceTileRows, height-row)
		for _, column := range []int{0, width - fenceColumns} {
			id := r.placementID(fmt.Sprintf("%s\x00fence-v\x00%d\x00%d", targetName, column, row))
			writeImagePlacementSource(out, kittyFenceVertID, id, placement{column: column, row: row, columns: fenceColumns, rows: rows, z: 2}, sourceRect{width: 64, height: 128 * rows / fenceTileRows})
		}
	}
}

func (r *kittyFrameRenderer) writeHorizontalFence(out *strings.Builder, targetName string, row, start, end int) {
	for column := start; column < end; column += fenceTileColumns {
		columns := min(fenceTileColumns, end-column)
		id := r.placementID(fmt.Sprintf("%s\x00fence-h\x00%d\x00%d", targetName, column, row))
		writeImagePlacementSource(out, kittyFenceHorizID, id, placement{column: column, row: row, columns: columns, rows: fenceRows, z: 2}, sourceRect{width: 128 * columns / fenceTileColumns, height: 64})
	}
}

func (r *kittyFrameRenderer) animatedPlacementID(key animationKey, imageID int) uint32 {
	return r.placementID(fmt.Sprintf("%s\x00frame\x00%d", key, imageID))
}

func (r *kittyFrameRenderer) writeAnimatedImagePlacement(out *strings.Builder, key animationKey, imageID, column, row, columns, rows, z int) {
	writeImagePlacement(out, imageID, r.animatedPlacementID(key, imageID), placement{column: column, row: row, columns: columns, rows: rows, z: z})
}
