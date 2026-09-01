package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
	"github.com/mjrusso/herdlord/internal/targetmgr"
)

type fakeClient struct {
	reads       int
	readLines   int
	readOutputs []string
	status      herdr.Status
	statusErr   error
	statusCalls chan string
}

func (c *fakeClient) Status(_ context.Context, configured target.Target) (herdr.Status, error) {
	if c.statusCalls != nil {
		c.statusCalls <- configured.Name
	}
	if c.status.Protocol == 0 {
		c.status = herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"}
	}
	return c.status, c.statusErr
}

func (*fakeClient) Snapshot(context.Context, target.Target, string) ([]herdr.Agent, error) {
	return nil, nil
}

func (c *fakeClient) Read(_ context.Context, _ target.Target, _ string, pane string, lines int) (string, error) {
	c.reads++
	c.readLines = lines
	if c.reads <= len(c.readOutputs) {
		return c.readOutputs[c.reads-1], nil
	}
	return "output for " + pane, nil
}

type blockingReadClient struct {
	fakeClient
}

func (*blockingReadClient) Read(ctx context.Context, _ target.Target, _, _ string, _ int) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestFocusedOutputCachedPerPaneRevision(t *testing.T) {
	client := &fakeClient{}
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: client})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, HerdrPath: "/opt/herdr", Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "codex", Status: "idle", Revision: 1},
		{PaneID: "p2", Agent: "codex", Status: "idle", Revision: 2},
	}}
	m.rebuildRows()

	runOutputCmd(t, m, m.readFocused())
	m.table.SetCursor(1)
	runOutputCmd(t, m, m.readFocused())
	m.table.SetCursor(0)
	if cmd := m.readFocused(); cmd != nil {
		t.Fatal("cached pane scheduled another read")
	}
	if client.reads != 2 || m.output != "output for p1" {
		t.Fatalf("reads = %d, output = %q", client.reads, m.output)
	}
	if client.readLines != 120 {
		t.Fatalf("read lines = %d, want 120", client.readLines)
	}

	m.statuses["box"] = poll.TargetStatus{State: poll.OK, HerdrPath: "/opt/herdr", Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "codex", Status: "working", Revision: 3},
		{PaneID: "p2", Agent: "codex", Status: "working", Revision: 2},
	}}
	m.rebuildRows()
	runOutputCmd(t, m, m.readFocused())
	if client.reads != 3 {
		t.Fatalf("revision change reads = %d", client.reads)
	}
}

func TestWorkingAgentOutputRefreshesWithoutRevisionChange(t *testing.T) {
	client := &fakeClient{readOutputs: []string{"first capture", "second capture"}}
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: client})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "working", Revision: 1}}}
	m.rebuildRows()
	runOutputCmd(t, m, m.readFocused())
	cmd := m.readFocused()
	if cmd == nil {
		t.Fatal("working agent reused stale output")
	}
	if m.output != "first capture" || m.outputLoading {
		t.Fatalf("live refresh hid existing output: output=%q loading=%v", m.output, m.outputLoading)
	}
	m.width, m.height = 60, 14
	m.openExpandedOutput()
	runOutputCmd(t, m, cmd)
	if client.reads != 2 || m.output != "second capture" || !strings.Contains(m.outputViewport.View(), "second capture") {
		t.Fatalf("live refresh = reads %d, output %q, viewport %q", client.reads, m.output, m.outputViewport.View())
	}
}

func TestOutputAndAttachErrorsAreVisible(t *testing.T) {
	m := New(nil, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.rows = []row{{target: "box", agent: &herdr.Agent{PaneID: "p1"}}}
	m.outputKey, m.output = "box\x00p1", "existing output"
	_, _ = m.Update(attachResult("box\x00p1")(errors.New("terminal busy")))
	if !strings.Contains(m.message, "attach: terminal busy") {
		t.Fatalf("message = %q", m.message)
	}
	if m.output != "existing output" {
		t.Fatalf("attach failure cleared output: %q", m.output)
	}
}

func TestAddValidateAndDeletePersistedTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "targets.json")
	m := New(nil, path, poll.Manager{Client: &fakeClient{}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m.addInputs[0].SetValue("local")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("add did not schedule validation")
	}
	_, _ = m.Update(cmd())

	got, err := target.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "local" || len(got[0].Prefix) != 0 {
		t.Fatalf("persisted targets = %#v", got)
	}

	m.beginDelete()
	_, _ = m.updateDeleteConfirmation(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got, err = target.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("targets after delete = %#v", got)
	}
}

