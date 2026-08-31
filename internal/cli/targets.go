package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
	"github.com/mjrusso/herdlord/internal/targetmgr"
)

type targetChange struct {
	Target   target.Target `json:"target"`
	State    string        `json:"state,omitempty"`
	Error    string        `json:"error,omitempty"`
	Protocol int           `json:"protocol,omitempty"`
	Version  string        `json:"version,omitempty"`
}

func newTargetsCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{Use: "targets", Short: "Manage configured targets"}
	cmd.AddCommand(
		newTargetsListCommand(opts),
		newTargetsShowCommand(opts),
		newTargetsAddCommand(opts),
		newTargetsUpdateCommand(opts),
		newTargetsPauseCommand(opts, true),
		newTargetsPauseCommand(opts, false),
		newTargetsRemoveCommand(opts),
	)
	return cmd
}

func newTargetsListCommand(opts *options) *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List configured targets", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		targets, _, err := opts.loadTargets()
		if err != nil {
			return err
		}
		return writeTargets(cmd.OutOrStdout(), opts.output, targets)
	}}
}

func newTargetsShowCommand(opts *options) *cobra.Command {
	return &cobra.Command{Use: "show <name>", Short: "Show one configured target", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		targets, _, err := opts.loadTargets()
		if err != nil {
			return err
		}
		configured, err := findTarget(targets, args[0])
		if err != nil {
			return err
		}
		if opts.output == "json" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(configured)
		}
		return writeTargets(cmd.OutOrStdout(), opts.output, []target.Target{configured})
	}}
}

func newTargetsAddCommand(opts *options) *cobra.Command {
	var prefixText, attachText string
	cmd := &cobra.Command{Use: "add <name>", Short: "Add and check a target", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := opts.path()
		if err != nil {
			return err
		}
		prefix, err := target.ParsePrefix(prefixText)
		if err != nil {
			return err
		}
		attach, err := optionalPrefix(attachText)
		if err != nil {
			return err
		}
		configured := target.Target{Name: args[0], Prefix: prefix, Interactive: attach}
		status, err := opts.targetManager().AddChecked(cmd.Context(), path, configured)
		if err != nil {
			return err
		}
		return writeTargetChange(cmd.OutOrStdout(), opts.output, "Added", configured, &status)
	}}
	cmd.Flags().StringVar(&prefixText, "prefix", "", "command prefix")
	cmd.Flags().StringVar(&attachText, "attach-prefix", "", "interactive command prefix; defaults to prefix")
	return cmd
}

func newTargetsUpdateCommand(opts *options) *cobra.Command {
	var prefixText, attachText string
	cmd := &cobra.Command{Use: "update <name>", Short: "Update a target's prefixes", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		prefixChanged := cmd.Flags().Changed("prefix")
		attachChanged := cmd.Flags().Changed("attach-prefix")
		if !prefixChanged && !attachChanged {
			return errors.New("at least one of --prefix or --attach-prefix is required")
		}
		path, err := opts.path()
		if err != nil {
			return err
		}
		var prefix, attach []string
		if prefixChanged {
			prefix, err = target.ParsePrefix(prefixText)
			if err != nil {
				return err
			}
		}
		if attachChanged {
			attach, err = optionalPrefix(attachText)
			if err != nil {
				return err
			}
		}
		configured, status, err := opts.targetManager().Update(cmd.Context(), path, args[0], func(configured *target.Target) error {
			if prefixChanged {
				configured.Prefix = prefix
			}
			if attachChanged {
				configured.Interactive = attach
			}
			return nil
		}, prefixChanged)
		if err != nil {
			return err
		}
		return writeTargetChange(cmd.OutOrStdout(), opts.output, "Updated", configured, status)
	}}
	cmd.Flags().StringVar(&prefixText, "prefix", "", "command prefix; empty selects local Herdr")
	cmd.Flags().StringVar(&attachText, "attach-prefix", "", "interactive command prefix; empty defaults to prefix")
	return cmd
}

func newTargetsPauseCommand(opts *options, paused bool) *cobra.Command {
	verb := "pause"
	short := "Pause a target"
	if !paused {
		verb, short = "resume", "Resume a target"
	}
	return &cobra.Command{Use: verb + " <name>", Short: short, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := opts.path()
		if err != nil {
			return err
		}
		configured, err := opts.targetManager().SetPaused(path, args[0], paused)
		if err != nil {
			return err
		}
		action := "Paused"
		if !paused {
			action = "Resumed"
		}
		return writeTargetChange(cmd.OutOrStdout(), opts.output, action, configured, nil)
	}}
}

func newTargetsRemoveCommand(opts *options) *cobra.Command {
	return &cobra.Command{Use: "rm <name>", Aliases: []string{"remove"}, Short: "Remove a target", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := opts.path()
		if err != nil {
			return err
		}
		removed, err := opts.targetManager().Remove(path, args[0])
		if err != nil {
			return err
		}
		return writeTargetChange(cmd.OutOrStdout(), opts.output, "Removed", removed, nil)
	}}
}

func optionalPrefix(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	return target.ParsePrefix(value)
}

func (o *options) targetManager() targetmgr.Manager {
	return targetmgr.Manager{Poller: o.manager(), Save: o.save}
}

func writeTargets(w io.Writer, format string, targets []target.Target) error {
	if format == "json" {
		return json.NewEncoder(w).Encode(targets)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tPREFIX\tATTACH PREFIX\tPAUSED")
	for _, configured := range targets {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%t\n", display.Text(configured.Name), display.Text(quoteArgs(configured.Prefix)), display.Text(quoteArgs(configured.InteractivePrefix())), configured.Paused)
	}
	return tw.Flush()
}

func writeTargetChange(w io.Writer, format, action string, configured target.Target, status *poll.TargetStatus) error {
	if format == "json" {
		change := targetChange{Target: configured}
		if status != nil {
			change.State = status.State.String()
			change.Error = status.Err
			change.Protocol = status.Protocol
			change.Version = status.Version
		}
		return json.NewEncoder(w).Encode(change)
	}
	if status == nil {
		_, err := fmt.Fprintf(w, "%s %s\n", action, display.Text(configured.Name))
		return err
	}
	message := fmt.Sprintf("%s %s: %s", action, display.Text(configured.Name), status.State)
	if status.State.Usable() && status.State == poll.OK {
		message = fmt.Sprintf("%s %s, Herdr %s", action, display.Text(configured.Name), display.Text(status.Version))
	} else if status.Err != "" {
		message += ": " + display.Text(status.Err)
	}
	_, err := fmt.Fprintln(w, message)
	return err
}
