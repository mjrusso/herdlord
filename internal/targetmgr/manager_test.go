package targetmgr

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

type blockingClient struct {
	started chan struct{}
	release chan struct{}
}

func (c blockingClient) Status(ctx context.Context, _ target.Target) (herdr.Status, error) {
	close(c.started)
	select {
	case <-c.release:
		return herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"}, nil
	case <-ctx.Done():
		return herdr.Status{}, ctx.Err()
	}
}

func (blockingClient) Snapshot(context.Context, target.Target, string) ([]herdr.Agent, error) {
	return nil, nil
}

func (blockingClient) Read(context.Context, target.Target, string, string, int) (string, error) {
	return "", nil
}

func TestAddCheckedRevalidatesAfterConcurrentMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	client := blockingClient{started: make(chan struct{}), release: make(chan struct{})}
	manager := Manager{Poller: poll.Manager{Client: client, Timeout: time.Second}}
	done := make(chan error, 1)
	go func() {
		_, err := manager.AddChecked(context.Background(), path, target.Target{Name: "box"})
		done <- err
	}()
	<-client.started
	if err := manager.Add(path, target.Target{Name: "box"}); err != nil {
		t.Fatal(err)
	}
	close(client.release)
	if err := <-done; err == nil {
		t.Fatal("checked add overwrote a concurrent addition")
	}
	assertTargets(t, path, []target.Target{{Name: "box"}})
}

func TestCheckedUpdateDoesNotRestoreConcurrentlyRemovedTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	initial := []target.Target{{Name: "box", Prefix: []string{"ssh", "old", "--"}}}
	if err := target.Save(path, initial); err != nil {
		t.Fatal(err)
	}
	client := blockingClient{started: make(chan struct{}), release: make(chan struct{})}
	manager := Manager{Poller: poll.Manager{Client: client, Timeout: time.Second}}
	done := make(chan error, 1)
	go func() {
		_, _, err := manager.Update(context.Background(), path, "box", func(configured *target.Target) error {
			configured.Prefix = []string{"ssh", "new", "--"}
			return nil
		}, true)
		done <- err
	}()
	<-client.started
	if _, err := manager.Remove(path, "box"); err != nil {
		t.Fatal(err)
	}
	close(client.release)
	if err := <-done; err == nil {
		t.Fatal("checked update restored a concurrently removed target")
	}
	assertTargets(t, path, nil)
}

func TestMutationFailurePreservesConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	initial := []target.Target{{Name: "one"}}
	if err := target.Save(path, initial); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Save: func(string, []target.Target) error { return errors.New("disk full") }}
	if err := manager.Add(path, target.Target{Name: "two"}); err == nil {
		t.Fatal("save failure was ignored")
	}
	assertTargets(t, path, initial)
}

func assertTargets(t *testing.T, path string, want []target.Target) {
	t.Helper()
	got, err := target.Load(path)
	equal := reflect.DeepEqual(got, want) || (len(got) == 0 && len(want) == 0)
	if err != nil || !equal {
		t.Fatalf("targets = %#v, want %#v, error = %v", got, want, err)
	}
}