func TestTargetManagerEditsAndPausesTargets(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	path := filepath.Join(t.TempDir(), "targets.json")
	configured := target.Target{
		Name:        "workbox",
		Prefix:      []string{"ssh", "work box", "--"},
		Interactive: []string{"ssh", "-t", "work box", "--"},
	}
	if err := target.Save(path, []target.Target{configured}); err != nil {
		t.Fatal(err)
	}
	m := New([]target.Target{configured}, path, poll.Manager{Client: &fakeClient{}})
	m.statuses["workbox"] = poll.TargetStatus{State: poll.OK}
	m.rebuildRows()

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	view := ansi.Strip(m.View())
	for _, want := range []string{"Targets", "workbox", "ssh 'work box' --", "a add", "e edit", "space pause/resume"} {
		if !strings.Contains(view, want) {
			t.Fatalf("target manager missing %q:\n%s", want, view)
		}
	}
	if raw := m.targetManagerView(); !strings.Contains(raw, "\x1b[1ma\x1b[0m \x1b[90madd") {
		t.Fatalf("target-manager shortcut styling is inconsistent:\n%q", raw)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	got, err := target.Load(path)
	if err != nil || !got[0].Paused {
		t.Fatalf("paused target = %#v, %v", got, err)
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if m.overlay.kind != overlayEdit || m.addInputs[0].Value() != "workbox" || m.addInputs[1].Value() != "ssh 'work box' --" {
		t.Fatalf("edit form state: mode=%q values=%q", m.overlay.kind, []string{m.addInputs[0].Value(), m.addInputs[1].Value(), m.addInputs[2].Value()})
	}
	m.addInputs[0].SetValue("renamed")
	m.addInputs[1].SetValue("voom ssh play --")
	m.addInputs[2].SetValue("")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("edit did not schedule validation")
	}
	_, _ = m.Update(cmd())
	got, err = target.Load(path)
	if err != nil || len(got) != 1 || got[0].Name != "renamed" || !got[0].Paused || !reflect.DeepEqual(got[0].Prefix, []string{"voom", "ssh", "play", "--"}) || got[0].Interactive != nil {
		t.Fatalf("edited target = %#v, %v", got, err)
	}
	if m.overlay.kind != overlayTargets || m.message != "Updated renamed · Herdr 0.8.0" {
		t.Fatalf("edit result: manage=%v message=%q", (m.overlay.kind == overlayTargets), m.message)
	}
}

func TestDashboardTargetActionsAreConsolidated(t *testing.T) {
	configured := target.Target{Name: "box"}
	m := New([]target.Target{configured}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK}
	m.rebuildRows()
	for _, key := range []rune{'a', 'd', ' '} {
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	}
	if m.overlay.kind != overlayNone || m.overlay.kind == overlayDelete || m.targets[0].Paused {
		t.Fatalf("dashboard exposed target action: mode=%q delete=%q target=%#v", m.overlay.kind, m.overlay.target, m.targets[0])
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if m.overlay.kind != overlayTargets {
		t.Fatal("t did not open target management")
	}
}

func TestEmptyStartupExplainsHowToContinue(t *testing.T) {
	m := New(nil, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	_ = m.Init()
	if m.overlay.kind != overlayNone {
		t.Fatalf("empty startup opened mode %q", m.overlay.kind)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"No targets configured", "Add a local or remote Herdr session", "t targets", "q quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty state missing %q:\n%s", want, view)
		}
	}
}

func TestAddFormShowsAllFieldsAndContextualControls(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	m := New(nil, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	rawView := m.View()
	view := ansi.Strip(rawView)
	for _, want := range []string{"Add target", "Name", "Command prefix", "Attach prefix", "non-interactive Herdr command", "`ssh workbox --`", "`voom ssh play --`", "transport TTY flags", "`ssh -t workbox --`", "Tab/Shift-Tab move", "Esc cancel", "Ctrl-C quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("add form missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "enter attach") {
		t.Fatalf("add form retained dashboard shortcuts:\n%s", view)
	}
	if !strings.Contains(rawView, "\x1b[1mTab/Shift-Tab\x1b[0m \x1b[90mmove") {
		t.Fatalf("add form shortcut styling is inconsistent:\n%q", rawView)
	}
	for _, block := range []string{
		"SSH example:   `ssh workbox --`\n  Voom example:  `voom ssh play --`",
		"SSH example:   `ssh -t workbox --`\n  Voom example:  `voom ssh play --`",
	} {
		if !strings.Contains(view, block) {
			t.Fatalf("examples are not on separate lines:\n%s", view)
		}
	}
	if !strings.Contains(view, "Herdr session.\n\n  Command prefix") || !strings.Contains(view, "`voom ssh play --`\n\n  Attach prefix") {
		t.Fatalf("fields are not separated:\n%s", view)
	}
	if !strings.Contains(view, "Voom example:  `voom ssh play --`\n\nTab/Shift-Tab move") {
		t.Fatalf("shortcut bar is not separated from form content:\n%s", view)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.overlay.kind != overlayAdd || m.addInputs[0].Value() != "q" {
		t.Fatalf("q did not remain form input: mode=%q value=%q", m.overlay.kind, m.addInputs[0].Value())
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.addFocus != 1 {
		t.Fatalf("Tab focus = %d, want 1", m.addFocus)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay.kind != overlayTargets {
		t.Fatalf("Esc left mode %q", m.overlay.kind)
	}
}

func TestStaleValidationResultCannotOverwriteNewerEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	configured := []target.Target{{Name: "box", Prefix: []string{"ssh", "old", "--"}}}
	if err := target.Save(path, configured); err != nil {
		t.Fatal(err)
	}
	m := New(configured, path, poll.Manager{Client: &fakeClient{}})
	m.validationGeneration = 2
	status := poll.TargetStatus{State: poll.OK, Version: "0.8.0"}
	newer := validationMsg{generation: 2, original: "box", target: target.Target{Name: "box", Prefix: []string{"ssh", "new", "--"}}, status: status}
	stale := validationMsg{generation: 1, original: "box", target: target.Target{Name: "box", Prefix: []string{"ssh", "stale", "--"}}, status: status}

	_, _ = m.Update(newer)
	_, _ = m.Update(stale)

	got, err := target.Load(path)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].Prefix, []string{"ssh", "new", "--"}) {
		t.Fatalf("targets after stale validation = %#v, %v", got, err)
	}
}

func TestModalViewsFrameDashboardAtStandardSize(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "idle"}}}
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Fatalf("add form is not framed over dashboard:\n%s", view)
	}
	if got := len(strings.Split(view, "\n")); got != 24 {
		t.Fatalf("modal height = %d, want 24", got)
	}
	for _, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width != 80 {
			t.Fatalf("modal line width = %d, want 80: %q", width, line)
		}
	}

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "╭") || !strings.Contains(view, "Toggle inspector") {
		t.Fatalf("help is not framed:\n%s", view)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "╭") || !strings.Contains(view, "Delete target") {
		t.Fatalf("confirmation is not framed:\n%s", view)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if view := ansi.Strip(m.View()); !strings.Contains(view, "╭") || !strings.Contains(view, "Recent terminal output") {
		t.Fatalf("output is not framed:\n%s", view)
	}
}

