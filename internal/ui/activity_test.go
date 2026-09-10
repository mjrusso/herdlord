package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func TestActivityViewIsOneBoxedRowWithNewestEventFirst(t *testing.T) {
	m := newASCIIModel(nil, "", poll.Manager{})
	m.width = 80
	m.recordActivity("older event")
	m.recordActivity("newer event")

	view := ansi.Strip(m.activityView())
	if lipgloss.Height(view) != 3 {
		t.Fatalf("activity height = %d, want 3\n%s", lipgloss.Height(view), view)
	}
	if strings.Index(view, "newer event") > strings.Index(view, "older event") {
		t.Fatalf("newest event is not first: %q", view)
	}
}

func TestActivityViewTruncatesAtTerminalWidth(t *testing.T) {
	m := newASCIIModel(nil, "", poll.Manager{})
	m.width = 36
	m.recordActivity(strings.Repeat("long activity ", 8))

	view := ansi.Strip(m.activityView())
	if lipgloss.Width(view) > m.width || !strings.Contains(view, "…") {
		t.Fatalf("activity did not truncate to %d columns: width=%d\n%s", m.width, lipgloss.Width(view), view)
	}
}

func TestNormalPollChangesProduceActivity(t *testing.T) {
	configured := target.Target{Name: "box"}
	m := newASCIIModel([]target.Target{configured}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{})
	m.statuses[configured.Name] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "idle"}}}

	next := poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "working"}, {PaneID: "two", Status: "idle"}}}
	m.recordPollActivity(configured.Name, fleet.DiffStatus(m.statuses[configured.Name], next, true))
	activity := strings.Join(m.activity, "\n")
	for _, want := range []string{"box / one idle -> working", "box / two appeared"} {
		if !strings.Contains(activity, want) {
			t.Fatalf("activity missing %q: %s", want, activity)
		}
	}
}

func TestBackingOffPollDoesNotRepeatFailureActivity(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "box"}}, "", poll.Manager{})
	m.statuses["box"] = poll.TargetStatus{State: poll.BackingOff}
	next := poll.TargetStatus{State: poll.Unreachable}
	m.recordPollActivity("box", fleet.DiffStatus(m.statuses["box"], next, true))
	if len(m.activity) != 0 {
		t.Fatalf("retry repeated failure activity: %v", m.activity)
	}
}

func TestStatusDiffUsesOneTargetTransitionRule(t *testing.T) {
	tests := []struct {
		name     string
		previous poll.State
		next     poll.State
		exists   bool
		want     bool
	}{
		{name: "new target", next: poll.OK},
		{name: "initial check", previous: poll.Checking, next: poll.OK, exists: true},
		{name: "repeated failure", previous: poll.BackingOff, next: poll.Unreachable, exists: true},
		{name: "recovery", previous: poll.BackingOff, next: poll.OK, exists: true, want: true},
		{name: "failure", previous: poll.OK, next: poll.Unreachable, exists: true, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			change := fleet.DiffStatus(poll.TargetStatus{State: test.previous}, poll.TargetStatus{State: test.next}, test.exists)
			if change.StateTransition != test.want {
				t.Fatalf("stateTransition = %v, want %v", change.StateTransition, test.want)
			}
		})
	}
}

func TestTargetReconciliationProducesActivity(t *testing.T) {
	m := newASCIIModel([]target.Target{{Name: "old"}, {Name: "paused"}}, "", poll.Manager{})
	m.reconcile([]target.Target{{Name: "paused", Paused: true}, {Name: "new"}})
	activity := strings.Join(m.activity, "\n")
	for _, want := range []string{"old target removed", "paused target paused", "new target added"} {
		if !strings.Contains(activity, want) {
			t.Fatalf("activity missing %q: %s", want, activity)
		}
	}
}

func TestRemovedTargetActivityUsesConfiguredOrder(t *testing.T) {
	targets := []target.Target{{Name: "zeta"}, {Name: "alpha"}, {Name: "middle"}}
	want := "zeta target removed\nalpha target removed\nmiddle target removed"
	for range 50 {
		m := newASCIIModel(targets, "", poll.Manager{})
		m.reconcile(nil)
		if got := strings.Join(m.activity, "\n"); got != want {
			t.Fatalf("removal activity order = %q, want %q", got, want)
		}
	}
}
