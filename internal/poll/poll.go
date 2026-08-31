package poll

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/target"
)

type State int

const (
	OK State = iota
	Unreachable
	NoHerdr
	Skewed
	Newer
	Paused
	BackingOff
	Checking
)

func (s State) String() string {
	switch s {
	case OK:
		return "ok"
	case Unreachable:
		return "unreachable"
	case NoHerdr:
		return "no herdr"
	case Skewed:
		return "skewed"
	case Newer:
		return "newer"
	case Paused:
		return "paused"
	case BackingOff:
		return "backing off"
	case Checking:
		return "checking"
	default:
		return "unknown"
	}
}

func (s State) Usable() bool {
	return s == OK || s == Newer
}

type TargetStatus struct {
	State       State
	Err         string
	Protocol    int
	HerdrPath   string
	Version     string
	Agents      []herdr.Agent
	FetchedAt   time.Time
	LastSuccess time.Time
}

type Result struct {
	Name   string
	Status TargetStatus
}

type Sender interface{ Send(tea.Msg) }

type Client interface {
	Status(context.Context, target.Target) (herdr.Status, error)
	Snapshot(context.Context, target.Target, string) ([]herdr.Agent, error)
	Read(context.Context, target.Target, string, string, int) (string, error)
}

type Manager struct {
	Client   Client
	Interval time.Duration
	Timeout  time.Duration
	newTimer func(time.Duration) waitTimer
}

type waitTimer interface {
	channel() <-chan time.Time
	stop()
}

type realTimer struct{ timer *time.Timer }

func (t realTimer) channel() <-chan time.Time { return t.timer.C }
func (t realTimer) stop()                     { t.timer.Stop() }

func (m Manager) timer(duration time.Duration) waitTimer {
	if m.newTimer != nil {
		return m.newTimer(duration)
	}
	return realTimer{timer: time.NewTimer(duration)}
}

func (m Manager) EffectiveTimeout() time.Duration {
	if m.Timeout > 0 {
		return m.Timeout
	}
	return 10 * time.Second
}

func (m Manager) Run(ctx context.Context, t target.Target, sender Sender, refresh <-chan struct{}) {
	m.RunFrom(ctx, t, TargetStatus{}, sender, refresh)
}

func (m Manager) RunFrom(ctx context.Context, t target.Target, initial TargetStatus, sender Sender, refresh <-chan struct{}) {
	interval := m.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	timeout := m.EffectiveTimeout()
	if t.Paused {
		sender.Send(Result{Name: t.Name, Status: TargetStatus{State: Paused}})
		return
	}
	backoff := interval
	status := initial
	for {
		status = m.fetch(ctx, t, status, timeout)
		sender.Send(Result{Name: t.Name, Status: status})
		wait := interval
		if !status.State.Usable() {
			wait = backoff
			backoff = min(backoff*2, time.Minute)
			backingOff := status
			backingOff.State = BackingOff
			sender.Send(Result{Name: t.Name, Status: backingOff})
		} else {
			backoff = interval
		}
		timer := m.timer(wait)
		select {
		case <-ctx.Done():
			timer.stop()
			return
		case <-refresh:
			timer.stop()
		case <-timer.channel():
		}
	}
}

func (m Manager) Check(ctx context.Context, t target.Target) TargetStatus {
	return m.fetch(ctx, t, TargetStatus{}, m.EffectiveTimeout())
}

func (m Manager) Probe(parent context.Context, t target.Target) TargetStatus {
	if t.Paused {
		return TargetStatus{State: Paused}
	}
	ctx, cancel := context.WithTimeout(parent, m.EffectiveTimeout())
	defer cancel()
	probe, err := m.Client.Status(ctx, t)
	if err != nil {
		return failed(err)
	}
	now := time.Now()
	status := TargetStatus{Protocol: probe.Protocol, Version: probe.Version, HerdrPath: probe.Path, FetchedAt: now}
	if !herdr.CanAttemptProtocol(probe.Protocol) {
		status.State, status.Err = Skewed, (&herdr.ProtocolError{Got: probe.Protocol}).Error()
		return status
	}
	status.State, status.Err = protocolState(probe.Protocol)
	status.LastSuccess = now
	return status
}

func (m Manager) fetch(parent context.Context, t target.Target, previous TargetStatus, timeout time.Duration) TargetStatus {
	status := previous
	if status.HerdrPath == "" || !status.State.Usable() {
		ctx, cancel := context.WithTimeout(parent, timeout)
		probe, err := m.Client.Status(ctx, t)
		cancel()
		if err != nil {
			failure := failed(err)
			failure.LastSuccess = previous.LastSuccess
			return failure
		}
		status.HerdrPath, status.Protocol, status.Version = probe.Path, probe.Protocol, probe.Version
		if !herdr.CanAttemptProtocol(probe.Protocol) {
			status.State, status.Err, status.FetchedAt = Skewed, (&herdr.ProtocolError{Got: probe.Protocol}).Error(), time.Now()
			return status
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	agents, err := m.Client.Snapshot(ctx, t, status.HerdrPath)
	cancel()
	if err != nil {
		var protocolErr *herdr.ProtocolError
		if errors.As(err, &protocolErr) {
			status.State, status.Err, status.Protocol, status.FetchedAt = Skewed, protocolErr.Error(), protocolErr.Got, time.Now()
			return status
		}
		failedStatus := failed(err)
		failedStatus.Protocol, failedStatus.HerdrPath, failedStatus.Version, failedStatus.LastSuccess = status.Protocol, status.HerdrPath, status.Version, status.LastSuccess
		return failedStatus
	}
	now := time.Now()
	status.State, status.Err = protocolState(status.Protocol)
	status.Agents, status.FetchedAt, status.LastSuccess = agents, now, now
	return status
}

func protocolState(protocol int) (State, string) {
	if herdr.SupportsProtocol(protocol) {
		return OK, ""
	}
	return Newer, herdr.NewerProtocolWarning(protocol)
}

func failed(err error) TargetStatus {
	state := Unreachable
	var commandErr *herdr.CommandError
	if errors.As(err, &commandErr) && missingHerdr(commandErr.Stderr) {
		state = NoHerdr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		err = errors.New("timed out")
	}
	return TargetStatus{State: state, Err: err.Error(), FetchedAt: time.Now()}
}

func missingHerdr(stderr string) bool {
	message := strings.ToLower(stderr)
	return strings.Contains(message, "herdr: not found") ||
		(strings.Contains(message, "herdr") && strings.Contains(message, "no such file"))
}