func TestModalFallsBackOnSmallTerminal(t *testing.T) {
	m := New(nil, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	_, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	view := ansi.Strip(m.View())
	if strings.Contains(view, "╭") || !strings.Contains(view, "Add target") {
		t.Fatalf("small-terminal fallback:\n%s", view)
	}
}

func TestAddPersistsUnhealthyTarget(t *testing.T) {
	tests := []struct {
		name   string
		client *fakeClient
		state  poll.State
		want   string
	}{
		{name: "unreachable", client: &fakeClient{statusErr: errors.New("offline")}, state: poll.Unreachable, want: "Added unreachable · unreachable"},
		{name: "no-herdr", client: &fakeClient{statusErr: &herdr.CommandError{Err: errors.New("exit status 127"), Stderr: "herdr: not found"}}, state: poll.NoHerdr, want: "Added no-herdr · no herdr"},
		{name: "skewed", client: &fakeClient{status: herdr.Status{Protocol: 18, Version: "0.7.0", Path: "/opt/herdr"}}, state: poll.Skewed, want: "Added skewed · skewed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "targets.json")
			manager := poll.Manager{Client: tt.client}
			configured := target.Target{Name: tt.name}
			status, err := (targetmgr.Manager{Poller: manager}).Check(context.Background(), configured)
			if err != nil {
				t.Fatal(err)
			}
			m := New(nil, path, manager)
			_, _ = m.Update(validationMsg{target: configured, status: status})

			got, err := target.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Name != tt.name {
				t.Fatalf("persisted targets = %#v", got)
			}
			if m.statuses[tt.name].State != tt.state {
				t.Fatalf("status = %#v", m.statuses[tt.name])
			}
			if m.message != tt.want {
				t.Fatalf("message = %q, want %q", m.message, tt.want)
			}
		})
	}
}

func TestValidationResultsAppendToCurrentTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	m := New(nil, path, poll.Manager{Client: &fakeClient{}})
	status := poll.TargetStatus{State: poll.OK, Version: "0.8.0"}
	_, _ = m.Update(validationMsg{target: target.Target{Name: "one"}, status: status})
	_, _ = m.Update(validationMsg{target: target.Target{Name: "two"}, status: status})
	got, err := target.Load(path)
	if err != nil || len(got) != 2 || got[0].Name != "one" || got[1].Name != "two" {
		t.Fatalf("persisted targets = %#v, %v", got, err)
	}
	if m.generations["one"] != 1 || m.generations["two"] != 1 {
		t.Fatalf("poller generations = %#v", m.generations)
	}
}

func TestExternalConfigChangesAreReconciled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	initial := []target.Target{{Name: "one", Prefix: []string{"ssh", "old", "--"}}, {Name: "removed"}}
	if err := target.Save(path, initial); err != nil {
		t.Fatal(err)
	}
	m := New(initial, path, poll.Manager{Client: &fakeClient{}})
	m.statuses["one"] = poll.TargetStatus{State: poll.OK}
	m.statuses["removed"] = poll.TargetStatus{State: poll.OK}
	m.rebuildRows()
	m.start(initial[0])
	oldGeneration := m.generations["one"]

	latest := []target.Target{{Name: "one", Prefix: []string{"ssh", "new", "--"}, Paused: true}, {Name: "added"}}
	_, _ = m.Update(configMsg{targets: latest})
	if !reflect.DeepEqual(m.targets, latest) {
		t.Fatalf("targets = %#v", m.targets)
	}
	if _, exists := m.statuses["removed"]; exists {
		t.Fatalf("removed target status remains: %#v", m.statuses)
	}
	if m.statuses["one"].State != poll.Paused {
		t.Fatalf("updated target status = %#v", m.statuses["one"])
	}
	if m.generations["one"] <= oldGeneration {
		t.Fatalf("updated target generation = %d, old %d", m.generations["one"], oldGeneration)
	}
	if m.targetIndex("added") < 0 {
		t.Fatal("added target is missing")
	}

	before := append([]target.Target(nil), m.targets...)
	_, _ = m.Update(configMsg{err: errors.New("malformed targets file")})
	if !reflect.DeepEqual(m.targets, before) || !strings.Contains(m.message, "malformed targets file") {
		t.Fatalf("malformed reload changed state: %#v, %q", m.targets, m.message)
	}
}

func TestConfigWatcherLoadsExternalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	m := New(nil, path, poll.Manager{Client: &fakeClient{}})
	want := []target.Target{{Name: "external"}}
	if err := target.Save(path, want); err != nil {
		t.Fatal(err)
	}
	msg, ok := m.watchConfig()().(configMsg)
	if !ok || msg.err != nil || !reflect.DeepEqual(msg.targets, want) {
		t.Fatalf("watch result = %#v", msg)
	}
}

