package poll

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/target"
)

type fakeClient struct {
	mu            sync.Mutex
	status        herdr.Status
	statusErr     error
	snapshot      []herdr.Agent
	snapshotErrs  []error
	statusCalls   int
	snapshotCalls int
	blockStatus   bool
	statusStarted chan struct{}
	statusDelay   time.Duration
	snapshotDelay time.Duration
}

func (c *fakeClient) Status(ctx context.Context, _ target.Target) (herdr.Status, error) {
	c.mu.Lock()
	c.statusCalls++
	block, delay := c.blockStatus, c.statusDelay
	status, err := c.status, c.statusErr
	c.mu.Unlock()
	if c.statusStarted != nil {
		select {
		case c.statusStarted <- struct{}{}:
		default:
		}
	}
	if block {
		<-ctx.Done()
		return herdr.Status{}, ctx.Err()
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return herdr.Status{}, ctx.Err()
		}
	}
	return status, err
}

func (c *fakeClient) Snapshot(ctx context.Context, _ target.Target, _ string) ([]herdr.Agent, error) {
	c.mu.Lock()
	i := c.snapshotCalls
	c.snapshotCalls++
	delay := c.snapshotDelay
	snapshot := c.snapshot
	var err error
	if i < len(c.snapshotErrs) && c.snapshotErrs[i] != nil {
		err = c.snapshotErrs[i]
	}
	c.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return snapshot, err
}

func (*fakeClient) Read(context.Context, target.Target, string, string, int) (string, error) {
	return "", nil
}

type channelSender chan tea.Msg

func (s channelSender) Send(msg tea.Msg) { s <- msg }

type manualTimer struct{ tick chan time.Time }

func (t *manualTimer) channel() <-chan time.Time { return t.tick }
func (*manualTimer) stop()                       {}

type timerRequest struct {
	duration time.Duration
	timer    *manualTimer
}

func TestCheckSuccessCachesProbeData(t *testing.T) {
	client := &fakeClient{
		status:   herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"},
		snapshot: []herdr.Agent{{PaneID: "w1:p1"}},
	}
	got := (Manager{Client: client, Timeout: time.Second}).Check(context.Background(), target.Target{Name: "box"})
	if got.State != OK || got.HerdrPath != "/opt/herdr" || got.Version != "0.8.0" || len(got.Agents) != 1 {
		t.Fatalf("Check() = %#v", got)
	}
}

func TestCheckAcceptsProtocol19(t *testing.T) {
	client := &fakeClient{
		status:   herdr.Status{Protocol: 19, Version: "0.8.0", Path: "/opt/herdr"},
		snapshot: []herdr.Agent{{PaneID: "w1:p1"}},
	}
	got := (Manager{Client: client, Timeout: time.Second}).Check(context.Background(), target.Target{Name: "box"})
	if got.State != OK || got.Protocol != 19 || len(got.Agents) != 1 {
		t.Fatalf("Check() = %#v", got)
	}
}

func TestCheckClassifiesStatusAndSnapshotSkew(t *testing.T) {
	tests := []struct {
		name   string
		client *fakeClient
		want   int
	}{
		{"status", &fakeClient{status: herdr.Status{Protocol: 18, Version: "0.7.0", Path: "/opt/herdr"}}, 18},
		{"snapshot", &fakeClient{status: herdr.Status{Protocol: herdr.Protocol, Version: "0.8.2", Path: "/opt/herdr"}, snapshotErrs: []error{&herdr.ProtocolError{Got: 18}}}, 18},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (Manager{Client: tt.client, Timeout: time.Second}).Check(context.Background(), target.Target{Name: "box"})
			if got.State != Skewed || got.Protocol != tt.want {
				t.Fatalf("Check() = %#v", got)
			}
		})
	}
}

func TestCheckAttemptsNewerProtocol(t *testing.T) {
	client := &fakeClient{
		status:   herdr.Status{Protocol: 21, Version: "0.9.0", Path: "/opt/herdr"},
		snapshot: []herdr.Agent{{PaneID: "w1:p1"}},
	}
	got := (Manager{Client: client, Timeout: time.Second}).Check(context.Background(), target.Target{Name: "box"})
	if got.State != Newer || got.Protocol != 21 || len(got.Agents) != 1 || got.LastSuccess.IsZero() || !strings.Contains(got.Err, "attempting compatibility") {
		t.Fatalf("Check() = %#v", got)
	}
}

func TestCheckTimeout(t *testing.T) {
	got := (Manager{Client: &fakeClient{blockStatus: true}, Timeout: 20 * time.Millisecond}).Check(context.Background(), target.Target{Name: "hung"})
	if got.State != Unreachable || got.Err != "timed out" {
		t.Fatalf("Check() = %#v", got)
	}
}

