package herdr

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mjrusso/herdlord/internal/target"
)

type runnerFunc func(context.Context, []string) (string, error)

func (f runnerFunc) Run(ctx context.Context, argv []string) (string, error) {
	return f(ctx, argv)
}

func TestCommand(t *testing.T) {
	want := []string{"ssh", "box", "--", "env", "-u", "HERDR_SOCKET_PATH", "-u", "HERDR_CLIENT_SOCKET_PATH", "herdr", "api", "snapshot"}
	got := Command([]string{"ssh", "box", "--"}, "herdr", "api", "snapshot")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Command() = %#v, want %#v", got, want)
	}
}

func TestStatusUsesServerFieldsAndResolvedPath(t *testing.T) {
	output := `client:
  version: 0.8.0
  protocol: 19

server:
  status: running
  version: 0.9.0
  protocol: 20
`
	client := Client{Runner: runnerFunc(func(_ context.Context, argv []string) (string, error) {
		if !reflect.DeepEqual(argv, []string{"ssh", "box", "--", "env", "-u", "HERDR_SOCKET_PATH", "-u", "HERDR_CLIENT_SOCKET_PATH", "herdr", "status"}) {
			t.Fatalf("status command = %#v", argv)
		}
		return output, nil
	})}
	got, err := client.Status(context.Background(), target.Target{Name: "box", Prefix: []string{"ssh", "box", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	want := Status{Protocol: 20, Version: "0.9.0", Path: "herdr"}
	if got != want {
		t.Fatalf("Status() = %#v, want %#v", got, want)
	}
}

func TestParseStatusRejectsStoppedServer(t *testing.T) {
	_, err := parseStatus("server:\n  status: not running\n")
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("parseStatus() error = %v", err)
	}
}

func TestStatusFindsUserLocalInstallation(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(binDir, "herdr")
	stub := "#!/bin/sh\nprintf 'client:\\n  version: 0.8.0\\n  protocol: 19\\n\\nserver:\\n  status: running\\n  version: 0.8.0\\n  protocol: 19\\n'\n"
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/bin:/usr/bin")
	status, err := (Client{}).Status(context.Background(), target.Target{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Path != path {
		t.Fatalf("Status().Path = %q, want %q", status.Path, path)
	}
}

func TestStatusFindsRemoteUserLocalInstallationWithoutShell(t *testing.T) {
	output := `client:
  version: 0.8.0
  protocol: 19

server:
  status: running
  version: 0.8.0
  protocol: 19
`
	tests := []struct {
		name       string
		prefix     []string
		directErr  error
		directText string
	}{
		{name: "SSH", prefix: []string{"ssh", "workbox", "--"}, directErr: errors.New("exit status 127"), directText: "sh: herdr: not found"},
		{name: "Voom SSH", prefix: []string{"voom", "ssh", "play", "--"}, directErr: errors.New("exit status 1"), directText: "fish: Unknown command: herdr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls [][]string
			client := Client{Runner: runnerFunc(func(_ context.Context, argv []string) (string, error) {
				calls = append(calls, append([]string(nil), argv...))
				switch argv[len(argv)-1] {
				case "status":
					if argv[len(argv)-2] == "herdr" {
						return "", &CommandError{Args: argv, Stderr: tt.directText, Err: tt.directErr}
					}
					return output, nil
				case "env":
					return "SHELL=/opt/homebrew/bin/fish\nHOME=/Users/mjr\n", nil
				default:
					return "", errors.New("unexpected command")
				}
			})}
			status, err := client.Status(context.Background(), target.Target{Name: "remote", Prefix: tt.prefix})
			if err != nil {
				t.Fatal(err)
			}
			if status.Path != "/Users/mjr/.local/bin/herdr" {
				t.Fatalf("Status().Path = %q", status.Path)
			}
			for _, call := range calls {
				if contains(call, "sh") || contains(call, "-c") {
					t.Fatalf("status invoked a shell: %#v", call)
				}
			}
		})
	}
}

func TestStatusDoesNotMaskHerdrFailureWithFallback(t *testing.T) {
	want := &CommandError{Stderr: "herdr server failed", Err: errors.New("exit status 1")}
	client := Client{Runner: runnerFunc(func(_ context.Context, argv []string) (string, error) {
		if argv[len(argv)-1] != "status" {
			t.Fatalf("unexpected fallback command: %#v", argv)
		}
		return "", want
	})}
	_, err := client.Status(context.Background(), target.Target{Name: "remote", Prefix: []string{"ssh", "workbox", "--"}})
	if !errors.Is(err, want) {
		t.Fatalf("Status() error = %v, want %v", err, want)
	}
}

func TestSnapshotDecoding(t *testing.T) {
	client := Client{Runner: runnerFunc(func(_ context.Context, argv []string) (string, error) {
		if argv[len(argv)-2] != "api" || argv[len(argv)-1] != "snapshot" {
			t.Fatalf("snapshot argv = %#v", argv)
		}
		return `{"result":{"snapshot":{"protocol":20,"workspaces":[{"workspace_id":"w1","label":"herdlord"}],"tabs":[{"tab_id":"w1:t1","workspace_id":"w1","label":"dashboard"}],"agents":[{"workspace_id":"w1","tab_id":"w1:t1","pane_id":"w1:p1","terminal_id":"term_1","agent":"codex","agent_status":"working","cwd":"/tmp/project","terminal_title":"◑ Building","terminal_title_stripped":"Building","revision":7}]}}}`, nil
	})}
	agents, err := client.Snapshot(context.Background(), target.Target{}, "/opt/herdr")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Workspace != "herdlord" || agents[0].Tab != "dashboard" || agents[0].Title() != "Building" || agents[0].PaneID != "w1:p1" || agents[0].Revision != 7 {
		t.Fatalf("Snapshot() = %#v", agents)
	}
}

func TestSnapshotAcceptsProtocol19(t *testing.T) {
	client := Client{Runner: runnerFunc(func(context.Context, []string) (string, error) {
		return `{"result":{"snapshot":{"protocol":19,"workspaces":[],"tabs":[],"agents":[]}}}`, nil
	})}
	agents, err := client.Snapshot(context.Background(), target.Target{}, "herdr")
	if err != nil || len(agents) != 0 {
		t.Fatalf("Snapshot() = %#v, %v", agents, err)
	}
}

func TestSnapshotAttemptsNewerProtocol(t *testing.T) {
	client := Client{Runner: runnerFunc(func(context.Context, []string) (string, error) {
		return `{"result":{"snapshot":{"protocol":21,"workspaces":[],"tabs":[],"agents":[]}}}`, nil
	})}
	agents, err := client.Snapshot(context.Background(), target.Target{}, "herdr")
	if err != nil || len(agents) != 0 {
		t.Fatalf("Snapshot() = %#v, %v", agents, err)
	}
}

func TestAgentTitleFallsBackToTerminalTitle(t *testing.T) {
	if got := (Agent{TerminalTitle: "◑ Building"}).Title(); got != "◑ Building" {
		t.Fatalf("Title() = %q", got)
	}
}

func TestSnapshotFailures(t *testing.T) {
	tests := []struct {
		name   string
		output string
		check  func(error) bool
	}{
		{"malformed", `{`, func(err error) bool { return strings.Contains(err.Error(), "decode snapshot") }},
		{"api error", `{"error":{"code":-1,"message":"failed"}}`, func(err error) bool { return strings.Contains(err.Error(), "herdr API error") }},
		{"protocol skew", `{"result":{"snapshot":{"protocol":18}}}`, func(err error) bool { var target *ProtocolError; return errors.As(err, &target) && target.Got == 18 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := Client{Runner: runnerFunc(func(context.Context, []string) (string, error) { return tt.output, nil })}
			_, err := client.Snapshot(context.Background(), target.Target{}, "herdr")
			if err == nil || !tt.check(err) {
				t.Fatalf("Snapshot() error = %v", err)
			}
		})
	}
}

func TestReadUsesResolvedPath(t *testing.T) {
	client := Client{Runner: runnerFunc(func(_ context.Context, argv []string) (string, error) {
		if !contains(argv, "/opt/herdr") || !contains(argv, "--format") || !contains(argv, "recent-unwrapped") || argv[len(argv)-1] != "text" {
			t.Fatalf("read argv = %#v", argv)
		}
		return "recent output", nil
	})}
	got, err := client.Read(context.Background(), target.Target{}, "/opt/herdr", "w1:p1", 12)
	if err != nil || got != "recent output" {
		t.Fatalf("Read() = %q, %v", got, err)
	}
}

func TestAttachUsesResolvedPath(t *testing.T) {
	cmd, err := AttachCommand(target.Target{Interactive: []string{"ssh", "-t", "box", "--"}}, "/opt/herdr", "term_1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-t", "box", "--", "env", "-u", "HERDR_SOCKET_PATH", "-u", "HERDR_CLIENT_SOCKET_PATH", "/opt/herdr", "terminal", "attach", "term_1"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("AttachCommand() = %#v, want %#v", cmd.Args, want)
	}
	if _, err := AttachCommand(target.Target{Interactive: []string{""}}, "/opt/herdr", "term_1"); err == nil || err.Error() != "attach command has no executable" {
		t.Fatalf("empty attach executable error = %v", err)
	}
}

func TestExecRunnerFailuresAndTimeout(t *testing.T) {
	runner := ExecRunner{}
	if _, err := runner.Run(context.Background(), nil); err == nil || err.Error() != "command has no executable" {
		t.Fatalf("empty command error = %v", err)
	}
	if _, err := runner.Run(context.Background(), []string{""}); err == nil || err.Error() != "command has no executable" {
		t.Fatalf("empty executable error = %v", err)
	}
	_, err := runner.Run(context.Background(), []string{"sh", "-c", "printf failure >&2; exit 7"})
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.Stderr != "failure" {
		t.Fatalf("non-zero error = %#v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, []string{"sh", "-c", "sleep 5"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestLocalIntegration(t *testing.T) {
	if os.Getenv("HERDLORD_INTEGRATION") == "" {
		t.Skip("set HERDLORD_INTEGRATION=1 to use the local Herdr server")
	}
	client := Client{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := client.Status(ctx, target.Target{Name: "local", Prefix: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath(status.Path); err != nil {
		t.Fatalf("resolved path %q: %v", status.Path, err)
	}
	if _, err := client.Snapshot(ctx, target.Target{Name: "local", Prefix: []string{}}, status.Path); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
