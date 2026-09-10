package pasture

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

func TestDetectRendererOverride(t *testing.T) {
	t.Setenv("HERDLORD_RENDERER", "ascii")
	if got := DetectRenderer(); got != ASCII {
		t.Fatal("ASCII renderer override was ignored")
	}
	t.Setenv("HERDLORD_RENDERER", "kitty")
	if got := DetectRenderer(); got != Kitty {
		t.Fatal("Kitty renderer override was ignored")
	}
}

func TestRendererTogglePreservesPastureState(t *testing.T) {
	agent := herdr.Agent{PaneID: "p1", Status: "idle"}
	m := physicsModel(t, []herdr.Agent{agent})
	key := fleet.NewAgentKey("local", agent.PaneID)
	before := m.state.sheep[key]
	if err := m.toggleRenderer(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.state.renderer.(asciiRenderer); !ok {
		t.Fatal("renderer toggle did not select ASCII")
	}
	if m.state.sheep[key] != before {
		t.Fatal("renderer toggle reset shared sheep state")
	}
	if !strings.Contains(m.state.control, fmt.Sprintf("a=d,d=I,i=%d", kittySheepImageID)) {
		t.Fatal("renderer toggle did not delete Kitty resources")
	}
}

func TestRendererToggleUploadsKittyResources(t *testing.T) {
	t.Setenv("HERDLORD_KITTY", "1")
	m := newTestModelWithRenderer([]target.Target{{Name: "local"}}, ASCII)
	if err := m.toggleRenderer(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.state.renderer.(*kittyRenderer); !ok {
		t.Fatal("renderer toggle did not select Kitty")
	}
	if !strings.Contains(m.state.control, "\x1b_Ga=t") {
		t.Fatal("renderer toggle did not upload Kitty resources")
	}
}

func TestRendererToggleRejectsUnavailableKitty(t *testing.T) {
	t.Setenv("HERDLORD_RENDERER", "")
	t.Setenv("HERDLORD_KITTY", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("TERM", "xterm-256color")
	m := newTestModelWithRenderer([]target.Target{{Name: "local"}}, ASCII)
	err := m.toggleRenderer()
	if _, ok := m.state.renderer.(asciiRenderer); !ok {
		t.Fatal("unavailable Kitty renderer was selected")
	}
	if !errors.Is(err, ErrKittyUnavailable) {
		t.Fatalf("renderer toggle error = %v, want %v", err, ErrKittyUnavailable)
	}
}

func TestASCIIPastureUsesSharedScene(t *testing.T) {
	m := newTestModelWithRenderer([]target.Target{{Name: "local"}}, ASCII)
	m.statuses["local"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "idle", Status: "idle"},
		{PaneID: "working", Status: "working"},
		{PaneID: "blocked", Status: "blocked"},
		{PaneID: "done", Status: "done"},
	}}
	m.width, m.height = 80, MinimumViewportHeight
	m.syncState(contentWidth(m.width))
	view := testView(m)
	for _, want := range []string{"_|_|_", "+- local -+", "#"} {
		if !strings.Contains(view, want) {
			t.Fatalf("ASCII pasture missing %q", want)
		}
	}
	if strings.Contains(view, "\x1b_G") {
		t.Fatal("ASCII pasture emitted Kitty graphics commands")
	}
	if width := lipgloss.Width(view); width != contentWidth(m.width) {
		t.Fatalf("ASCII pasture width = %d, want %d", width, contentWidth(m.width))
	}
	if height := lipgloss.Height(view); height != m.height {
		t.Fatalf("ASCII pasture height = %d, want %d", height, m.height)
	}
}

func TestASCIIAnimationFramesAreDistinct(t *testing.T) {
	working := herdr.Agent{Status: "working"}
	blocked := herdr.Agent{Status: "blocked"}
	if strings.Join(asciiSheepPose(sheepPose(working, sheep{}, 0), false), "\n") == strings.Join(asciiSheepPose(sheepPose(working, sheep{}, 2), false), "\n") {
		t.Fatal("working ASCII animation frames are identical")
	}
	if strings.Join(asciiSheepPose(sheepPose(blocked, sheep{}, 0), false), "\n") == strings.Join(asciiSheepPose(sheepPose(blocked, sheep{}, 2), false), "\n") {
		t.Fatal("blocked ASCII animation frames are identical")
	}
}

func TestASCIIAnimationFramesKeepTheirGeometry(t *testing.T) {
	for _, status := range []string{"idle", "working", "blocked", "done"} {
		agent := herdr.Agent{Status: status}
		sheep := sheep{ambientSteps: 1}
		first := asciiSheepPose(sheepPose(agent, sheep, 0), false)
		second := asciiSheepPose(sheepPose(agent, sheep, 2), false)
		if len(first) != len(second) {
			t.Fatalf("%s frame heights differ: %d and %d", status, len(first), len(second))
		}
		for row := range first {
			if len(first[row]) != len(second[row]) {
				t.Fatalf("%s row %d widths differ: %q and %q", status, row, first[row], second[row])
			}
		}
	}
}

func TestASCIIUsesSharedRoleStateMapping(t *testing.T) {
	working := strings.Join(asciiShepherdPose(shepherdPose(poll.TargetStatus{State: poll.Checking})), "\n")
	base := strings.Join(asciiShepherdPose(shepherdPose(poll.TargetStatus{State: poll.Paused})), "\n")
	alert := strings.Join(asciiShepherdPose(shepherdPose(poll.TargetStatus{State: poll.Unreachable})), "\n")
	if !strings.Contains(working, "/]") {
		t.Fatalf("checking target did not use working shepherd:\n%s", working)
	}
	if strings.Contains(base, "!") || strings.Contains(base, "/]") {
		t.Fatalf("paused target did not use base shepherd:\n%s", base)
	}
	if !strings.Contains(alert, "!") {
		t.Fatalf("unreachable target did not use alert shepherd:\n%s", alert)
	}
}

func TestASCIIUsesSharedTransitionPrecedence(t *testing.T) {
	agent := herdr.Agent{PaneID: "worker", Status: "working"}
	walkingSheep := sheep{phase: relocating, dx: 1}
	walking := strings.Join(asciiSheepPose(sheepPose(agent, walkingSheep, 2), false), "\n")
	if strings.Contains(walking, ">>") || !strings.Contains(walking, "(oo)") {
		t.Fatalf("relocating worker did not use walking form:\n%s", walking)
	}

	stages := make(map[string]bool)
	for _, step := range []int{0, 2, 4} {
		dying := sheep{phase: dying, phaseStep: step}
		stages[strings.Join(asciiSheepPose(sheepPose(agent, dying, 0), false), "\n")] = true
	}
	if len(stages) != 3 {
		t.Fatalf("death sequence has %d distinct ASCII stages, want 3", len(stages))
	}
}

func TestASCIIRoyalRoadDoesNotCoverLord(t *testing.T) {
	m := newTestModel(nil)
	scene := m.buildSceneForTest(72, m.layout(72))
	scene.roadCenters = []int{12, 36, 60}
	view := asciiRoyalScene(scene)
	line := strings.Split(view, "\n")[4]
	if !strings.Contains(line, "/ \\") {
		t.Fatalf("road covered the lord's feet:\n%s", view)
	}
}

func TestWorkUsesDedicatedAnimationPoses(t *testing.T) {
	working := herdr.Agent{PaneID: "working", Status: "working"}
	blocked := herdr.Agent{PaneID: "blocked", Status: "blocked"}
	position := sheep{phase: present}
	for step := uint64(0); step < 8; step++ {
		want := []pose{poseSheepWorking, poseSheepWorkingAlt}[(step/2)%2]
		if got := sheepPose(working, position, step); got != want {
			t.Fatalf("working pose at step %d = %d, want %d", step, got, want)
		}
		wantLoom := []pose{poseLoomWorking, poseLoomWorkingAlt}[(step/2)%2]
		if got, visible := loomPose(working, position, step); !visible || got != wantLoom {
			t.Fatalf("loom pose at step %d = %d, want %d", step, got, wantLoom)
		}
		wantBlocked := []pose{poseSheepBlocked, poseSheepStomp}[(step/2)%2]
		if got := sheepPose(blocked, position, step); got != wantBlocked {
			t.Fatalf("blocked pose at step %d = %d, want %d", step, got, wantBlocked)
		}
		if got, visible := loomPose(blocked, position, step); !visible || got != poseLoomJammed {
			t.Fatalf("jammed loom pose at step %d = %d", step, got)
		}
	}
}

func TestKittyAnimationFramesHoldForTwoTicks(t *testing.T) {
	working := herdr.Agent{PaneID: "working", Status: "working"}
	position := sheep{phase: present}
	want := []pose{poseSheepWorking, poseSheepWorking, poseSheepWorkingAlt, poseSheepWorkingAlt}
	for step, pose := range want {
		if got := sheepPose(working, position, uint64(step)); got != pose {
			t.Fatalf("working pose at step %d = %d, want %d", step, got, pose)
		}
	}
}

func TestDyingWorkingAgentHasNoLoom(t *testing.T) {
	agent := herdr.Agent{PaneID: "removed", Status: "working"}
	if pose, visible := loomPose(agent, sheep{phase: dying}, 0); visible {
		t.Fatalf("dying working agent retained loom pose %d", pose)
	}
}

func TestKittyBlockedAnimationUsesTextJamIndicator(t *testing.T) {
	agent := herdr.Agent{PaneID: "blocked", Status: "blocked"}
	m := physicsModel(t, []herdr.Agent{agent})
	m.state.step = 0
	first := ansi.Strip(testView(m))
	m.state.step = 2
	second := ansi.Strip(testView(m))
	if !strings.Contains(first, "!!") || !strings.Contains(second, "XX") {
		t.Fatalf("jam indicator did not animate: first=%q second=%q", first, second)
	}
}

func TestKittyWorkAnimationDoesNotUseRedundantTextShuttle(t *testing.T) {
	agent := herdr.Agent{PaneID: "worker", Status: "working"}
	m := physicsModel(t, []herdr.Agent{agent})
	m.state.step = 0
	first := ansi.Strip(testView(m))
	m.state.step = 2
	second := ansi.Strip(testView(m))
	if strings.Contains(first, ">>") || strings.Contains(first, "<<") || strings.Contains(second, ">>") || strings.Contains(second, "<<") {
		t.Fatalf("work animation contains a text shuttle: first=%q second=%q", first, second)
	}
}

func TestASCIISheepStatesHaveDistinctShapes(t *testing.T) {
	for _, test := range []struct {
		status string
		want   string
	}{{"idle", "(oo)"}, {"working", "(oo)"}, {"blocked", "!(xx)!"}, {"done", "(--)z"}} {
		agent := herdr.Agent{Status: test.status}
		view := strings.Join(asciiSheepPose(sheepPose(agent, sheep{}, 1), false), "\n")
		if !strings.Contains(view, test.want) {
			t.Fatalf("%s ASCII sheep missing %q:\n%s", test.status, test.want, view)
		}
	}
}

func TestASCIISignSanitizesNonASCIIName(t *testing.T) {
	cells := asciiCanvas(24, top+1, ' ')
	asciiSign(cells, "café\n", 24)
	if got := asciiLines(cells); !strings.Contains(got, "+- caf?? -+") {
		t.Fatalf("ASCII sign did not sanitize target name:\n%s", got)
	}

	for name, draw := range map[string]func([][]rune){
		"above actor":  func(cells [][]rune) { drawASCIIBubble(cells, "日本語", 4, 0, 24, 2) },
		"beside actor": func(cells [][]rune) { drawASCIISideBubble(cells, "日本語", 2, 0, 3, 1, 24, 2) },
	} {
		t.Run(name, func(t *testing.T) {
			cells := asciiCanvas(24, 2, '.')
			draw(cells)
			view := asciiLines(cells)
			for row, line := range strings.Split(view, "\n") {
				if width := ansi.StringWidth(line); width != 24 {
					t.Fatalf("ASCII bubble row %d width = %d, want 24: %q", row, width, line)
				}
			}
			if !strings.Contains(view, "[???]") {
				t.Fatalf("ASCII bubble did not sanitize text: %q", view)
			}
		})
	}
}

func TestASCIISheepStayInsideFence(t *testing.T) {
	const height = 10
	row := asciiSheepRow(1_000, height)
	if row+len(asciiSheepPose(poseSheepRestLeft, false)) >= height {
		t.Fatalf("ASCII sheep at row %d crosses bottom fence at %d", row, height-1)
	}
}
