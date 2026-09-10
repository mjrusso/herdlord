package pasture

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func TestInitialSnapshotDoesNotCreatePastureBubbles(t *testing.T) {
	m := newTestModel([]target.Target{{Name: "box"}})
	m.state.visible = true
	m.statuses["box"] = poll.TargetStatus{State: poll.Checking}
	recordTestChange(m, "box", poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "done"}}})
	if len(m.state.bubbles) != 0 {
		t.Fatalf("initial snapshot created bubbles: %#v", m.state.bubbles)
	}
}

func TestAgentTransitionsCreatePrioritizedPastureBubbles(t *testing.T) {
	m := newTestModel([]target.Target{{Name: "box"}})
	m.state.visible = true
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "idle"}}}
	recordTestChange(m, "box", poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "working"}}})
	if _, exists := m.state.bubbles[sheepBubbleKey("box")]; exists {
		t.Fatal("working transition created a redundant bubble")
	}

	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "working"}}}
	recordTestChange(m, "box", poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "blocked"}}})
	if bubble := m.state.bubbles[sheepBubbleKey("box")]; bubble.text != "Help!" || bubble.priority != bubbleUrgent {
		t.Fatalf("blocked bubble = %#v", bubble)
	}

	m.state.step += bubbleLifetime
	if _, exists := m.state.activeBubble(sheepBubbleKey("box")); exists {
		t.Fatal("bubble remained active after its lifetime")
	}
}

func TestAgentEntryBubbleUsesUsefulIdentity(t *testing.T) {
	tests := []struct {
		name  string
		agent herdr.Agent
		want  string
	}{
		{name: "terminal title", agent: herdr.Agent{Agent: "codex", Workspace: "project", Tab: "tests", TerminalTitleStripped: "Fixing tests"}, want: "Fixing tests"},
		{name: "workspace and tab", agent: herdr.Agent{Agent: "codex", Workspace: "project", Tab: "tests"}, want: "project / tests"},
		{name: "agent", agent: herdr.Agent{Agent: "codex"}, want: "codex"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := agentBubbleLabel(test.agent); got != test.want {
				t.Fatalf("entry label = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRoutineIdleAndWorkingTransitionsDoNotCreateBubbles(t *testing.T) {
	for _, status := range []string{"idle", "working"} {
		if text, _ := agentStateBubble(status); text != "" {
			t.Fatalf("%s transition bubble = %q", status, text)
		}
	}
}

func TestTargetFailureCreatesShepherdAndLordBubbles(t *testing.T) {
	m := newTestModel([]target.Target{{Name: "box"}})
	m.state.visible = true
	m.statuses["box"] = poll.TargetStatus{State: poll.OK}
	recordTestChange(m, "box", poll.TargetStatus{State: poll.Unreachable})

	if bubble := m.state.bubbles[shepherdBubbleKey("box")]; bubble.text != "Offline!" {
		t.Fatalf("shepherd bubble = %#v", bubble)
	}
	if bubble := m.state.bubbles[bubbleKey{kind: bubbleLord}]; bubble.text != "1 herd needs aid!" {
		t.Fatalf("lord bubble = %#v", bubble)
	}
}

func TestLordBubbleSummarizesMultiplePastures(t *testing.T) {
	m := newTestModel([]target.Target{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	m.state.visible = true
	m.statuses["a"] = poll.TargetStatus{State: poll.Unreachable}
	m.statuses["b"] = poll.TargetStatus{State: poll.OK}
	m.statuses["c"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "idle"}}}

	recordTestChange(m, "b", poll.TargetStatus{State: poll.NoHerdr})
	if bubble := m.state.bubbles[bubbleKey{kind: bubbleLord}]; bubble.text != "2 herds need aid!" || bubble.priority != bubbleUrgent {
		t.Fatalf("attention summary = %#v", bubble)
	}

	m.state.step += bubbleLifetime
	m.statuses["a"] = poll.TargetStatus{State: poll.OK}
	m.statuses["b"] = poll.TargetStatus{State: poll.OK}
	recordTestChange(m, "c", poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "working"}}})
	if bubble := m.state.bubbles[bubbleKey{kind: bubbleLord}]; bubble.text != "Directing 1 active." || bubble.priority != bubbleImportant {
		t.Fatalf("active summary = %#v", bubble)
	}
}

func recordTestChange(m *testModel, name string, next poll.TargetStatus) {
	m.state.RecordBubbles(m.snapshot(), name, fleet.DiffStatus(m.statuses[name], next, true))
}

func TestLordBubbleRendersBesideASCIILord(t *testing.T) {
	m := newTestModel(nil)
	m.state.visible = true
	m.state.emitLordBubble("To work!", bubbleNormal)
	scene := m.buildSceneForTest(80, m.layout(80))
	scene.roadCenters = []int{20, 40, 60}
	view := asciiRoyalScene(scene)
	if !strings.Contains(view, "o>") || !strings.Contains(view, "[To work!]") {
		t.Fatalf("lord or bubble is missing:\n%s", view)
	}
}

func TestASCIIPenRendersSpeechBubbleWithoutChangingSize(t *testing.T) {
	m := newTestModel([]target.Target{{Name: "box"}})
	m.state.visible = true
	agent := herdr.Agent{PaneID: "one", Status: "done", TerminalTitleStripped: "日本語"}
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{agent}}
	m.state.sheep[fleet.NewAgentKey("box", "one")] = sheep{x: 12, y: 10}
	m.state.emitSheepBubble("box", "one", agentBubbleLabel(agent), bubbleImportant)

	grid := grid{columns: 1, penWidth: 44, heights: []int{14}, offsets: []int{0}, rowHeights: []int{14}, visibleRows: 1}
	view := asciiPen(m.buildSceneForTest(44, grid).rows[0].pens[0])
	if !strings.Contains(view, "[???]") || len(strings.Split(view, "\n")) != 14 {
		t.Fatalf("ASCII bubble changed the pen or is missing:\n%s", view)
	}
	for row, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width != 44 {
			t.Fatalf("ASCII pen row %d width = %d, want 44: %q", row, width, line)
		}
	}
}

func TestPastureChatterIsRareAndUsesIdleAgent(t *testing.T) {
	m := newTestModel([]target.Target{{Name: "box"}})
	m.state.visible = true
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "one", Status: "idle"}}}
	m.state.step = 49
	m.state.maybeChatter(m.snapshot())
	if len(m.state.bubbles) != 0 {
		t.Fatal("idle chatter appeared before its interval")
	}
	m.state.step = 50
	m.state.maybeChatter(m.snapshot())
	if bubble := m.state.bubbles[sheepBubbleKey("box")]; bubble.text != "Baa." {
		t.Fatalf("idle chatter bubble = %#v", bubble)
	}
}
