package targetmgr

import (
	"context"
	"fmt"

	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

type Manager struct {
	Poller poll.Manager
	Save   func(string, []target.Target) error
}

func (m Manager) Check(ctx context.Context, configured target.Target) (poll.TargetStatus, error) {
	status := m.Poller.Probe(ctx, configured)
	if err := ctx.Err(); err != nil {
		return poll.TargetStatus{}, err
	}
	return status, nil
}

func (m Manager) Add(path string, configured target.Target) error {
	_, err := m.mutate(path, func(current []target.Target) ([]target.Target, error) {
		updated := append(current, configured)
		if err := target.Validate(updated); err != nil {
			return nil, err
		}
		return updated, nil
	})
	return err
}

func (m Manager) AddChecked(ctx context.Context, path string, configured target.Target) (poll.TargetStatus, error) {
	current, err := target.Load(path)
	if err != nil {
		return poll.TargetStatus{}, err
	}
	if err := target.Validate(append(current, configured)); err != nil {
		return poll.TargetStatus{}, err
	}
	status, err := m.Check(ctx, configured)
	if err != nil {
		return poll.TargetStatus{}, err
	}
	return status, m.Add(path, configured)
}

func (m Manager) Update(ctx context.Context, path, name string, change func(*target.Target) error, probe bool) (target.Target, *poll.TargetStatus, error) {
	var configured target.Target
	var status *poll.TargetStatus
	if probe {
		current, err := target.Load(path)
		if err != nil {
			return configured, nil, err
		}
		i := index(current, name)
		if i < 0 {
			return configured, nil, notConfigured(name)
		}
		probeTarget := current[i]
		if err := change(&probeTarget); err != nil {
			return configured, nil, err
		}
		probeTarget.Paused = false
		result, err := m.Check(ctx, probeTarget)
		if err != nil {
			return configured, nil, err
		}
		status = &result
	}
	_, err := m.mutate(path, func(current []target.Target) ([]target.Target, error) {
		i := index(current, name)
		if i < 0 {
			return nil, notConfigured(name)
		}
		configured = current[i]
		if err := change(&configured); err != nil {
			return nil, err
		}
		current[i] = configured
		return current, nil
	})
	return configured, status, err
}

func (m Manager) SetPaused(path, name string, paused bool) (target.Target, error) {
	configured, _, err := m.Update(context.Background(), path, name, func(configured *target.Target) error {
		configured.Paused = paused
		return nil
	}, false)
	return configured, err
}

func (m Manager) TogglePaused(path, name string) (target.Target, error) {
	configured, _, err := m.Update(context.Background(), path, name, func(configured *target.Target) error {
		configured.Paused = !configured.Paused
		return nil
	}, false)
	return configured, err
}

func (m Manager) Remove(path, name string) (target.Target, error) {
	var removed target.Target
	_, err := m.mutate(path, func(current []target.Target) ([]target.Target, error) {
		i := index(current, name)
		if i < 0 {
			return nil, notConfigured(name)
		}
		removed = current[i]
		return append(current[:i], current[i+1:]...), nil
	})
	return removed, err
}

func (m Manager) mutate(path string, change func([]target.Target) ([]target.Target, error)) ([]target.Target, error) {
	if m.Save == nil {
		return target.Mutate(path, change)
	}
	current, err := target.Load(path)
	if err != nil {
		return nil, err
	}
	updated, err := change(current)
	if err != nil {
		return nil, err
	}
	if err := m.Save(path, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func notConfigured(name string) error {
	return fmt.Errorf("target %q is not configured", name)
}

func index(targets []target.Target, name string) int {
	for i := range targets {
		if targets[i].Name == name {
			return i
		}
	}
	return -1
}
