package fleet

import (
	"context"
	"fmt"
	"sync"

	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

type Observation struct {
	Target target.Target
	Status poll.TargetStatus
}

type Collector struct {
	Manager poll.Manager
}

func (c Collector) Collect(ctx context.Context, targets []target.Target, names []string, includePaused bool, probeOnly bool) ([]Observation, error) {
	selected, err := selectTargets(targets, names, includePaused)
	if err != nil {
		return nil, err
	}
	results := make([]Observation, len(selected))
	var wg sync.WaitGroup
	for i, selectedTarget := range selected {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := poll.TargetStatus{State: poll.Paused}
			if !selectedTarget.Paused {
				if probeOnly {
					status = c.Manager.Probe(ctx, selectedTarget)
				} else {
					status = c.Manager.Check(ctx, selectedTarget)
				}
			}
			results[i] = Observation{Target: selectedTarget, Status: status}
		}()
	}
	wg.Wait()
	return results, nil
}

func selectTargets(targets []target.Target, names []string, includePaused bool) ([]target.Target, error) {
	requested := make(map[string]struct{}, len(names))
	for _, name := range names {
		requested[name] = struct{}{}
	}
	found := make(map[string]struct{}, len(names))
	selected := make([]target.Target, 0, len(targets))
	for _, candidate := range targets {
		if len(requested) > 0 {
			if _, ok := requested[candidate.Name]; !ok {
				continue
			}
			found[candidate.Name] = struct{}{}
		}
		if candidate.Paused && !includePaused {
			continue
		}
		selected = append(selected, candidate)
	}
	for name := range requested {
		if _, ok := found[name]; !ok {
			return nil, fmt.Errorf("target %q is not configured", name)
		}
	}
	return selected, nil
}

func AllFailed(observations []Observation) bool {
	if len(observations) == 0 {
		return false
	}
	for _, observation := range observations {
		if observation.Status.State.Usable() || observation.Status.State == poll.Paused {
			return false
		}
	}
	return true
}
