package display

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func Text(value string) string {
	return sanitize(value, false)
}

func Block(value string) string {
	return sanitize(value, true)
}

func sanitize(value string, multiline bool) string {
	value = ansi.Strip(value)
	return strings.Map(func(r rune) rune {
		if multiline && r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}
