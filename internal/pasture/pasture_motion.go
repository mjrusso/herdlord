package pasture

import (
	"cmp"
	"slices"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
)

type targetKey interface {
	comparable
	TargetName() string
}

func deleteForTarget[K targetKey, V any](entries map[K]V, name string) {
	for key := range entries {
		if key.TargetName() == name {
			delete(entries, key)
		}
	}
}

func (p *State) ClearTarget(name string) {
	deleteForTarget(p.sheep, name)
	deleteForTarget(p.missing, name)
	delete(p.shepherds, name)
	deleteForTarget(p.bubbles, name)
	deleteForTarget(p.bubbleCooldown, name)
	p.frameReady = false
	p.displayReady = false
}

func (p *State) syncLayout(snapshot fleet.Snapshot, width int, heights []int) {
	placeImmediately := !p.ready || p.width != width
	if p.width != width {
		p.width = width
		clear(p.sheep)
	}
	active := make(map[fleet.AgentKey]bool)
	for targetIndex, configured := range snapshot.Targets {
		status := snapshot.Statuses[configured.Name]
		height := penHeight(len(status.Agents), width)
		if targetIndex < len(heights) {
			height = heights[targetIndex]
		}
		p.syncTargetLayout(configured.Name, status, width, height, placeImmediately, active)
	}
	for key := range p.sheep {
		if !active[key] && p.missing[key] == 0 && p.sheep[key].phase != dying {
			delete(p.sheep, key)
		}
	}
	p.ready = true
}

func (p *State) syncTargetLayout(targetName string, status poll.TargetStatus, width, height int, placeImmediately bool, active map[fleet.AgentKey]bool) {
	if !status.State.Usable() {
		for key := range p.sheep {
			if key.Target == targetName {
				active[key] = true
			}
		}
	}
	p.syncShepherd(targetName, status, width, height, placeImmediately)
	workSlots := p.workSlots(targetName, status.Agents)
	columns, slotWidth := grazingGrid(width)
	entranceIndex := 0
	for i, agent := range status.Agents {
		key := fleet.NewAgentKey(targetName, agent.PaneID)
		active[key] = true
		destination := sheepSlot(targetName, agent.PaneID, i, width, height, columns, slotWidth)
		if agentUsesLoom(agent) {
			destination = workSlot(targetName, agent.PaneID, workSlots[key], width, height)
		}
		if existing, ok := p.sheep[key]; ok {
			p.sheep[key] = updateSheep(existing, agent, destination, workSlots[key])
			continue
		}
		seed := agentSeed(key)
		dx := 1
		if seed%2 == 0 {
			dx = -1
		}
		dy := int((seed/2)%3) - 1
		phase := entering
		startX := gateCenter(width) - sheepColumns/2
		startY := max(0, top+fenceRows-entranceIndex*sheepBodyRows)
		if placeImmediately {
			phase, startX, startY = present, destination.x, destination.y
		} else {
			entranceIndex++
		}
		p.sheep[key] = sheep{
			x: startX, y: startY,
			dx: dx, dy: dy, destinationX: destination.x, destinationY: destination.y,
			phase: phase, workSlot: workSlots[key], agent: agent,
		}
	}
	for key, sheep := range p.sheep {
		if key.Target == targetName {
			p.sheep[key] = constrainSheep(sheep, width, height)
		}
	}
	p.separateSheep(targetName, status.Agents, width, height, columns, slotWidth)
}

func updateSheep(sheep sheep, agent herdr.Agent, destination localPoint, workSlot int) sheep {
	sheep.workSlot = workSlot
	switch {
	case sheep.phase == entering || sheep.phase == relocating:
		sheep.destinationX, sheep.destinationY = destination.x, destination.y
	case sheep.phase != dying && agentUsesLoom(sheep.agent) != agentUsesLoom(agent):
		sheep.destinationX, sheep.destinationY = destination.x, destination.y
		sheep.phase = relocating
		sheep.phaseStep = 0
		sheep.ambientSteps = 0
	case sheep.phase != dying && agentUsesLoom(agent) &&
		(sheep.destinationX != destination.x || sheep.destinationY != destination.y):
		sheep.x, sheep.y = destination.x, destination.y
		sheep.destinationX, sheep.destinationY = destination.x, destination.y
	}
	sheep.agent = agent
	return sheep
}

