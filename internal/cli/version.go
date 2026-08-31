package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/mjrusso/herdlord/internal/buildinfo"
)

type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func buildVersion() string { return buildinfo.Version }

func writeVersion(w io.Writer, format string) error {
	info := versionInfo{Version: buildinfo.Version, Commit: buildinfo.Commit, Date: buildinfo.Date}
	if format == "json" {
		return json.NewEncoder(w).Encode(info)
	}
	_, err := fmt.Fprintf(w, "herdlord %s\ncommit: %s\nbuilt: %s\n", info.Version, info.Commit, info.Date)
	return err
}

func newVersionCommand(opts *options) *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print version information", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return writeVersion(cmd.OutOrStdout(), opts.output)
	}}
}
