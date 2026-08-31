package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/skill"
	"github.com/mjrusso/herdlord/internal/target"
)

type fakeClient struct {
	mu            sync.Mutex
	statuses      map[string]herdr.Status
	statusErrors  map[string]error
	agents        map[string][]herdr.Agent
	reads         map[string]string
	snapshotCalls int
	blockStatus   bool
	statusStarted chan struct{}
}

func (c *fakeClient) Status(ctx context.Context, configured target.Target) (herdr.Status, error) {
	if c.statusStarted != nil {
		select {
		case c.statusStarted <- struct{}{}:
		default:
		}
	}
	if c.blockStatus {
		<-ctx.Done()
		return herdr.Status{}, ctx.Err()
	}
	if err := c.statusErrors[configured.Name]; err != nil {
		return herdr.Status{}, err
	}
	if status, ok := c.statuses[configured.Name]; ok {
		return status, nil
	}
	return herdr.Status{Protocol: herdr.Protocol, Version: "0.8.0", Path: "/opt/herdr"}, nil
}

func (c *fakeClient) Snapshot(_ context.Context, configured target.Target, _ string) ([]herdr.Agent, error) {
	c.mu.Lock()
	c.snapshotCalls++
	c.mu.Unlock()
	return c.agents[configured.Name], nil
}

func (c *fakeClient) Read(_ context.Context, configured target.Target, _ string, pane string, _ int) (string, error) {
	return c.reads[configured.Name+"/"+pane], nil
}

