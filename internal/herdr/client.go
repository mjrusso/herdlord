package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mjrusso/herdlord/internal/target"
)

const Protocol = 20

const supportedProtocolList = "19, 20"

func SupportsProtocol(protocol int) bool {
	return protocol == 19 || protocol == 20
}

func CanAttemptProtocol(protocol int) bool {
	return protocol >= 19
}

func NewerProtocolWarning(protocol int) string {
	return fmt.Sprintf("protocol %d is newer than tested protocols %s; attempting compatibility", protocol, supportedProtocolList)
}

const pathMarker = "__HERDLORD_PATH__="

type Agent struct {
	WorkspaceID           string `json:"workspace_id"`
	Workspace             string `json:"workspace"`
	TabID                 string `json:"tab_id"`
	Tab                   string `json:"tab"`
	PaneID                string `json:"pane_id"`
	TerminalID            string `json:"terminal_id"`
	Agent                 string `json:"agent"`
	Status                string `json:"agent_status"`
	CWD                   string `json:"cwd"`
	TerminalTitle         string `json:"terminal_title"`
	TerminalTitleStripped string `json:"terminal_title_stripped,omitempty"`
	Revision              int64  `json:"revision"`
}

func (a Agent) Title() string {
	if a.TerminalTitleStripped != "" {
		return a.TerminalTitleStripped
	}
	return a.TerminalTitle
}

type Status struct {
	Protocol int
	Version  string
	Path     string
}

type CommandError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("%v: %s", e.Err, e.Stderr)
	}
	return e.Err.Error()
}

func (e *CommandError) Unwrap() error { return e.Err }

type ProtocolError struct {
	Got int
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("protocol %d is too old (minimum: 19)", e.Got)
}

type Runner interface {
	Run(context.Context, []string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string) (string, error) {
	if len(argv) == 0 || argv[0] == "" {
		return "", errors.New("command has no executable")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return "", &CommandError{Args: argv, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return string(out), nil
}

type Client struct {
	Runner Runner
}

func (c Client) runner() Runner {
	if c.Runner != nil {
		return c.Runner
	}
	return ExecRunner{}
}

func (c Client) Status(ctx context.Context, t target.Target) (Status, error) {
	script := `path=$(command -v herdr 2>/dev/null || true)
if [ -z "$path" ] && [ -x "${HOME:-}/.local/bin/herdr" ]; then
  path="${HOME}/.local/bin/herdr"
fi
if [ -z "$path" ]; then
  printf 'herdlord: herdr: not found\n' >&2
  exit 127
fi
printf '` + pathMarker + `%s\n' "$path"
exec "$path" status`
	out, err := c.runner().Run(ctx, Command(t.Prefix, "sh", "-c", script))
	if err != nil {
		return Status{}, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], pathMarker) {
		return Status{}, errors.New("herdr status did not report its resolved path")
	}
	path := strings.TrimPrefix(lines[0], pathMarker)
	if path == "" || !strings.HasPrefix(path, "/") {
		return Status{}, fmt.Errorf("herdr resolved to invalid path %q", path)
	}
	status, err := parseStatus(strings.Join(lines[1:], "\n"))
	if err != nil {
		return Status{}, err
	}
	status.Path = path
	return status, nil
}

func (c Client) Snapshot(ctx context.Context, t target.Target, herdrPath string) ([]Agent, error) {
	if herdrPath == "" {
		herdrPath = "herdr"
	}
	out, err := c.runner().Run(ctx, Command(t.Prefix, herdrPath, "api", "snapshot"))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result struct {
			Snapshot struct {
				Protocol   int `json:"protocol"`
				Workspaces []struct {
					ID    string `json:"workspace_id"`
					Label string `json:"label"`
				} `json:"workspaces"`
				Tabs []struct {
					ID    string `json:"tab_id"`
					Label string `json:"label"`
				} `json:"tabs"`
				Agents []Agent `json:"agents"`
			} `json:"snapshot"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return nil, fmt.Errorf("herdr API error: %s", envelope.Error)
	}
	if !CanAttemptProtocol(envelope.Result.Snapshot.Protocol) {
		return nil, &ProtocolError{Got: envelope.Result.Snapshot.Protocol}
	}
	workspaces := make(map[string]string, len(envelope.Result.Snapshot.Workspaces))
	for _, workspace := range envelope.Result.Snapshot.Workspaces {
		workspaces[workspace.ID] = workspace.Label
	}
	tabs := make(map[string]string, len(envelope.Result.Snapshot.Tabs))
	for _, tab := range envelope.Result.Snapshot.Tabs {
		tabs[tab.ID] = tab.Label
	}
	agents := envelope.Result.Snapshot.Agents
	for i := range agents {
		agents[i].Workspace = workspaces[agents[i].WorkspaceID]
		agents[i].Tab = tabs[agents[i].TabID]
	}
	return agents, nil
}

func (c Client) Read(ctx context.Context, t target.Target, herdrPath, paneID string, lines int) (string, error) {
	if herdrPath == "" {
		herdrPath = "herdr"
	}
	return c.runner().Run(ctx, Command(t.Prefix, herdrPath, "agent", "read", paneID, "--source", "recent-unwrapped", "--lines", strconv.Itoa(lines), "--format", "text"))
}

func Command(prefix []string, herdrPath string, args ...string) []string {
	argv := append([]string{}, prefix...)
	argv = append(argv, "env", "-u", "HERDR_SOCKET_PATH", "-u", "HERDR_CLIENT_SOCKET_PATH", herdrPath)
	return append(argv, args...)
}

func AttachCommand(t target.Target, herdrPath, terminalID string) (*exec.Cmd, error) {
	if herdrPath == "" {
		herdrPath = "herdr"
	}
	argv := append([]string{}, t.InteractivePrefix()...)
	argv = append(argv, "env", "-u", "HERDR_SOCKET_PATH", "-u", "HERDR_CLIENT_SOCKET_PATH", herdrPath, "terminal", "attach", terminalID)
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("attach command has no executable")
	}
	return exec.Command(argv[0], argv[1:]...), nil
}

func parseStatus(output string) (Status, error) {
	section := ""
	fields := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if line == "server:" {
			section = "server"
			continue
		}
		if len(line) > 0 && line[0] != ' ' && strings.HasSuffix(line, ":") {
			section = ""
			continue
		}
		if section != "server" {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	if fields["status"] != "running" {
		return Status{}, errors.New("herdr server is not running")
	}
	protocol, err := strconv.Atoi(fields["protocol"])
	if err != nil || fields["version"] == "" {
		return Status{}, errors.New("herdr status did not report a server version and protocol")
	}
	return Status{Protocol: protocol, Version: fields["version"]}, nil
}
