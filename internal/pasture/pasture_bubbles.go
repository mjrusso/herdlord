package pasture

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
)

const (
	bubbleLow bubblePriority = iota
	bubbleNormal
	bubbleImportant
	bubbleUrgent
)

type bubblePriority uint8

const (
	bubbleLifetime = 5
	bubbleCooldown = 8
)

type bubble struct {
	text     string
	pane     string
	expires  uint64
	priority bubblePriority
}

type bubbleKind uint8

const (
	bubbleSheep bubbleKind = iota
	bubbleShepherd
	bubbleLord
)

type bubbleKey struct {
	kind   bubbleKind
	target string
}

func (k bubbleKey) TargetName() string { return k.target }

func sheepBubbleKey(targetName string) bubbleKey {
	return bubbleKey{kind: bubbleSheep, target: targetName}
}

func shepherdBubbleKey(targetName string) bubbleKey {
	return bubbleKey{kind: bubbleShepherd, target: targetName}
}

func (p *State) emitBubble(key bubbleKey, pane, text string, priority bubblePriority) {
	if !p.visible || text == "" {
		return
	}
	if current, exists := p.bubbles[key]; exists && p.step < current.expires && current.priority > priority {
		return
	}
	if p.step < p.bubbleCooldown[key] && priority <= bubbleNormal {
		return
	}
	p.bubbles[key] = bubble{text: text, pane: pane, expires: p.step + bubbleLifetime, priority: priority}
	p.bubbleCooldown[key] = p.step + bubbleCooldown
	p.displayReady = false
}

func (p *State) emitSheepBubble(targetName, pane, text string, priority bubblePriority) {
	p.emitBubble(sheepBubbleKey(targetName), pane, text, priority)
}

func (p *State) emitShepherdBubble(targetName, text string, priority bubblePriority) {
	p.emitBubble(shepherdBubbleKey(targetName), "", text, priority)
}

func (p *State) emitLordBubble(text string, priority bubblePriority) {
	p.emitBubble(bubbleKey{kind: bubbleLord}, "", text, priority)
}

func (p *State) activeBubble(key bubbleKey) (bubble, bool) {
	current, exists := p.bubbles[key]
	if !exists || p.step >= current.expires {
		return bubble{}, false
	}
	return current, true
}

func (p *State) RecordBubbles(snapshot fleet.Snapshot, targetName string, change fleet.StatusChange) {
	if !p.visible {
		return
	}
	if !change.StateTransition && len(change.Agents) == 0 {
		return
	}
	before := fleetSummary(snapshot, targetName, change.Previous)
	after := fleetSummary(snapshot, targetName, change.Next)
	if change.StateTransition {
		p.recordTargetBubbles(targetName, change.Next)
	}
	p.recordSheepBubbles(targetName, change.Agents)
	if before != after {
		p.emitFleetSummary(after)
	}
}

func (p *State) recordTargetBubbles(targetName string, next poll.TargetStatus) {
	message := ""
	priority := bubbleImportant
	switch next.State {
	case poll.OK:
		message = "All clear!"
		priority = bubbleNormal
	case poll.Unreachable:
		message = "Offline!"
	case poll.NoHerdr:
		message = "No Herdr!"
	case poll.Skewed:
		message = "Protocol?"
	case poll.Newer:
		message = "New version!"
	}
	if message != "" {
		p.emitShepherdBubble(targetName, message, priority)
	}
}

type fleetSummaryState struct {
	attention int
	active    int
}

func fleetSummary(fleet fleet.Snapshot, changed string, replacement poll.TargetStatus) fleetSummaryState {
	var summary fleetSummaryState
	for _, configured := range fleet.Targets {
		status, exists := fleet.Statuses[configured.Name]
		if configured.Name == changed {
			status = replacement
			exists = true
		}
		if !exists {
			summary.active++
			continue
		}
		if targetNeedsAttention(status) {
			summary.attention++
		} else if status.State == poll.Checking || agentsHaveStatus(status.Agents, "working") {
			summary.active++
		}
	}
	return summary
}

func (p *State) emitFleetSummary(summary fleetSummaryState) {
	text, priority := fleetSummaryMessage(summary)
	p.emitLordBubble(text, priority)
}

