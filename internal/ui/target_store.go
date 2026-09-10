package ui

import (
	"context"

	"github.com/mjrusso/herdlord/internal/demo"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
	"github.com/mjrusso/herdlord/internal/targetmgr"
)

type targetStore interface {
	load() ([]target.Target, error)
	watchable() bool
	add(configured target.Target) ([]target.Target, error)
	update(original string, configured target.Target) ([]target.Target, error)
	remove(name string) ([]target.Target, error)
	togglePaused(name string) ([]target.Target, error)
}

type fileTargetStore struct {
	path    string
	manager poll.Manager
}

func (s fileTargetStore) load() ([]target.Target, error) { return target.Load(s.path) }

func (s fileTargetStore) watchable() bool { return s.path != "" }

func (s fileTargetStore) add(configured target.Target) ([]target.Target, error) {
	manager := targetmgr.Manager{Poller: s.manager}
	if err := manager.Add(s.path, configured); err != nil {
		return nil, err
	}
	return s.load()
}

func (s fileTargetStore) update(original string, configured target.Target) ([]target.Target, error) {
	manager := targetmgr.Manager{Poller: s.manager}
	_, _, err := manager.Update(context.Background(), s.path, original, func(current *target.Target) error {
		paused := current.Paused
		*current = configured
		current.Paused = paused
		return nil
	}, false)
	if err != nil {
		return nil, err
	}
	return s.load()
}

func (s fileTargetStore) remove(name string) ([]target.Target, error) {
	if _, err := (targetmgr.Manager{Poller: s.manager}).Remove(s.path, name); err != nil {
		return nil, err
	}
	return s.load()
}

func (s fileTargetStore) togglePaused(name string) ([]target.Target, error) {
	if _, err := (targetmgr.Manager{Poller: s.manager}).TogglePaused(s.path, name); err != nil {
		return nil, err
	}
	return s.load()
}

type demoTargetStore struct {
	client *demo.Client
}

func (s demoTargetStore) load() ([]target.Target, error) { return s.client.Targets(), nil }

func (demoTargetStore) watchable() bool { return false }

func (s demoTargetStore) add(configured target.Target) ([]target.Target, error) {
	if err := s.client.AddTarget(configured); err != nil {
		return nil, err
	}
	return s.client.Targets(), nil
}

func (s demoTargetStore) update(original string, configured target.Target) ([]target.Target, error) {
	if err := s.client.UpdateTarget(original, configured); err != nil {
		return nil, err
	}
	return s.client.Targets(), nil
}

func (s demoTargetStore) remove(name string) ([]target.Target, error) {
	if err := s.client.RemoveTarget(name); err != nil {
		return nil, err
	}
	return s.client.Targets(), nil
}

func (s demoTargetStore) togglePaused(name string) ([]target.Target, error) {
	if err := s.client.ToggleTargetPaused(name); err != nil {
		return nil, err
	}
	return s.client.Targets(), nil
}