func TestListTextAndJSON(t *testing.T) {
	client := &fakeClient{agents: map[string][]herdr.Agent{
		"one": {{WorkspaceID: "w1", Workspace: "herdlord", TabID: "w1:t1", Tab: "dashboard", PaneID: "p1", Agent: "codex", Status: "working", TerminalTitle: "◑ Implementing", TerminalTitleStripped: "Implementing"}},
		"two": {{PaneID: "p2", Agent: "claude", Status: "blocked", TerminalTitle: "Waiting"}},
	}}
	path := saveTargets(t, []target.Target{{Name: "one"}, {Name: "two"}, {Name: "paused", Paused: true}})
	out := runCommand(t, client, "--config", path, "list")
	if !strings.Contains(out, "two") || !strings.Contains(out, "herdlord") || !strings.Contains(out, "dashboard") || !strings.Contains(out, "Implementing") || strings.Contains(out, "◑") || strings.Index(out, "blocked") > strings.Index(out, "working") || strings.Contains(out, "paused") {
		t.Fatalf("list output:\n%s", out)
	}
	out = runCommand(t, client, "--config", path, "--output", "json", "list", "--target", "one")
	var envelope struct {
		Targets []targetResult `json:"targets"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Targets) != 1 || envelope.Targets[0].Name != "one" || len(envelope.Targets[0].Agents) != 1 || envelope.Targets[0].Agents[0].WorkspaceID != "w1" || envelope.Targets[0].Agents[0].Workspace != "herdlord" || envelope.Targets[0].Agents[0].TabID != "w1:t1" || envelope.Targets[0].Agents[0].Tab != "dashboard" {
		t.Fatalf("list JSON = %#v", envelope)
	}
	alias := runCommand(t, client, "--config", path, "--format", "json", "list", "--target", "one")
	var aliasEnvelope struct {
		Targets []targetResult `json:"targets"`
	}
	if err := json.Unmarshal([]byte(alias), &aliasEnvelope); err != nil || len(aliasEnvelope.Targets) != 1 || aliasEnvelope.Targets[0].Name != "one" {
		t.Fatalf("--format json output = %q, %v", alias, err)
	}
}

func TestListAttemptsNewerProtocolAndShowsWarning(t *testing.T) {
	client := &fakeClient{
		statuses: map[string]herdr.Status{"future": {Protocol: 21, Version: "0.9.0", Path: "/opt/herdr"}},
		agents:   map[string][]herdr.Agent{"future": {{Workspace: "project", PaneID: "p1", Agent: "codex", Status: "working"}}},
	}
	path := saveTargets(t, []target.Target{{Name: "future"}})
	out := runCommand(t, client, "--config", path, "list")
	if !strings.Contains(out, "codex") || !strings.Contains(out, "newer") || !strings.Contains(out, "attempting compatibility") {
		t.Fatalf("list output:\n%s", out)
	}
	jsonOutput := runCommand(t, client, "--config", path, "--output", "json", "list")
	if !strings.Contains(jsonOutput, `"state":"newer"`) || !strings.Contains(jsonOutput, `"protocol":21`) {
		t.Fatalf("list JSON = %s", jsonOutput)
	}
}

func TestHumanOutputSanitizesControlSequencesAndJSONPreservesRawData(t *testing.T) {
	const raw = "\x1b[31magent\x1b[0m\tspoof\nnext"
	client := &fakeClient{agents: map[string][]herdr.Agent{
		"one": {{Workspace: raw, Agent: raw, Status: "idle", TerminalTitle: raw}},
	}}
	path := saveTargets(t, []target.Target{{Name: "one"}})
	textOutput := runCommand(t, client, "--config", path, "list")
	if strings.Contains(textOutput, "\x1b") || strings.Contains(textOutput, "\tspoof") || strings.Contains(textOutput, "\nnext") {
		t.Fatalf("text output contains unsafe controls: %q", textOutput)
	}
	jsonOutput := runCommand(t, client, "--config", path, "--output", "json", "list")
	if !strings.Contains(jsonOutput, `\u001b[31magent`) || !strings.Contains(jsonOutput, `\tspoof\nnext`) {
		t.Fatalf("JSON output did not preserve raw data: %q", jsonOutput)
	}
}

func TestStatusDoesNotFetchSnapshots(t *testing.T) {
	client := &fakeClient{statuses: map[string]herdr.Status{"old": {Protocol: 18, Version: "0.7.0", Path: "/opt/herdr"}}}
	path := saveTargets(t, []target.Target{{Name: "old"}, {Name: "paused", Paused: true}})
	out, err := executeCommand(client, "--config", path, "status")
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	if !strings.Contains(out, "LAST SUCCESS") || !strings.Contains(out, "skewed") || !strings.Contains(out, "paused") || client.snapshotCalls != 0 {
		t.Fatalf("status output = %q, snapshot calls = %d", out, client.snapshotCalls)
	}
}

func TestPausedStatusJSONOmitsUnknownTimestamps(t *testing.T) {
	client := &fakeClient{}
	path := saveTargets(t, []target.Target{{Name: "paused", Paused: true}})
	out := runCommand(t, client, "--config", path, "--output", "json", "status")
	if strings.Contains(out, "fetchedAt") || strings.Contains(out, "lastSuccess") || strings.Contains(out, "0001-01-01") {
		t.Fatalf("paused status contains unknown timestamps: %s", out)
	}
}

func TestStatusJSONIncludesLastSuccess(t *testing.T) {
	client := &fakeClient{}
	path := saveTargets(t, []target.Target{{Name: "box"}})
	out := runCommand(t, client, "--config", path, "--output", "json", "status")
	var envelope struct {
		Targets []targetResult `json:"targets"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Targets) != 1 || envelope.Targets[0].LastSuccess == nil || envelope.Targets[0].LastSuccess.IsZero() {
		t.Fatalf("status JSON = %s", out)
	}
}

func TestFailedStatusJSONOmitsUnknownLastSuccess(t *testing.T) {
	client := &fakeClient{statusErrors: map[string]error{"box": errors.New("offline")}}
	path := saveTargets(t, []target.Target{{Name: "box"}})
	out, _ := executeCommand(client, "--config", path, "--output", "json", "status")
	if strings.Contains(out, "lastSuccess") {
		t.Fatalf("failed status contains an unknown lastSuccess: %s", out)
	}
}

func TestPartialFailureRemainsInJSON(t *testing.T) {
	client := &fakeClient{
		statusErrors: map[string]error{"bad": errors.New("transport failed")},
		agents:       map[string][]herdr.Agent{"good": {{PaneID: "p1", Agent: "codex", Status: "idle"}}},
	}
	path := saveTargets(t, []target.Target{{Name: "good"}, {Name: "bad"}})
	out, err := executeCommand(client, "--config", path, "--output", "json", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"name":"bad"`) || !strings.Contains(out, `"state":"unreachable"`) || !strings.Contains(out, `"name":"good"`) {
		t.Fatalf("partial JSON = %s", out)
	}
}

func TestReadAndTargetsList(t *testing.T) {
	client := &fakeClient{reads: map[string]string{"box/p1": "recent output\n"}}
	path := saveTargets(t, []target.Target{{Name: "box", Prefix: []string{"ssh", "box", "--"}, Interactive: []string{"ssh", "-t", "box", "--"}}})
	if out := runCommand(t, client, "--config", path, "read", "box", "p1", "--lines", "10"); out != "recent output\n" {
		t.Fatalf("read output = %q", out)
	}
	out := runCommand(t, client, "--config", path, "targets", "list")
	if !strings.Contains(out, "ssh box --") || !strings.Contains(out, "ssh -t box --") {
		t.Fatalf("targets output = %q", out)
	}
}

func TestTargetsCRUD(t *testing.T) {
	client := &fakeClient{}
	path := filepath.Join(t.TempDir(), "config", "targets.json")
	out := runCommand(t, client, "--config", path, "targets", "add", "workbox", "--prefix", `ssh -o "ControlMaster auto" workbox --`, "--attach-prefix", "ssh -t workbox --")
	if !strings.Contains(out, "Added workbox, Herdr 0.8.0") {
		t.Fatalf("add output = %q", out)
	}
	got, err := target.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"ssh", "-o", "ControlMaster auto", "workbox", "--"}
	wantAttach := []string{"ssh", "-t", "workbox", "--"}
	if len(got) != 1 || !reflect.DeepEqual(got[0].Prefix, wantPrefix) || !reflect.DeepEqual(got[0].Interactive, wantAttach) {
		t.Fatalf("added targets = %#v", got)
	}
	if out := runCommand(t, client, "--config", path, "targets", "show", "workbox"); !strings.Contains(out, "ControlMaster auto") {
		t.Fatalf("show output = %q", out)
	}

	runCommand(t, client, "--config", path, "targets", "update", "workbox", "--prefix", "docker exec dev", "--attach-prefix", "")
	got, err = target.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got[0].Prefix, []string{"docker", "exec", "dev"}) || got[0].Interactive != nil {
		t.Fatalf("updated target = %#v", got[0])
	}

	runCommand(t, client, "--config", path, "targets", "pause", "workbox")
	runCommand(t, client, "--config", path, "targets", "pause", "workbox")
	got, _ = target.Load(path)
	if !got[0].Paused {
		t.Fatalf("paused target = %#v", got[0])
	}
	runCommand(t, client, "--config", path, "targets", "resume", "workbox")
	runCommand(t, client, "--config", path, "targets", "resume", "workbox")
	got, _ = target.Load(path)
	if got[0].Paused {
		t.Fatalf("resumed target = %#v", got[0])
	}

	runCommand(t, client, "--config", path, "targets", "remove", "workbox")
	got, _ = target.Load(path)
	if len(got) != 0 {
		t.Fatalf("targets after remove = %#v", got)
	}
}

func TestTargetsAddAndUpdatePersistUnhealthyTarget(t *testing.T) {
	client := &fakeClient{statusErrors: map[string]error{"offline": errors.New("transport failed")}}
	path := filepath.Join(t.TempDir(), "targets.json")
	out := runCommand(t, client, "--config", path, "targets", "add", "offline", "--prefix", "ssh offline --")
	if !strings.Contains(out, "Added offline: unreachable: transport failed") {
		t.Fatalf("add output = %q", out)
	}
	got, err := target.Load(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("targets = %#v, %v", got, err)
	}
	out = runCommand(t, client, "--config", path, "targets", "update", "offline", "--prefix", "ssh replacement --")
	if !strings.Contains(out, "Updated offline: unreachable") {
		t.Fatalf("update output = %q", out)
	}
	got, _ = target.Load(path)
	if !reflect.DeepEqual(got[0].Prefix, []string{"ssh", "replacement", "--"}) {
		t.Fatalf("updated target = %#v", got[0])
	}
	out = runCommand(t, client, "--config", path, "targets", "update", "offline", "--attach-prefix", "ssh -t replacement --")
	if out != "Updated offline\n" {
		t.Fatalf("attach-only update output = %q", out)
	}
}

func TestTargetsJSONAndValidation(t *testing.T) {
	client := &fakeClient{}
	path := filepath.Join(t.TempDir(), "targets.json")
	out := runCommand(t, client, "--config", path, "--output", "json", "targets", "add", "local")
	var change targetChange
	if err := json.Unmarshal([]byte(out), &change); err != nil {
		t.Fatal(err)
	}
	if change.Target.Name != "local" || change.State != "ok" || change.Version != "0.8.0" {
		t.Fatalf("add JSON = %#v", change)
	}
	out = runCommand(t, client, "--config", path, "--output", "json", "targets", "show", "local")
	var configured target.Target
	if err := json.Unmarshal([]byte(out), &configured); err != nil || configured.Name != "local" {
		t.Fatalf("show JSON = %q, %v", out, err)
	}

	for _, args := range [][]string{
		{"--config", path, "targets", "add", "local"},
		{"--config", path, "targets", "add", "bad", "--prefix", `ssh "unterminated`},
		{"--config", path, "targets", "update", "local"},
		{"--config", path, "targets", "update", "local", "--attach-prefix", `ssh "unterminated`},
		{"--config", path, "targets", "show", "missing"},
		{"--config", path, "targets", "rm", "missing"},
	} {
		if _, err := executeCommand(client, args...); err == nil {
			t.Fatalf("herdlord %v succeeded", args)
		}
	}
	got, err := target.Load(path)
	if err != nil || len(got) != 1 || got[0].Name != "local" || len(got[0].Prefix) != 0 || got[0].Interactive != nil {
		t.Fatalf("targets after rejected mutations = %#v, %v", got, err)
	}
}

func TestTargetsMutationJSON(t *testing.T) {
	client := &fakeClient{}
	path := saveTargets(t, []target.Target{{Name: "box"}})
	tests := []struct {
		args       []string
		wantPaused bool
	}{
		{args: []string{"targets", "update", "box", "--attach-prefix", "ssh -t box --"}},
		{args: []string{"targets", "pause", "box"}, wantPaused: true},
		{args: []string{"targets", "resume", "box"}},
		{args: []string{"targets", "rm", "box"}},
	}
	for _, tt := range tests {
		args := append([]string{"--config", path, "--output", "json"}, tt.args...)
		out := runCommand(t, client, args...)
		var change targetChange
		if err := json.Unmarshal([]byte(out), &change); err != nil {
			t.Fatalf("herdlord %v JSON = %q: %v", tt.args, out, err)
		}
		if change.Target.Name != "box" || change.Target.Paused != tt.wantPaused {
			t.Fatalf("herdlord %v JSON = %#v", tt.args, change)
		}
	}
}

func TestTargetsCancelledProbeDoesNotSave(t *testing.T) {
	started := make(chan struct{}, 1)
	client := &fakeClient{blockStatus: true, statusStarted: started}
	path := filepath.Join(t.TempDir(), "targets.json")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := newRootCommand(client)
	cmd.SetContext(ctx)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--config", path, "targets", "add", "box"})
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("health probe did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("add error = %v", err)
	}
	got, err := target.Load(path)
	if err != nil || len(got) != 0 {
		t.Fatalf("targets after cancellation = %#v, %v", got, err)
	}
}

func TestTargetsPersistenceFailureLeavesFileUnchanged(t *testing.T) {
	client := &fakeClient{}
	path := saveTargets(t, []target.Target{{Name: "box", Prefix: []string{"ssh", "old", "--"}}})
	_, err := executeCommandConfigured(client, func(opts *options) {
		opts.save = func(string, []target.Target) error { return errors.New("disk full") }
	}, "--config", path, "targets", "update", "box", "--prefix", "ssh new --")
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("update error = %v", err)
	}
	got, loadErr := target.Load(path)
	if loadErr != nil || !reflect.DeepEqual(got, []target.Target{{Name: "box", Prefix: []string{"ssh", "old", "--"}}}) {
		t.Fatalf("targets after save failure = %#v, %v", got, loadErr)
	}
}

