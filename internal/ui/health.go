package ui

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/mjrusso/herdlord/internal/display"
	"github.com/mjrusso/herdlord/internal/poll"
)

func (m *Model) healthView() string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	contentWidth := max(1, width-4)
	var body strings.Builder
	tw := tabwriter.NewWriter(&body, 0, 0, 2, ' ', 0)
	compact := width < 70
	if compact {
		_, _ = fmt.Fprintln(tw, "TARGET\tSTATE\tERROR")
	} else {
		_, _ = fmt.Fprintln(tw, "TARGET\tSTATE\tLAST SUCCESS\tERROR")
	}
	now := time.Now()
	count, hidden := 0, 0
	limit := m.healthRowLimit()
	for _, configured := range m.targets {
		status, ok := m.statuses[configured.Name]
		if !ok || status.State == poll.OK || status.State == poll.Paused || status.State == poll.Checking {
			continue
		}
		if count >= limit {
			hidden++
			continue
		}
		err := strings.Join(strings.Fields(display.Text(status.Err)), " ")
		targetName := ansi.Truncate(display.Text(configured.Name), 14, "…")
		errWidth := max(8, contentWidth-34)
		if !compact {
			errWidth = max(12, contentWidth-52)
		}
		err = ansi.Truncate(err, errWidth, "…")
		if compact {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", targetName, status.State.String(), err)
		} else {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", targetName, status.State.String(), relativeAge(status.LastSuccess, now), err)
		}
		count++
	}
	if count == 0 {
		return ""
	}
	_ = tw.Flush()
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render("Target health")
	view := title + "\n" + strings.TrimRight(body.String(), "\n")
	if hidden > 0 {
		view += fmt.Sprintf("\n… and %d more", hidden)
	}
	return lipgloss.NewStyle().
		Width(max(1, width-2)).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Render(view)
}

func (m *Model) healthRowLimit() int {
	switch {
	case m.height > 42:
		return 4
	case m.height > 30:
		return 2
	default:
		return 1
	}
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