func (p *State) workSlots(targetName string, agents []herdr.Agent) map[fleet.AgentKey]int {
	assigned := make(map[fleet.AgentKey]int)
	occupied := make(map[int]bool)
	for _, agent := range agents {
		key := fleet.NewAgentKey(targetName, agent.PaneID)
		sheep, exists := p.sheep[key]
		if agentUsesLoom(agent) && exists && !occupied[sheep.workSlot] {
			assigned[key] = sheep.workSlot
			occupied[sheep.workSlot] = true
		}
	}
	next := 0
	for _, agent := range agents {
		key := fleet.NewAgentKey(targetName, agent.PaneID)
		if !agentUsesLoom(agent) {
			continue
		}
		if _, exists := assigned[key]; exists {
			continue
		}
		for occupied[next] {
			next++
		}
		assigned[key] = next
		occupied[next] = true
	}
	return assigned
}

func (p *State) syncShepherd(targetName string, status poll.TargetStatus, width, height int, placeImmediately bool) {
	home := shepherd{x: fenceColumns + 1, y: top + fenceRows + 1}
	destination := home
	if agentsUseLoom(status.Agents) {
		destination.y = max(home.y, workyardTop(height)-shepherdRows)
	}
	destination.destinationX, destination.destinationY = destination.x, destination.y
	current, exists := p.shepherds[targetName]
	if !exists || placeImmediately {
		p.shepherds[targetName] = destination
		return
	}
	current.destinationX, current.destinationY = destination.x, destination.y
	p.shepherds[targetName] = current
}

func (p *State) separateSheep(targetName string, agents []herdr.Agent, width, height, columns, slotWidth int) {
	if !sheepOverlapForTarget(targetName, agents, p.sheep) {
		return
	}
	for slot, agent := range agents {
		if agentUsesLoom(agent) {
			continue
		}
		key := fleet.NewAgentKey(targetName, agent.PaneID)
		sheep := p.sheep[key]
		position := sheepSlot(targetName, agent.PaneID, slot, width, height, columns, slotWidth)
		sheep.x, sheep.y = position.x, position.y
		sheep.destinationX, sheep.destinationY = position.x, position.y
		sheep.phase = present
		sheep.phaseStep = 0
		sheep.ambientSteps = 0
		p.sheep[key] = sheep
	}
}

func sheepSlot(targetName, paneID string, slot, width, height, columns, slotWidth int) localPoint {
	seed := agentSeed(fleet.NewAgentKey(targetName, paneID))
	column := slot % columns
	row := slot / columns
	xSpace := max(0, slotWidth-sheepColumns)
	x := min(width-fenceColumns-sheepColumns, sheepAreaOrigin+column*slotWidth+int(seed%uint64(xSpace+1)))
	yBase := top + fenceRows + row*(sheepBodyRows+2)
	ySpace := max(0, min(2, height-fenceRows-sheepBodyRows-yBase))
	y := constrainSheepY(yBase+int((seed/7)%uint64(ySpace+1)), height)
	return localPoint{x: x, y: y}
}

func workSlot(targetName, paneID string, slot, width, height int) localPoint {
	columns, slotWidth := workyardGrid(width)
	seed := agentSeed(fleet.NewAgentKey(targetName, paneID))
	xSpace := max(0, slotWidth-sheepColumns)
	x := min(width-fenceColumns-sheepColumns, fenceColumns+(slot%columns)*slotWidth+int(seed%uint64(xSpace+1)))
	y := constrainSheepY(workyardTop(height)+(slot/columns)*sheepBodyRows, height)
	return localPoint{x: x, y: y}
}

