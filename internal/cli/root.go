package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/mjrusso/herdlord/internal/buildinfo"
	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/herdr"
	"github.com/mjrusso/herdlord/internal/poll"
	"github.com/mjrusso/herdlord/internal/target"
	"github.com/mjrusso/herdlord/internal/ui"
)

type options struct {
	config   string
	output   string
	format   string
	interval time.Duration
	timeout  time.Duration
	version  bool
	skill    bool
	client   poll.Client
	save     func(string, []target.Target) error
}

func Execute() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := NewRootCommand().ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "herdlord:", display.Text(err.Error()))
		os.Exit(1)
	}
}

func NewRootCommand() *cobra.Command { return newRootCommand(herdr.Client{}) }

func newRootCommand(client poll.Client, configure ...func(*options)) *cobra.Command {
	opts := &options{output: "text", interval: 2 * time.Second, timeout: 10 * time.Second, client: client}
	for _, apply := range configure {
		if apply != nil {
			apply(opts)
		}
	}
	cmd := &cobra.Command{
		Use:           "herdlord",
		Short:         "See every Herdr agent in one place",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if opts.timeout <= 0 {
				return errors.New("--timeout must be greater than zero")
			}
			if opts.interval <= 0 {
				return errors.New("--interval must be greater than zero")
			}
			if opts.format != "" {
				if cmd.Flags().Changed("output") {
					return errors.New("--output and --format cannot be used together")
				}
				switch opts.format {
				case "table":
					opts.output = "text"
				case "json":
					opts.output = "json"
				default:
					return fmt.Errorf("unsupported format %q; use table or json", opts.format)
				}
			}
			if opts.output != "text" && opts.output != "json" {
				return fmt.Errorf("unsupported output format %q; use text or json", opts.output)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.version && opts.skill {
				return fmt.Errorf("--version and --skill cannot be used together")
			}
			if opts.version {
				return writeVersion(cmd.OutOrStdout(), opts.output)
			}
			if opts.skill {
				return writeSkill(cmd.OutOrStdout(), buildinfo.Version, opts.output)
			}
			return runTUI(opts)
		},
	}
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.PersistentFlags().StringVar(&opts.config, "config", "", "targets file (default: user config directory)")
	cmd.PersistentFlags().StringVar(&opts.output, "output", "text", "output format: text or json")
	cmd.PersistentFlags().StringVar(&opts.format, "format", "", "output format: table or json")
	cmd.PersistentFlags().DurationVar(&opts.timeout, "timeout", 10*time.Second, "timeout for each Herdr command")
	cmd.PersistentFlags().DurationVar(&opts.interval, "interval", 2*time.Second, "TUI poll interval")
	cmd.Flags().BoolVar(&opts.version, "version", false, "print version information")
	cmd.Flags().BoolVar(&opts.skill, "skill", false, "print the Herdlord agent skill")
	cmd.AddCommand(newListCommand(opts), newStatusCommand(opts), newReadCommand(opts), newTargetsCommand(opts), newVersionCommand(opts), newSkillCommand(opts))
	return cmd
}

func (o *options) path() (string, error) {
	if o.config != "" {
		return o.config, nil
	}
	return target.DefaultPath()
}

func (o *options) loadTargets() ([]target.Target, string, error) {
	path, err := o.path()
	if err != nil {
		return nil, "", err
	}
	targets, err := target.Load(path)
	return targets, path, err
}

func (o *options) manager() poll.Manager {
	return poll.Manager{Client: o.client, Interval: o.interval, Timeout: o.timeout}
}

func runTUI(opts *options) error {
	targets, path, err := opts.loadTargets()
	if err != nil {
		return err
	}
	model := ui.New(targets, path, opts.manager())
	program := tea.NewProgram(model, tea.WithAltScreen())
	model.SetProgram(program)
	_, err = program.Run()
	return err
}