func TestRunningTUIReloadsChangesFromCLIProcess(t *testing.T) {
	temp := t.TempDir()
	configPath := filepath.Join(temp, "targets.json")
	herdrPath := filepath.Join(temp, "herdr")
	initial := []target.Target{{Name: "existing", Paused: true}}
	if err := target.Save(configPath, initial); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(herdrPath, []byte("#!/bin/sh\nprintf 'server:\\n  status: running\\n  version: 0.8.0\\n  protocol: 19\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	binary := filepath.Join(temp, "herdlord-bin")
	build := exec.Command("go", "build", "-o", binary, "./cmd/herdlord")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build herdlord: %v\n%s", err, out)
	}

	statusCalls := make(chan string, 4)
	m := New(initial, configPath, poll.Manager{Client: &fakeClient{statusCalls: statusCalls}})
	program := tea.NewProgram(m, tea.WithInput(nil), tea.WithoutRenderer())
	m.SetProgram(program)
	done := make(chan tea.Model, 1)
	errCh := make(chan error, 1)
	go func() {
		final, err := program.Run()
		if err != nil {
			errCh <- err
			return
		}
		done <- final
	}()
	t.Cleanup(program.Kill)

	runCLI := func(args ...string) {
		t.Helper()
		commandArgs := append([]string{"--config", configPath}, args...)
		cmd := exec.Command(binary, commandArgs...)
		cmd.Env = append(os.Environ(), "PATH="+temp+string(os.PathListSeparator)+os.Getenv("PATH"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("herdlord %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	runCLI("targets", "add", "external")
	select {
	case name := <-statusCalls:
		if name != "external" {
			t.Fatalf("TUI polled %q", name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TUI did not reload the CLI change")
	}
	program.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	select {
	case err := <-errCh:
		t.Fatal(err)
	case final := <-done:
		got := final.(*Model)
		if len(got.targets) != 2 || got.targets[1].Name != "external" || got.targets[1].Paused {
			t.Fatalf("TUI targets = %#v", got.targets)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TUI did not stop")
	}
}

func TestTUIMutationPreservesExternalChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	initial := []target.Target{{Name: "one"}}
	if err := target.Save(path, initial); err != nil {
		t.Fatal(err)
	}
	m := New(initial, path, poll.Manager{Client: &fakeClient{}})
	m.statuses["one"] = poll.TargetStatus{State: poll.OK}
	m.rebuildRows()
	if _, err := target.Mutate(path, func(current []target.Target) ([]target.Target, error) {
		current[0].Paused = true
		return append(current, target.Target{Name: "external"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	m.toggleFocused()
	got, err := target.Load(path)
	if err != nil || len(got) != 2 || got[0].Paused || got[1].Name != "external" {
		t.Fatalf("targets after TUI mutation = %#v, %v", got, err)
	}
	if m.targetIndex("external") < 0 {
		t.Fatal("TUI did not reconcile external target after mutation")
	}
}

func TestRemovingFocusedTargetClearsItsOutput(t *testing.T) {
	targets := []target.Target{{Name: "one"}, {Name: "two"}}
	m := New(targets, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["one"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "working"}}}
	m.statuses["two"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p2", Status: "working"}}}
	m.rebuildRows()
	m.outputKey, m.output = "one\x00p1", "output from removed target"
	m.outputs[m.outputKey] = cachedOutput{revision: 1, text: m.output}
	m.reconcile([]target.Target{{Name: "two"}})
	if m.output != "" || m.outputKey != "" {
		t.Fatalf("output after removal = %q, key %q", m.output, m.outputKey)
	}
}

func TestStalePollResultIsIgnoredAfterTargetRestart(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.start(target.Target{Name: "box"})
	staleGeneration := m.generations["box"]
	m.reconcile([]target.Target{{Name: "box", Prefix: []string{"ssh", "new", "--"}}})
	_, _ = m.Update(pollMsg{generation: staleGeneration, result: poll.Result{Name: "box", Status: poll.TargetStatus{State: poll.Unreachable}}})
	if m.statuses["box"].State == poll.Unreachable {
		t.Fatal("stale poll result replaced restarted target state")
	}
}

func TestPauseResumeAndRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "targets.json")
	targets := []target.Target{{Name: "box"}}
	if err := target.Save(path, targets); err != nil {
		t.Fatal(err)
	}
	m := New(targets, path, poll.Manager{Client: &fakeClient{}})
	lastSuccess := time.Now().Add(-time.Minute)
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, LastSuccess: lastSuccess}
	m.rebuildRows()
	m.toggleFocused()
	got, err := target.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Paused || m.statuses["box"].State != poll.Paused {
		t.Fatalf("paused target = %#v, status = %#v", got[0], m.statuses["box"])
	}
	if m.message != "Paused box" || m.messageKind != noticeSuccess {
		t.Fatalf("pause notice = %q, kind %v", m.message, m.messageKind)
	}
	if !m.statuses["box"].LastSuccess.Equal(lastSuccess) {
		t.Fatalf("pause lost last success: %#v", m.statuses["box"])
	}
	m.toggleFocused()
	got, err = target.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Paused {
		t.Fatalf("resumed target = %#v", got[0])
	}
	if m.message != "Resumed box" || m.messageKind != noticeSuccess {
		t.Fatalf("resume notice = %q, kind %v", m.message, m.messageKind)
	}
	if !m.statuses["box"].LastSuccess.Equal(lastSuccess) || m.statuses["box"].State != poll.Checking {
		t.Fatalf("resume status = %#v", m.statuses["box"])
	}

	refresh := make(chan struct{}, 1)
	m.refresh["box"] = refresh
	m.refreshAll()
	if m.message != "Refreshing 0 of 1 targets…" {
		t.Fatalf("refresh notice = %q", m.message)
	}
	select {
	case <-refresh:
	default:
		t.Fatal("refresh did not signal active target")
	}
}

func TestRefreshTracksAllTargetsAndPreservesErrors(t *testing.T) {
	m := New([]target.Target{{Name: "one"}, {Name: "two"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.refresh["one"] = make(chan struct{}, 1)
	m.refresh["two"] = make(chan struct{}, 1)
	m.refreshAll()
	m.updateRefreshProgress("one")
	if m.message != "Refreshing 1 of 2 targets…" {
		t.Fatalf("partial refresh notice = %q", m.message)
	}
	m.setNotice(noticeError, "Could not read recent output: offline")
	m.updateRefreshProgress("two")
	if m.messageKind != noticeError || !strings.Contains(m.message, "offline") {
		t.Fatalf("refresh erased error: %q, kind %v", m.message, m.messageKind)
	}
}

func TestEmptyRecentOutputIsExplicit(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Revision: 1}}}
	m.rebuildRows()
	key := m.focusKey()
	m.width, m.height = 80, 24
	m.outputKey, m.output = key, ""
	m.showInspector = true
	m.updateTableHeight()
	emptyHeight := m.table.Height()
	m.output = "one line"
	m.updateTableHeight()
	if m.table.Height() != emptyHeight {
		t.Fatalf("empty output table height = %d, populated output height = %d", emptyHeight, m.table.Height())
	}
	m.output = ""
	if view := m.View(); !strings.Contains(view, "Recent output") || !strings.Contains(view, "No recent output") {
		t.Fatalf("empty output state:\n%s", view)
	}
}

func TestExpandedOutputScrollsAndCloses(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Workspace: "project", Agent: "codex", Revision: 1}}}
	m.rebuildRows()
	m.width, m.height = 52, 14
	m.outputKey = m.focusKey()
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("useful output line %02d", i+1)
	}
	m.output = strings.Join(lines, "\n")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if m.overlay.kind != overlayOutput || !m.outputViewport.AtBottom() {
		t.Fatalf("expanded output did not open at bottom: expanded=%v offset=%d", (m.overlay.kind == overlayOutput), m.outputViewport.YOffset)
	}
	view := m.View()
	plainView := ansi.Strip(view)
	for _, want := range []string{"Recent terminal output", "box / project / codex", "PgUp/PgDn page", "o/Esc close"} {
		if !strings.Contains(plainView, want) {
			t.Fatalf("expanded output missing %q:\n%s", want, plainView)
		}
	}
	if !strings.Contains(view, "\x1b[1m↑/↓\x1b[0m \x1b[90mscroll") {
		t.Fatalf("expanded-output shortcut styling is inconsistent: %q", view)
	}
	bottom := m.outputViewport.YOffset
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.outputViewport.YOffset >= bottom {
		t.Fatalf("output did not scroll up: before=%d after=%d", bottom, m.outputViewport.YOffset)
	}
	m.outputViewport.GotoBottom()
	_, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	if !m.outputViewport.AtBottom() {
		t.Fatalf("resize lost bottom position: offset=%d", m.outputViewport.YOffset)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay.kind == overlayOutput {
		t.Fatal("Esc did not close expanded output")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.overlay.kind == overlayOutput {
		t.Fatal("q did not close expanded output")
	}
}

func TestOutputDoesNotOpenForTargetStatusRow(t *testing.T) {
	m := New([]target.Target{{Name: "offline"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["offline"] = poll.TargetStatus{State: poll.Unreachable}
	m.rebuildRows()
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if m.overlay.kind == overlayOutput {
		t.Fatal("output opened for a target status row")
	}
}

func TestSelectionAndTargetRowsHaveTextMarkers(t *testing.T) {
	m := New([]target.Target{{Name: "agents"}, {Name: "offline"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["agents"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "working"}}}
	m.statuses["offline"] = poll.TargetStatus{State: poll.Unreachable}
	m.rebuildRows()
	view := ansi.Strip(m.table.View())
	if !strings.Contains(view, "> *") || !strings.Contains(view, "◇") {
		t.Fatalf("row markers missing:\n%s", view)
	}
}

func TestCursorDoesNotMoveAttentionMarker(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "blocked", Agent: "codex", Status: "blocked"},
		{PaneID: "done", Agent: "codex", Status: "done"},
	}}
	m.rebuildRows()
	if got := m.table.Rows()[1][0]; got != "  ●" {
		t.Fatalf("unselected marker = %q", got)
	}
	m.table.SetCursor(1)
	m.updateSelectionMarkers()
	if got := m.table.Rows()[1][0]; got != "> ●" {
		t.Fatalf("selected marker = %q", got)
	}
}

func TestAttentionStateIsExplicitAndReadOnly(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "blocked", Agent: "codex", Status: "blocked"},
		{PaneID: "done", Agent: "codex", Status: "done"},
		{PaneID: "idle", Agent: "codex", Status: "idle"},
	}}
	m.rebuildRows()
	rows := m.table.Rows()
	if rows[0][0] != "> !" || rows[0][len(rows[0])-1] != "needs input" {
		t.Fatalf("blocked row = %#v", rows[0])
	}
	if rows[1][0] != "  ●" || rows[1][len(rows[1])-1] != "done" {
		t.Fatalf("done row = %#v", rows[1])
	}
	if rows[2][0] != "  " || rows[2][len(rows[2])-1] != "idle" {
		t.Fatalf("idle row = %#v", rows[2])
	}
	if got := m.rows[0].agent.Status; got != "blocked" {
		t.Fatalf("display mapping changed authoritative status to %q", got)
	}
}

func TestStaleOutputDoesNotReplaceNewerRevision(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "working", Revision: 5}}}
	m.rebuildRows()
	key := m.focusKey()
	m.outputLoading, m.loadingKey, m.loadingRev = true, key, 5
	_, _ = m.Update(outputMsg{key: key, revision: 5, text: "new"})
	_, _ = m.Update(outputMsg{key: key, revision: 4, text: "old"})
	if m.output != "new" || m.outputs[key].revision != 5 {
		t.Fatalf("output = %q, cache = %#v", m.output, m.outputs[key])
	}
}

func TestOutOfOrderOutputDoesNotCompleteNewerRead(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "working", Revision: 1}}}
	m.rebuildRows()
	key := m.focusKey()
	if m.readFocused() == nil {
		t.Fatal("first revision did not schedule a read")
	}
	m.statuses["box"].Agents[0].Revision = 2
	m.rebuildRows()
	if m.readFocused() == nil {
		t.Fatal("new revision did not schedule a read")
	}
	_, _ = m.Update(outputMsg{key: key, revision: 1, text: "old"})
	if m.output != "" || !m.outputLoading || m.loadingRev != 2 {
		t.Fatalf("old completion changed current load: output %q, loading %v, revision %d", m.output, m.outputLoading, m.loadingRev)
	}
	_, _ = m.Update(outputMsg{key: key, revision: 2, text: "new"})
	if m.output != "new" || m.outputLoading {
		t.Fatalf("new completion = output %q, loading %v", m.output, m.outputLoading)
	}
}