func fleetSummaryMessage(summary fleetSummaryState) (string, bubblePriority) {
	if summary.attention > 0 {
		verb := "need"
		if summary.attention == 1 {
			verb = "needs"
		}
		return fmt.Sprintf("%d %s %s aid!", summary.attention, herdWord(summary.attention), verb), bubbleUrgent
	}
	if summary.active > 0 {
		return fmt.Sprintf("Directing %d active.", summary.active), bubbleImportant
	}
	return "All herds clear.", bubbleImportant
}

func herdWord(count int) string {
	if count == 1 {
		return "herd"
	}
	return "herds"
}

func (p *State) recordSheepBubbles(targetName string, changes []fleet.AgentChange) {
	for _, change := range changes {
		switch change.Kind {
		case fleet.AgentAppeared:
			p.emitSheepBubble(targetName, change.Next.PaneID, agentBubbleLabel(change.Next), bubbleLow)
		case fleet.AgentStatusChanged:
			text, priority := agentStateBubble(change.Next.Status)
			p.emitSheepBubble(targetName, change.Next.PaneID, text, priority)
		case fleet.AgentDisappeared:
			p.emitSheepBubble(targetName, change.Previous.PaneID, "Farewell!", bubbleImportant)
		}
	}
}

func agentBubbleLabel(agent herdr.Agent) string {
	if title := bubbleLabelPart(agent.Title()); title != "" {
		return title
	}
	workspace, tab := bubbleLabelPart(agent.Workspace), bubbleLabelPart(agent.Tab)
	if workspace != "" && tab != "" {
		return workspace + " / " + tab
	}
	if workspace != "" {
		return workspace
	}
	if tab != "" {
		return tab
	}
	return bubbleLabelPart(agent.Agent)
}

func bubbleLabelPart(value string) string {
	return strings.TrimSpace(display.Text(value))
}

func agentStateBubble(status string) (string, bubblePriority) {
	switch status {
	case "blocked":
		return "Help!", bubbleUrgent
	case "done":
		return "Done!", bubbleImportant
	default:
		return "", bubbleLow
	}
}

func (p *State) maybeChatter(fleet fleet.Snapshot) {
	if p.step == 0 || p.step%chatInterval != 0 {
		return
	}
	var idle []struct{ target, pane string }
	for _, configured := range fleet.Targets {
		for _, agent := range fleet.Statuses[configured.Name].Agents {
			if agent.Status == "idle" {
				idle = append(idle, struct{ target, pane string }{configured.Name, agent.PaneID})
			}
		}
	}
	if len(idle) == 0 {
		return
	}
	selected := idle[int(p.step/chatInterval)%len(idle)]
	p.emitSheepBubble(selected.target, selected.pane, "Baa.", bubbleLow)
}

func bubbleLayout(text string, x, y, width, height int) (string, int, int, int) {
	if width < 3 || height < 2 {
		return "", 0, 0, 0
	}
	text = ansi.Truncate(text, max(0, width-4), "")
	line := "[" + text + "]"
	lineWidth := ansi.StringWidth(line)
	x = max(1, min(x, width-lineWidth-1))
	y = max(0, min(y, height-2))
	tail := x + min(lineWidth-1, max(1, lineWidth/2))
	return line, x, y, tail
}

func sideBubbleLayout(text string, actorX, actorY, actorWidth, actorHeight, width, height int) (string, int, int, int, string) {
	if text == "" || width < 5 || height < 1 {
		return "", 0, 0, 0, ""
	}
	maximum := max(1, width-actorWidth-4)
	text = ansi.Truncate(text, maximum, "")
	line := "[" + text + "]"
	lineWidth := ansi.StringWidth(line)
	y := max(0, min(actorY+actorHeight/2, height-1))
	if x := actorX + actorWidth + 1; x+lineWidth <= width {
		return line, x, y, x - 1, "<"
	}
	x := actorX - lineWidth - 1
	if x >= 0 {
		return line, x, y, actorX - 1, ">"
	}
	x = max(0, min(actorX, width-lineWidth))
	return line, x, y, max(0, min(actorX+actorWidth, width-1)), "<"
}