func workyardTop(height int) int {
	interior := max(sheepBodyRows, height-top-fenceRows*2)
	rows := max(sheepBodyRows, interior/3)
	return height - fenceRows - rows
}

func constrainSheep(sheep sheep, width, height int) sheep {
	sheep.x = max(0, min(max(0, width-sheepColumns), sheep.x))
	sheep.y = constrainSheepY(sheep.y, height)
	return sheep
}

func constrainSheepY(y, height int) int {
	return max(0, min(max(0, height-fenceRows-sheepRows), y))
}

func agentUsesLoom(agent herdr.Agent) bool {
	return agent.Status == "working" || agent.Status == "blocked"
}

func agentsUseLoom(agents []herdr.Agent) bool {
	for _, agent := range agents {
		if agentUsesLoom(agent) {
			return true
		}
	}
	return false
}

func sheepOverlapForTarget(targetName string, agents []herdr.Agent, sheepByKey map[fleet.AgentKey]sheep) bool {
	for i, agent := range agents {
		first := sheepByKey[fleet.NewAgentKey(targetName, agent.PaneID)]
		for _, other := range agents[i+1:] {
			second := sheepByKey[fleet.NewAgentKey(targetName, other.PaneID)]
			if agent.Status == "working" && other.Status == "working" {
				continue
			}
			if !sheepInTransit(first) && !sheepInTransit(second) && sheepOverlap(first, second) {
				return true
			}
		}
	}
	return false
}

func sheepInTransit(sheep sheep) bool {
	return sheep.phase == entering || sheep.phase == relocating
}

func (p *State) ReconcileAgents(targetName string, status poll.TargetStatus) {
	if !status.State.Usable() {
		return
	}
	current := make(map[fleet.AgentKey]bool, len(status.Agents))
	for _, agent := range status.Agents {
		key := fleet.NewAgentKey(targetName, agent.PaneID)
		current[key] = true
		delete(p.missing, key)
		if sheep, ok := p.sheep[key]; ok {
			if sheep.phase == dying {
				sheep.phase = present
				sheep.phaseStep = 0
			}
			p.sheep[key] = sheep
		}
	}
	for key, sheep := range p.sheep {
		if key.Target != targetName {
			continue
		}
		if current[key] {
			continue
		}
		p.missing[key]++
		if p.missing[key] < 2 {
			continue
		}
		if sheep.phase != dying {
			sheep.phase = dying
			sheep.phaseStep = 0
			p.sheep[key] = sheep
		}
	}
	p.frameReady = false
	p.displayReady = false
}

func (p *State) advance(fleet fleet.Snapshot, grid grid) {
	p.expireBubbles()
	for targetIndex, configured := range fleet.Targets {
		p.advanceShepherd(configured.Name)
		status := fleet.Statuses[configured.Name]
		agents := p.agents(configured.Name, status.Agents, !status.State.Usable())
		p.advanceSheep(configured.Name, agents, grid.penWidth, grid.heights[targetIndex])
	}
}

func (p *State) expireBubbles() {
	for key, bubble := range p.bubbles {
		if p.step >= bubble.expires {
			delete(p.bubbles, key)
		}
	}
}

func (p *State) advanceShepherd(targetName string) {
	shepherd := p.shepherds[targetName]
	shepherd.x = moveToward(shepherd.x, shepherd.destinationX, 1)
	shepherd.y = moveToward(shepherd.y, shepherd.destinationY, 1)
	p.shepherds[targetName] = shepherd
}

