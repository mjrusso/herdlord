package pasture

import (
	"fmt"
	"strings"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

type pose uint8

const (
	poseSheepRestLeft pose = iota
	poseSheepRestRight
	poseSheepBlinkLeft
	poseSheepBlinkRight
	poseSheepWalkLeft
	poseSheepWalkRight
	poseSheepWorking
	poseSheepWorkingAlt
	poseSheepBlocked
	poseSheepStomp
	poseSheepDone
	poseSheepDoneAlt
	poseSheepWobble
	poseSheepFallen
	poseSheepSpirit
	poseShepherdCalm
	poseShepherdWorking
	poseShepherdAlert
	poseLordCalm
	poseLordDirecting
	poseLordAlert
	poseLoomWorking
	poseLoomWorkingAlt
	poseLoomJammed
	poseCount
)

type localPoint struct {
	x int
	y int
}

type sceneRect struct {
	x      int
	y      int
	width  int
	height int
}

type actorScene struct {
	position   localPoint
	pose       pose
	transition phase
	agent      herdr.Agent
}

type penScene struct {
	name     string
	status   poll.TargetStatus
	bounds   sceneRect
	workyard sceneRect
	sheep    []actorScene
	looms    []actorScene
	shepherd actorScene
	bubbles  map[bubbleKind]bubble
}

type rowScene struct {
	top         int
	height      int
	pens        []penScene
	roadCenters []int
}

type Scene struct {
	width       int
	tick        uint64
	rows        []rowScene
	castle      sceneRect
	lord        actorScene
	lordBubble  string
	roadCenters []int
	gridTop     int
	gridBottom  int
}

func (p *State) buildScene(fleet fleet.Snapshot, width, availableHeight int, grid grid) Scene {
	castleWidth := min(castleColumns, width)
	ground := royalSceneRows - royalRoadRows
	castleColumn, lordColumn := commandStationColumns(width, castleWidth, lordColumns, 2)
	scene := Scene{
		width:  width,
		tick:   p.step,
		castle: sceneRect{x: castleColumn, y: ground - castleRows, width: castleWidth, height: castleRows},
		lord: actorScene{
			position: localPoint{x: lordColumn, y: ground - lordRows},
			pose:     lordPose(fleet.Targets, fleet.Statuses),
		},
	}
	contentHeight := 0
	firstRow, lastRow := grid.visibleRowRange()
	if firstRow < lastRow {
		scene.roadCenters = grid.roadCenters(firstRow, width)
	}
	for row := firstRow; row < lastRow; row++ {
		contentHeight += grid.rowHeights[row]
	}
	if contentHeight > 0 {
		contentHeight -= penGapRows
	}
	unusedHeight := max(0, availableHeight-royalSceneRows-contentHeight)
	scene.gridTop = unusedHeight / 2
	scene.gridBottom = unusedHeight - scene.gridTop
	if bubble, exists := p.activeBubble(bubbleKey{kind: bubbleLord}); exists {
		scene.lordBubble = bubble.text
	}
	y := royalSceneRows + scene.gridTop
	for row := firstRow; row < lastRow; row++ {
		centers := grid.roadCenters(row, width)
		rowScene := rowScene{top: y, height: grid.rowHeights[row], roadCenters: centers}
		start := row * grid.columns
		end := min(start+grid.columns, len(fleet.Targets))
		for i := start; i < end; i++ {
			configured := fleet.Targets[i]
			column := i - start
			x := centers[column] - gateCenter(grid.penWidth)
			bounds := sceneRect{x: x, y: y + grid.offsets[i], width: grid.penWidth, height: grid.heights[i]}
			status := fleet.Statuses[configured.Name]
			rowScene.pens = append(rowScene.pens, p.buildPenScene(configured, status, bounds))
		}
		scene.rows = append(scene.rows, rowScene)
		y += rowScene.height
	}
	return scene
}

func (p *State) buildPenScene(configured target.Target, status poll.TargetStatus, bounds sceneRect) penScene {
	pen := penScene{
		name:     configured.Name,
		status:   status,
		bounds:   bounds,
		workyard: sceneRect{x: fenceColumns, y: workyardTop(bounds.height), width: bounds.width - fenceColumns*2, height: bounds.height - fenceRows - workyardTop(bounds.height)},
		bubbles:  make(map[bubbleKind]bubble),
	}
	shepherd := p.shepherds[configured.Name]
	pen.shepherd = actorScene{
		position: localPoint{x: shepherd.x, y: shepherd.y},
		pose:     shepherdPose(status),
	}
	for _, agent := range p.agents(configured.Name, status.Agents, !status.State.Usable()) {
		key := fleet.NewAgentKey(configured.Name, agent.PaneID)
		position := p.sheep[key]
		actor := actorScene{
			position:   localPoint{x: position.x, y: position.y},
			pose:       sheepPose(agent, position, p.step),
			transition: position.phase,
			agent:      agent,
		}
		pen.sheep = append(pen.sheep, actor)
		if pose, visible := loomPose(agent, position, p.step); visible {
			station := workSlot(configured.Name, agent.PaneID, position.workSlot, bounds.width, bounds.height)
			pen.looms = append(pen.looms, actorScene{
				position: localPoint{x: station.x, y: station.y},
				pose:     pose,
				agent:    agent,
			})
		}
	}
	if bubble, exists := p.activeBubble(sheepBubbleKey(configured.Name)); exists {
		pen.bubbles[bubbleSheep] = bubble
	}
	if bubble, exists := p.activeBubble(shepherdBubbleKey(configured.Name)); exists {
		pen.bubbles[bubbleShepherd] = bubble
	}
	return pen
}

func sceneObjectID(kind string, parts ...string) string {
	var id strings.Builder
	id.WriteString(kind)
	for _, part := range parts {
		fmt.Fprintf(&id, "\x00%d:%s", len(part), part)
	}
	return id.String()
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func sheepPose(agent herdr.Agent, sheep sheep, step uint64) pose {
	if sheep.phase == dying {
		switch {
		case sheep.phaseStep < deathWobbleEnd:
			return poseSheepWobble
		case sheep.phaseStep < deathFallenEnd:
			return poseSheepFallen
		default:
			return poseSheepSpirit
		}
	}
	if sheep.phase == entering || sheep.phase == relocating {
		return walkingPose(sheep.dx, step)
	}
	switch agent.Status {
	case "working":
		if (step/2)%2 == 1 {
			return poseSheepWorkingAlt
		}
		return poseSheepWorking
	case "blocked":
		if (step/2)%2 == 1 {
			return poseSheepStomp
		}
		return poseSheepBlocked
	case "done":
		if (step/3)%2 == 1 {
			return poseSheepDoneAlt
		}
		return poseSheepDone
	case "idle":
		if sheep.ambientSteps > 0 {
			return walkingPose(sheep.dx, step)
		}
		blink := (step/2+seed(agent.PaneID))%12 == 0
		return restingPose(sheep.dx, blink)
	default:
		blink := (step/2+seed(agent.PaneID))%8 == 0
		return restingPose(sheep.dx, blink)
	}
}

func walkingPose(direction int, step uint64) pose {
	walking := (step/2)%2 == 1
	if direction > 0 {
		if walking {
			return poseSheepWalkRight
		}
		return poseSheepRestRight
	}
	if walking {
		return poseSheepWalkLeft
	}
	return poseSheepRestLeft
}

func restingPose(direction int, blink bool) pose {
	if direction > 0 {
		if blink {
			return poseSheepBlinkRight
		}
		return poseSheepRestRight
	}
	if blink {
		return poseSheepBlinkLeft
	}
	return poseSheepRestLeft
}

func shepherdPose(status poll.TargetStatus) pose {
	if targetNeedsAttention(status) {
		return poseShepherdAlert
	}
	if status.State == poll.Checking || agentsHaveStatus(status.Agents, "working") {
		return poseShepherdWorking
	}
	return poseShepherdCalm
}

func lordPose(targets []target.Target, statuses map[string]poll.TargetStatus) pose {
	active := false
	for _, configured := range targets {
		status, ok := statuses[configured.Name]
		if !ok {
			active = true
			continue
		}
		if targetNeedsAttention(status) {
			return poseLordAlert
		}
		if status.State == poll.Checking || agentsHaveStatus(status.Agents, "working") {
			active = true
		}
	}
	if active {
		return poseLordDirecting
	}
	return poseLordCalm
}

func loomPose(agent herdr.Agent, sheep sheep, step uint64) (pose, bool) {
	if !agentUsesLoom(agent) || sheep.phase == dying {
		return 0, false
	}
	if agent.Status == "blocked" && sheep.phase == present {
		return poseLoomJammed, true
	}
	if agent.Status == "working" && (step/2)%2 == 1 {
		return poseLoomWorkingAlt, true
	}
	return poseLoomWorking, true
}
