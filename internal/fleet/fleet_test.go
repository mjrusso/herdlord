package fleet

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

type fleetClient struct {
	delays  map[string]time.Duration
	mu      sync.Mutex
	probes  []string
	started chan string
	release <-chan struct{}
}

func (c *fleetClient) Status(ctx context.Context, configured target.Target) (herdr.Status, error) {
	c.mu.Lock()
	c.probes = append(c.probes, configured.Name)
	c.mu.Unlock()
	if c.started != nil {
		c.started <- configured.Name
	}
	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
			return herdr.Status{}, ctx.Err()
		}
	}
	if delay := c.delays[configured.Name]; delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return herdr.Status{}, ctx.Err()
		}
	}
	return herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"}, nil
}

func (*fleetClient) Snapshot(context.Context, target.Target, string) ([]herdr.Agent, error) {
	return nil, nil
}

func (*fleetClient) Read(context.Context, target.Target, string, string, int) (string, error) {
	return "", nil
}

func TestCollectRunsConcurrentlyAndPreservesConfigurationOrder(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	client := &fleetClient{started: started, release: release}
	targets := []target.Target{{Name: "first"}, {Name: "paused", Paused: true}, {Name: "third"}}
	type result struct {
		observations []Observation
		err          error
	}
	done := make(chan result, 1)
	go func() {
		got, err := (Collector{Manager: poll.Manager{Client: client, Timeout: time.Second}}).Collect(context.Background(), targets, nil, true, true)
		done <- result{observations: got, err: err}
	}()
	seen := make(map[string]bool, 2)
	for len(seen) < 2 {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("only started probes %#v; independent probes appear serialized", seen)
		}
	}
	if !seen["first"] || !seen["third"] {
		t.Fatalf("started probes = %#v", seen)
	}
	close(release)
	resultValue := <-done
	got, err := resultValue.observations, resultValue.err
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Target.Name != "first" || got[1].Target.Name != "paused" || got[2].Target.Name != "third" {
		t.Fatalf("observations = %#v", got)
	}
	if got[1].Status.State != poll.Paused {
		t.Fatalf("paused status = %#v", got[1].Status)
	}
}

func TestCollectHonorsCancellation(t *testing.T) {
	client := &fleetClient{delays: map[string]time.Duration{"one": time.Second, "two": time.Second}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := (Collector{Manager: poll.Manager{Client: client, Timeout: time.Second}}).Collect(ctx, []target.Target{{Name: "one"}, {Name: "two"}}, nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Status.State != poll.Unreachable || got[1].Status.State != poll.Unreachable {
		t.Fatalf("canceled observations = %#v", got)
	}
}

func TestSelectTargetsAndAllFailed(t *testing.T) {
	targets := []target.Target{{Name: "one"}, {Name: "paused", Paused: true}}
	selected, err := selectTargets(targets, []string{"paused"}, false)
	if err != nil || len(selected) != 0 {
		t.Fatalf("selected = %#v, %v", selected, err)
	}
	if _, err := selectTargets(targets, []string{"missing"}, true); err == nil {
		t.Fatal("missing requested target was accepted")
	}
	if !AllFailed([]Observation{{Status: poll.TargetStatus{State: poll.Unreachable}}}) {
		t.Fatal("failed observation was treated as successful")
	}
	if AllFailed([]Observation{{Status: poll.TargetStatus{State: poll.Paused}}}) || AllFailed(nil) {
		t.Fatal("paused or empty observations were treated as all failed")
	}
	if AllFailed([]Observation{{Status: poll.TargetStatus{State: poll.Newer}}}) {
		t.Fatal("newer protocol observation was treated as failed")
	}
}
