package cli

import (
	"encoding/json"
	"io"

	"github.com/spf13/cobra"

	herdlordskill "github.com/mjrusso/herdlord/internal/skill"
)

type skillInfo struct {
	Name            string `json:"name"`
	ProducerVersion string `json:"producerVersion"`
	ContentSHA256   string `json:"contentSha256"`
	Content         string `json:"content"`
}

func writeSkill(w io.Writer, producerVersion, format string) error {
	if format == "json" {
		return json.NewEncoder(w).Encode(skillInfo{Name: "herdlord", ProducerVersion: producerVersion, ContentSHA256: herdlordskill.SHA256(), Content: herdlordskill.Markdown()})
	}
	_, err := io.WriteString(w, herdlordskill.Markdown())
	return err
}

func newSkillCommand(opts *options) *cobra.Command {
	return &cobra.Command{Use: "skill", Short: "Print the Herdlord agent skill", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return writeSkill(cmd.OutOrStdout(), buildVersion(), opts.output)
	}}
}
