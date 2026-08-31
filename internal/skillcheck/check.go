package skillcheck

import (
	"bufio"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

const maxSize = 12 * 1024

func Validate(markdown string, root *cobra.Command) error {
	if len(markdown) > maxSize {
		return fmt.Errorf("skill is %d bytes; maximum is %d", len(markdown), maxSize)
	}
	if err := validateFrontmatter(markdown); err != nil {
		return err
	}
	return validateBash(markdown, root)
}

func validateFrontmatter(markdown string) error {
	if !strings.HasPrefix(markdown, "---\n") {
		return fmt.Errorf("skill must begin with YAML frontmatter")
	}
	rest := strings.TrimPrefix(markdown, "---\n")
	frontmatter, body, ok := strings.Cut(rest, "\n---\n")
	if !ok || strings.TrimSpace(body) == "" {
		return fmt.Errorf("skill must contain closed frontmatter and a markdown body")
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &fields); err != nil {
		return fmt.Errorf("parse skill frontmatter: %w", err)
	}
	if len(fields) != 2 {
		return fmt.Errorf("skill frontmatter must contain exactly name and description")
	}
	name, nameOK := fields["name"].(string)
	description, descriptionOK := fields["description"].(string)
	if !nameOK || name != "herdlord" {
		return fmt.Errorf("skill frontmatter name must be herdlord")
	}
	if !descriptionOK || strings.TrimSpace(description) == "" {
		return fmt.Errorf("skill frontmatter description must be a non-empty string")
	}
	if utf8.RuneCountInString(description) > 500 {
		return fmt.Errorf("skill frontmatter description exceeds 500 characters")
	}
	return nil
}

func validateBash(markdown string, root *cobra.Command) error {
	scanner := bufio.NewScanner(strings.NewReader(markdown))
	inBash := false
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "```") {
			if inBash {
				inBash = false
			} else {
				inBash = line == "```bash"
			}
			continue
		}
		fields := strings.Fields(line)
		if !inBash || len(fields) == 0 || fields[0] != "herdlord" {
			continue
		}
		if strings.ContainsAny(line, "|;&$`()\\") {
			return fmt.Errorf("line %d: complex shell command is not supported", lineNumber)
		}
		if err := validateInvocation(fields, root); err != nil {
			return fmt.Errorf("line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan skill: %w", err)
	}
	if inBash {
		return fmt.Errorf("unclosed bash code fence")
	}
	return nil
}

func validateInvocation(tokens []string, root *cobra.Command) error {
	cmd, args, err := root.Find(tokens[1:])
	if err != nil {
		return err
	}
	cmd.InitDefaultHelpFlag()
	changed := map[string]bool{}
	cmd.Flags().VisitAll(func(flag *pflag.Flag) { changed[flag.Name] = flag.Changed })
	defer cmd.Flags().VisitAll(func(flag *pflag.Flag) { flag.Changed = changed[flag.Name] })
	if err := cmd.ParseFlags(args); err != nil {
		return fmt.Errorf("%s: %w", cmd.CommandPath(), err)
	}
	if help := cmd.Flag("help"); help != nil && help.Changed {
		return nil
	}
	positional := cmd.Flags().Args()
	if len(positional) > 0 && cmd.HasSubCommands() && cmd.Run == nil && cmd.RunE == nil {
		return fmt.Errorf("unknown command %q for %q", positional[0], cmd.CommandPath())
	}
	if err := cmd.ValidateArgs(positional); err != nil {
		return fmt.Errorf("%s: %w", cmd.CommandPath(), err)
	}
	return nil
}