func TestRepeatedPollDoesNotDuplicateFocusedRead(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Agent: "codex", Status: "working", Revision: 1}}}
	m.rebuildRows()
	if m.readFocused() == nil {
		t.Fatal("first read was not scheduled")
	}
	if m.readFocused() != nil {
		t.Fatal("duplicate read was scheduled for the same revision")
	}
}

func TestReturningToInflightPaneDoesNotDuplicateRead(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "one", Status: "working", Revision: 1},
		{PaneID: "p2", Agent: "two", Status: "idle", Revision: 1},
	}}
	m.rebuildRows()
	if m.readFocused() == nil {
		t.Fatal("first pane did not schedule a read")
	}
	m.table.SetCursor(1)
	if m.readFocused() == nil {
		t.Fatal("second pane did not schedule a read")
	}
	m.table.SetCursor(0)
	if m.readFocused() != nil || !m.outputLoading || m.loadingKey != "box\x00p1" {
		t.Fatalf("returning to in-flight pane scheduled a duplicate: loading %v, key %q", m.outputLoading, m.loadingKey)
	}
}

func TestStaleOutputErrorIsNotShown(t *testing.T) {
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{
		{PaneID: "p1", Agent: "one", Status: "working", Revision: 1},
		{PaneID: "p2", Agent: "two", Status: "idle", Revision: 1},
	}}
	m.rebuildRows()
	_ = m.readFocused()
	m.table.SetCursor(1)
	_ = m.readFocused()
	_, _ = m.Update(outputMsg{key: "box\x00p1", target: "box", revision: 1, err: errors.New("timed out")})
	if m.message != "" || !m.outputLoading || m.loadingKey != "box\x00p2" {
		t.Fatalf("stale error changed UI: message %q, loading %v, key %q", m.message, m.outputLoading, m.loadingKey)
	}
}

