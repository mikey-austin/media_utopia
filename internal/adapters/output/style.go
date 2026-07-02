package output

import (
	"os"
	"regexp"
	"sync/atomic"

	"golang.org/x/term"
)

// colorOn gates every ANSI escape the output package emits. It is decided
// once at startup (TTY + NO_COLOR/CLICOLOR/--no-color) so colors never leak
// into pipes or files.
var colorOn atomic.Bool

// SetColorEnabled turns styled output on or off globally.
func SetColorEnabled(v bool) { colorOn.Store(v) }

// ColorsEnabled reports whether styled output is active.
func ColorsEnabled() bool { return colorOn.Load() }

// AutoColor computes whether colors should be enabled given the standard
// conventions: an explicit --no-color flag, the NO_COLOR env var
// (https://no-color.org/), CLICOLOR=0, and whether stdout is a terminal.
func AutoColor(noColorFlag bool) bool {
	if noColorFlag || os.Getenv("NO_COLOR") != "" || os.Getenv("CLICOLOR") == "0" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// TerminalWidth returns the stdout terminal width, or fallback when stdout
// is not a terminal (pipes get a stable generous budget).
func TerminalWidth(fallback int) int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return fallback
}

func sgr(code string, s string) string {
	if s == "" || !colorOn.Load() {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// The palette. Deliberately small: headers and chrome are dim, identifiers
// are dim, state is colored, everything else is the terminal default.
func Dim(s string) string    { return sgr("2", s) }
func Bold(s string) string   { return sgr("1", s) }
func Header(s string) string { return sgr("1;2", s) }
func Green(s string) string  { return sgr("32", s) }
func Yellow(s string) string { return sgr("33", s) }
func Red(s string) string    { return sgr("31", s) }
func Cyan(s string) string   { return sgr("36", s) }

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes SGR sequences (used for width math on styled text).
func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }
