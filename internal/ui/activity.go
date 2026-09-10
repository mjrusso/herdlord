package ui

import (
	"fmt"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

const activityCapacity = 12

type activityLog struct {
	activity         []string
	activitySequence int
}

func (log *activityLog) add(message string) {
	log.activitySequence++
	log.activity = append(log.activity, message)
	if len(log.activity) > activityCapacity {
		log.activity = log.activity[len(log.activity)-activityCapacity:]
	}
}

func (m *Model) recordPollActivity(name string, change fleet.StatusChange) {
	if change.StateTransition {
		if message := targetStateActivity(name, change.Next.State); message != "" {
			m.recordActivity(message)
		}
	}
	for _, agent := range change.Agents {
		switch agent.Kind {
		case fleet.AgentAppeared:
			m.recordActivity(fmt.Sprintf("%s / %s appeared", name, agent.Next.PaneID))
		case fleet.AgentStatusChanged:
			m.recordActivity(fmt.Sprintf("%s / %s %s -> %s", name, agent.Next.PaneID, agent.Previous.Status, agent.Next.Status))
		case fleet.AgentDisappeared:
			m.recordActivity(fmt.Sprintf("%s / %s disappeared", name, agent.Previous.PaneID))
		}
	}
}

func targetStateActivity(name string, next poll.State) string {
	switch next {
	case poll.OK:
		return name + " recovered"
	case poll.Unreachable:
		return name + " became unreachable"
	case poll.NoHerdr:
		return name + " cannot find Herdr"
	case poll.Skewed:
		return name + " has an incompatible protocol"
	case poll.Newer:
		return name + " has a newer protocol"
	default:
		return ""
	}
}

func targetChangeActivity(previous, configured target.Target, existed bool) string {
	if !existed {
		return configured.Name + " target added"
	}
	if previous.Paused == configured.Paused {
		return configured.Name + " target updated"
	}
	if configured.Paused {
		return configured.Name + " target paused"
	}
	return configured.Name + " target resumed"
}