func TestConcurrentTargetUpdatesMergeAgainstLatestConfig(t *testing.T) {
	path := saveTargets(t, []target.Target{{Name: "box"}})
	commands := [][]string{
		{"--config", path, "targets", "update", "box", "--prefix", "ssh new --"},
		{"--config", path, "targets", "update", "box", "--attach-prefix", "ssh -t new --"},
	}
	errs := make(chan error, len(commands))
	for _, args := range commands {
		go func() {
			_, err := executeCommand(&fakeClient{}, args...)
			errs <- err
		}()
	}
	for range commands {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	got, err := target.Load(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("targets = %#v, %v", got, err)
	}
	if !reflect.DeepEqual(got[0].Prefix, []string{"ssh", "new", "--"}) || !reflect.DeepEqual(got[0].Interactive, []string{"ssh", "-t", "new", "--"}) {
		t.Fatalf("merged target = %#v", got[0])
	}
}

func TestVersionAndSkillSurfaces(t *testing.T) {
	client := &fakeClient{}
	if flag, command := runCommand(t, client, "--version"), runCommand(t, client, "version"); flag != command {
		t.Fatalf("version surfaces differ:\n%s\n%s", flag, command)
	}
	if flag, command := runCommand(t, client, "--skill"), runCommand(t, client, "skill"); flag != command || flag != skill.Markdown() {
		t.Fatal("skill surfaces differ")
	}
	out := runCommand(t, client, "--output", "json", "skill")
	var info skillInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatal(err)
	}
	if info.Name != "herdlord" || info.ContentSHA256 != skill.SHA256() || info.Content != skill.Markdown() {
		t.Fatalf("skill JSON = %#v", info)
	}
}

