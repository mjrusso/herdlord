package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/fleet"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
)

type targetResult struct {
	Name        string        `json:"name"`
	State       string        `json:"state"`
	Error       string        `json:"error,omitempty"`
	Protocol    int           `json:"protocol,omitempty"`
	Version     string        `json:"version,omitempty"`
	FetchedAt   *time.Time    `json:"fetchedAt,omitempty"`
	LastSuccess *time.Time    `json:"lastSuccess,omitempty"`
	Agents      []herdr.Agent `json:"agents,omitempty"`
}

func newListCommand(opts *options) *cobra.Command {
	var names []string
	var includePaused bool
	cmd := &cobra.Command{Use: "list", Short: "List agents across targets", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		targets, _, err := opts.loadTargets()
		if err != nil {
			return err
		}
		observations, err := (fleet.Collector{Manager: opts.manager()}).Collect(cmd.Context(), targets, names, includePaused, false)
		if err != nil {
			return err
		}
		if err := writeList(cmd.OutOrStdout(), opts.output, observations); err != nil {
			return err
		}
		if fleet.AllFailed(observations) {
			return errors.New("all requested targets failed")
		}
		return nil
	}}
	cmd.Flags().StringSliceVar(&names, "target", nil, "restrict output to target names")
	cmd.Flags().BoolVar(&includePaused, "include-paused", false, "include paused targets")
	return cmd
}

func newStatusCommand(opts *options) *cobra.Command {
	var includePaused bool
	cmd := &cobra.Command{Use: "status [target]", Short: "Show target health and compatibility", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		targets, _, err := opts.loadTargets()
		if err != nil {
			return err
		}
		var names []string
		if len(args) == 1 {
			names = args
			includePaused = true
		}
		observations, err := (fleet.Collector{Manager: opts.manager()}).Collect(cmd.Context(), targets, names, includePaused, true)
		if err != nil {
			return err
		}
		if err := writeStatus(cmd.OutOrStdout(), opts.output, observations); err != nil {
			return err
		}
		if fleet.AllFailed(observations) {
			return errors.New("all requested targets failed")
		}
		return nil
	}}
	cmd.Flags().BoolVar(&includePaused, "include-paused", true, "include paused targets")
	return cmd
}

func newReadCommand(opts *options) *cobra.Command {
	var lines int
	cmd := &cobra.Command{Use: "read <target> <pane>", Short: "Print recent output from one agent pane", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if lines < 1 {
			return errors.New("lines must be at least 1")
		}
		targets, _, err := opts.loadTargets()
		if err != nil {
			return err
		}
		selected, err := findTarget(targets, args[0])
		if err != nil {
			return err
		}
		selected.Paused = false
		probe := opts.manager().Probe(cmd.Context(), selected)
		if !probe.State.Usable() {
			return fmt.Errorf("target %s: %s: %s", selected.Name, probe.State, probe.Err)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), opts.manager().EffectiveTimeout())
		defer cancel()
		output, err := opts.client.Read(ctx, selected, probe.HerdrPath, args[1], lines)
		if err != nil {
			return err
		}
		if opts.output == "json" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"target": selected.Name, "paneId": args[1], "output": output})
		}
		_, err = io.WriteString(cmd.OutOrStdout(), display.Block(output))
		return err
	}}
	cmd.Flags().IntVar(&lines, "lines", 50, "number of recent lines")
	return cmd
}

func writeList(w io.Writer, format string, observations []fleet.Observation) error {
	if format == "json" {
		return json.NewEncoder(w).Encode(map[string]any{"targets": results(observations)})
	}
	type row struct{ target, workspace, tab, agent, status, title string }
	var rows []row
	for _, observation := range observations {
		if observation.Status.State == poll.Newer {
			rows = append(rows, row{observation.Target.Name, "—", "—", "—", observation.Status.State.String(), observation.Status.Err})
		}
		if !observation.Status.State.Usable() || len(observation.Status.Agents) == 0 {
			state := observation.Status.State.String()
			if observation.Status.State.Usable() {
				state = "no agents"
			}
			rows = append(rows, row{observation.Target.Name, "—", "—", "—", state, observation.Status.Err})
			continue
		}
		for _, agent := range observation.Status.Agents {
			rows = append(rows, row{observation.Target.Name, agent.Workspace, agent.Tab, agent.Agent, agent.Status, agent.Title()})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return agentRank(rows[i].status) < agentRank(rows[j].status) })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TARGET\tWORKSPACE\tTAB\tAGENT\tSTATUS\tTITLE")
	for _, row := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", display.Text(row.target), display.Text(row.workspace), display.Text(row.tab), display.Text(row.agent), display.Text(row.status), display.Text(row.title))
	}
	return tw.Flush()
}

func writeStatus(w io.Writer, format string, observations []fleet.Observation) error {
	if format == "json" {
		return json.NewEncoder(w).Encode(map[string]any{"targets": results(observations)})
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TARGET\tSTATE\tLAST SUCCESS\tVERSION\tPROTOCOL\tERROR")
	now := time.Now()
	for _, observation := range observations {
		s := observation.Status
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n", display.Text(observation.Target.Name), s.State, relativeAge(s.LastSuccess, now), display.Text(s.Version), s.Protocol, display.Text(s.Err))
	}
	return tw.Flush()
}

func results(observations []fleet.Observation) []targetResult {
	result := make([]targetResult, len(observations))
	for i, observation := range observations {
		s := observation.Status
		var fetchedAt *time.Time
		if !s.FetchedAt.IsZero() {
			value := s.FetchedAt
			fetchedAt = &value
		}
		var lastSuccess *time.Time
		if !s.LastSuccess.IsZero() {
			value := s.LastSuccess
			lastSuccess = &value
		}
		result[i] = targetResult{Name: observation.Target.Name, State: s.State.String(), Error: s.Err, Protocol: s.Protocol, Version: s.Version, FetchedAt: fetchedAt, LastSuccess: lastSuccess, Agents: s.Agents}
	}
	return result
}

func relativeAge(when, now time.Time) string {
	if when.IsZero() {
		return "never"
	}
	age := now.Sub(when)
	if age < time.Second {
		return "just now"
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds ago", int(age/time.Second))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm ago", int(age/time.Minute))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(age/time.Hour))
	}
	return fmt.Sprintf("%dd ago", int(age/(24*time.Hour)))
}

func findTarget(targets []target.Target, name string) (target.Target, error) {
	for _, configured := range targets {
		if configured.Name == name {
			return configured, nil
		}
	}
	return target.Target{}, fmt.Errorf("target %q is not configured", name)
}

func quoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\"'") {
			quoted[i] = fmt.Sprintf("%q", arg)
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}

func agentRank(status string) int {
	switch status {
	case "blocked":
		return 0
	case "done":
		return 1
	case "working":
		return 2
	case "idle":
		return 3
	default:
		return 4
	}
}
