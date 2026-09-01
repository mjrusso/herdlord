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
	path := "herdr"
	out, err := c.runner().Run(ctx, Command(t.Prefix, path, "status"))
	if err != nil {
		directErr := err
		if !commandUnavailable(err) {
			return Status{}, err
		}
		environment, envErr := c.runner().Run(ctx, Command(t.Prefix, "env"))
		if envErr != nil {
			return Status{}, directErr
		}
		home := environmentValue(environment, "HOME")
		if home == "" || !strings.HasPrefix(home, "/") {
			return Status{}, directErr
		}
		path = strings.TrimRight(home, "/") + "/.local/bin/herdr"
		out, err = c.runner().Run(ctx, Command(t.Prefix, path, "status"))
		if err != nil {
			return Status{}, err
		}
	}
	status, err := parseStatus(out)
	if err != nil {
		return Status{}, err
	}
	status.Path = path
	return status, nil
}

func commandUnavailable(err error) bool {
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		return false
	}
	var execErr *exec.Error
	if errors.As(commandErr.Err, &execErr) {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(commandErr.Err, &exitErr) {
		if exitErr.ExitCode() == 127 {
			return true
		}
	}
	message := strings.ToLower(commandErr.Stderr + " " + commandErr.Err.Error())
	return strings.Contains(message, "herdr: not found") ||
		strings.Contains(message, "herdr: command not found") ||
		strings.Contains(message, "unknown command: herdr") ||
		(strings.Contains(message, "herdr") && strings.Contains(message, "no such file"))
}

func environmentValue(output, name string) string {
	prefix := name + "="
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
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