func (p *State) advanceSheep(targetName string, agents []herdr.Agent, width, height int) {
	ambientMoving := false
	for _, agent := range agents {
		if p.sheep[fleet.NewAgentKey(targetName, agent.PaneID)].ambientSteps > 0 {
			ambientMoving = true
			break
		}
	}
	for _, agent := range agents {
		key := fleet.NewAgentKey(targetName, agent.PaneID)
		moving := p.sheep[key]
		switch moving.phase {
		case entering, relocating:
			if dx := directionToward(moving.x, moving.destinationX); dx != 0 {
				moving.dx = dx
			}
			moving.x = moveToward(moving.x, moving.destinationX, 4)
			moving.y += directionToward(moving.y, moving.destinationY)
			moving.phaseStep++
			if moving.x == moving.destinationX && moving.y == moving.destinationY {
				moving.phase = present
				moving.phaseStep = 0
			}
			p.sheep[key] = moving
			continue
		case dying:
			moving.phaseStep++
			if moving.phaseStep >= deathAnimationSteps {
				delete(p.sheep, key)
				delete(p.missing, key)
			} else {
				p.sheep[key] = moving
			}
			continue
		}
		if p.missing[key] > 0 || agent.Status != "idle" {
			continue
		}
		if moving.ambientSteps == 0 {
			if ambientMoving || (p.step+agentSeed(key))%ambientWalkInterval != 0 {
				continue
			}
			moving.ambientSteps, ambientMoving = 3, true
			p.sheep[key] = moving
		}
		p.advanceAmbientSheep(key, agents, width, height)
	}
}

func (p *State) advanceAmbientSheep(key fleet.AgentKey, agents []herdr.Agent, width, height int) {
	moving := p.sheep[key]
	next := moving
	next.x += next.dx
	next.y += next.dy
	if next.x < sheepAreaOrigin || next.x+sheepColumns > width-fenceColumns {
		moving.dx = -moving.dx
		next.x = moving.x + moving.dx
	}
	if next.y < top+fenceRows || next.y+sheepBodyRows > height-fenceRows {
		moving.dy = -moving.dy
		next.y = moving.y + moving.dy
	}
	next.x = max(sheepAreaOrigin, min(width-fenceColumns-sheepColumns, next.x))
	next.y = max(top+fenceRows, min(height-fenceRows-sheepBodyRows, next.y))
	collided := false
	for _, otherAgent := range agents {
		if key.Pane == otherAgent.PaneID {
			continue
		}
		otherKey := fleet.NewAgentKey(key.Target, otherAgent.PaneID)
		other := p.sheep[otherKey]
		if sheepOverlap(next, other) {
			collided = true
			if otherAgent.Status == "idle" {
				other.dx = -other.dx
				other.dy = -other.dy
				p.sheep[otherKey] = other
			}
			break
		}
	}
	if collided {
		moving.dx = -moving.dx
		moving.dy = -moving.dy
		moving.ambientSteps--
		p.sheep[key] = moving
		return
	}
	next.dx = moving.dx
	next.dy = moving.dy
	next.ambientSteps--
	p.sheep[key] = next
}

func directionToward(value, destination int) int {
	switch {
	case value < destination:
		return 1
	case value > destination:
		return -1
	default:
		return 0
	}
}

func moveToward(value, destination, distance int) int {
	if value < destination {
		return min(destination, value+distance)
	}
	return max(destination, value-distance)
}

func (p *State) agents(targetName string, current []herdr.Agent, preserveLastKnown bool) []herdr.Agent {
	agents := append([]herdr.Agent(nil), current...)
	present := make(map[fleet.AgentKey]bool, len(current))
	for _, agent := range current {
		present[fleet.NewAgentKey(targetName, agent.PaneID)] = true
	}
	var departed []herdr.Agent
	for key, sheep := range p.sheep {
		if key.Target == targetName && !present[key] && (preserveLastKnown || p.missing[key] > 0 || sheep.phase == dying) {
			departed = append(departed, sheep.agent)
		}
	}
	slices.SortFunc(departed, func(a, b herdr.Agent) int { return cmp.Compare(a.PaneID, b.PaneID) })
	agents = append(agents, departed...)
	return agents
}

func sheepOverlap(a, b sheep) bool {
	return a.x < b.x+sheepColumns && a.x+sheepColumns > b.x && a.y < b.y+sheepBodyRows && a.y+sheepBodyRows > b.y
}
