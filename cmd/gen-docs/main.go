package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra/doc"

	"github.com/mjrusso/herdlord/internal/cli"
)

func main() {
	out := filepath.Join("docs", "commands")
	if len(os.Args) == 2 {
		out = os.Args[1]
	}
	if err := os.RemoveAll(out); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	cmd := cli.NewRootCommand()
	cmd.DisableAutoGenTag = true
	if err := doc.GenMarkdownTree(cmd, out); err != nil {
		log.Fatal(err)
	}
	if err := filepath.WalkDir(out, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(strings.TrimRight(string(content), "\n")+"\n"), 0o644)
	}); err != nil {
		log.Fatal(err)
	}
}