func TestHelpAndValidation(t *testing.T) {
	client := &fakeClient{}
	for _, args := range [][]string{
		{"--help"},
		{"list", "--help"},
		{"status", "--help"},
		{"read", "--help"},
		{"targets", "add", "--help"},
		{"targets", "list", "--help"},
		{"targets", "pause", "--help"},
		{"targets", "resume", "--help"},
		{"targets", "rm", "--help"},
		{"targets", "show", "--help"},
		{"targets", "update", "--help"},
		{"skill", "--help"},
	} {
		if out := runCommand(t, client, args...); !strings.Contains(out, "Usage:") {
			t.Fatalf("herdlord %v help = %q", args, out)
		}
	}
	if _, err := executeCommand(client, "--output", "xml", "version"); err == nil {
		t.Fatal("unsupported output succeeded")
	}
	if _, err := executeCommand(client, "read", "box"); err == nil {
		t.Fatal("invalid read arguments succeeded")
	}
	for _, args := range [][]string{{"--timeout", "0", "version"}, {"--timeout", "-1s", "version"}, {"--interval", "0", "version"}, {"--interval", "-1s", "version"}} {
		if _, err := executeCommand(client, args...); err == nil || !strings.Contains(err.Error(), "greater than zero") {
			t.Fatalf("herdlord %v error = %v", args, err)
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteTargetChangeReturnsWriteError(t *testing.T) {
	want := errors.New("closed pipe")
	err := writeTargetChange(failingWriter{err: want}, "text", "Added", target.Target{Name: "box"}, &poll.TargetStatus{State: poll.Unreachable, Err: "offline"})
	if !errors.Is(err, want) {
		t.Fatalf("writeTargetChange() error = %v, want %v", err, want)
	}
}

func saveTargets(t *testing.T, targets []target.Target) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targets.json")
	if err := target.Save(path, targets); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCommand(t *testing.T, client *fakeClient, args ...string) string {
	t.Helper()
	out, err := executeCommand(client, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func executeCommand(client *fakeClient, args ...string) (string, error) {
	return executeCommandConfigured(client, nil, args...)
}

func executeCommandConfigured(client *fakeClient, configure func(*options), args ...string) (string, error) {
	var out bytes.Buffer
	cmd := newRootCommand(client, configure)
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