func TestTargetChangeInvalidatesOutputAndRejectsInflightRead(t *testing.T) {
	m := New([]target.Target{{Name: "box", Prefix: []string{"ssh", "old", "--"}}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: &fakeClient{}})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "working", Revision: 1}}}
	m.rebuildRows()
	m.start(m.targets[0])
	oldGeneration := m.generations["box"]
	key := m.focusKey()
	m.outputs[key] = cachedOutput{revision: 1, text: "old machine"}

	m.reconcile([]target.Target{{Name: "box", Prefix: []string{"ssh", "new", "--"}}})
	if _, ok := m.outputs[key]; ok {
		t.Fatalf("output cache retained changed target: %#v", m.outputs)
	}
	_, _ = m.Update(outputMsg{key: key, target: "box", generation: oldGeneration, revision: 1, text: "late old machine"})
	if _, ok := m.outputs[key]; ok || m.output != "" {
		t.Fatalf("stale output accepted: cache %#v, output %q", m.outputs, m.output)
	}
}

func TestFocusedReadUsesConfiguredTimeout(t *testing.T) {
	client := &blockingReadClient{}
	m := New([]target.Target{{Name: "box"}}, filepath.Join(t.TempDir(), "targets.json"), poll.Manager{Client: client, Timeout: 20 * time.Millisecond})
	m.statuses["box"] = poll.TargetStatus{State: poll.OK, Agents: []herdr.Agent{{PaneID: "p1", Status: "working", Revision: 1}}}
	m.rebuildRows()
	started := time.Now()
	msg := m.readFocused()().(outputMsg)
	if !errors.Is(msg.err, context.DeadlineExceeded) || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("focused read error = %v after %v", msg.err, time.Since(started))
	}
}