func TestCheckAppliesTimeoutPerCommand(t *testing.T) {
	client := &fakeClient{
		status:        herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"},
		statusDelay:   20 * time.Millisecond,
		snapshotDelay: 20 * time.Millisecond,
	}
	got := (Manager{Client: client, Timeout: 30 * time.Millisecond}).Check(context.Background(), target.Target{Name: "slow"})
	if got.State != OK {
		t.Fatalf("Check() = %#v", got)
	}
}

func TestRunHungTargetDoesNotDelayHealthyTarget(t *testing.T) {
	sender := make(channelSender, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	healthy := Manager{
		Client: &fakeClient{
			status:   herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"},
			snapshot: []herdr.Agent{{PaneID: "w1:p1"}},
		},
		Interval: time.Hour,
		Timeout:  500 * time.Millisecond,
	}
	hungStarted := make(chan struct{}, 1)
	hung := Manager{
		Client:   &fakeClient{blockStatus: true, statusStarted: hungStarted},
		Interval: time.Hour,
		Timeout:  500 * time.Millisecond,
	}
	go hung.Run(ctx, target.Target{Name: "hung"}, sender, make(chan struct{}))
	select {
	case <-hungStarted:
	case <-time.After(time.Second):
		t.Fatal("hung target did not start")
	}
	go healthy.Run(ctx, target.Target{Name: "healthy"}, sender, make(chan struct{}))

	select {
	case msg := <-sender:
		result := msg.(Result)
		if result.Name != "healthy" || result.Status.State != OK {
			t.Fatalf("first result = %#v", result)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("healthy target was delayed by hung target")
	}
}

func TestFailureClassification(t *testing.T) {
	missing := &herdr.CommandError{Stderr: "env: herdr: No such file or directory", Err: errors.New("exit status 127")}
	if got := failed(missing); got.State != NoHerdr {
		t.Fatalf("missing Herdr state = %v", got.State)
	}
	transport := &herdr.CommandError{Stderr: "ssh: connect to host failed", Err: errors.New("exit status 255")}
	if got := failed(transport); got.State != Unreachable {
		t.Fatalf("transport state = %v", got.State)
	}
}

func TestRunBacksOffAndResetsAfterSuccess(t *testing.T) {
	client := &fakeClient{
		status:       herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"},
		snapshot:     []herdr.Agent{{PaneID: "w1:p1"}},
		snapshotErrs: []error{errors.New("first failure"), errors.New("second failure"), nil},
	}
	sender := make(channelSender, 8)
	timers := make(chan timerRequest, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := Manager{
		Client:   client,
		Interval: 10 * time.Millisecond,
		Timeout:  time.Second,
		newTimer: func(duration time.Duration) waitTimer {
			timer := &manualTimer{tick: make(chan time.Time, 1)}
			timers <- timerRequest{duration: duration, timer: timer}
			return timer
		},
	}
	go manager.Run(ctx, target.Target{Name: "box"}, sender, make(chan struct{}))

	wantDelays := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 10 * time.Millisecond}
	for i, want := range wantDelays {
		select {
		case request := <-timers:
			if request.duration != want {
				t.Fatalf("timer %d = %v, want %v", i, request.duration, want)
			}
			if i < len(wantDelays)-1 {
				request.timer.tick <- time.Now()
			}
		case <-time.After(time.Second):
			t.Fatalf("timer %d was not created", i)
		}
	}
	cancel()

	var states []State
	for len(sender) > 0 {
		states = append(states, (<-sender).(Result).Status.State)
	}
	wantStates := []State{Unreachable, BackingOff, Unreachable, BackingOff, OK}
	if !reflect.DeepEqual(states, wantStates) {
		t.Fatalf("states = %#v, want %#v", states, wantStates)
	}
}

func TestRunPausedDoesNotPoll(t *testing.T) {
	client := &fakeClient{}
	sender := make(channelSender, 1)
	(Manager{Client: client}).Run(context.Background(), target.Target{Name: "box", Paused: true}, sender, make(chan struct{}))
	result := (<-sender).(Result)
	if result.Status.State != Paused || client.statusCalls != 0 {
		t.Fatalf("paused result = %#v, calls = %d", result, client.statusCalls)
	}
}

func TestFailurePreservesLastSuccess(t *testing.T) {
	client := &fakeClient{
		status:       herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"},
		snapshot:     []herdr.Agent{{PaneID: "w1:p1"}},
		snapshotErrs: []error{nil, errors.New("offline")},
	}
	manager := Manager{Client: client}
	first := manager.fetch(context.Background(), target.Target{Name: "box"}, TargetStatus{}, time.Second)
	if first.State != OK || first.LastSuccess.IsZero() {
		t.Fatalf("successful status = %#v", first)
	}
	second := manager.fetch(context.Background(), target.Target{Name: "box"}, first, time.Second)
	if second.State != Unreachable || !second.LastSuccess.Equal(first.LastSuccess) {
		t.Fatalf("failed status = %#v, first success = %v", second, first.LastSuccess)
	}
}
