package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Color codes — only emitted when stdout is a TTY and NO_COLOR is unset.
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	cyan   = "\033[36m"
	white  = "\033[37m"
)

var colorEnabled bool

func init() {
	// Respect NO_COLOR (https://no-color.org) and non-TTY environments.
	_, noColor := os.LookupEnv("NO_COLOR")
	fi, _ := os.Stdout.Stat()
	isTTY := (fi.Mode() & os.ModeCharDevice) != 0
	colorEnabled = !noColor && isTTY
}

// out is the writer used by all print functions.
// Override in tests via SetOutput.
var out io.Writer = os.Stdout

// SetOutput redirects all UI output. Useful for testing.
func SetOutput(w io.Writer) {
	out = w
}

func colorize(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + reset
}

// ---------------------------------------------------------------------------
// Core print functions
// ---------------------------------------------------------------------------

// Info prints a neutral informational line.
//
//	shipyard :: cloning repository user/my-app
func Info(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := colorize(dim, "::")
	fmt.Fprintf(out, "%s %s\n", prefix, msg)
}

// Ok prints a success confirmation.
//
//	shipyard ok image pushed to ghcr.io/user/my-app:latest
func Ok(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := colorize(green, "ok")
	fmt.Fprintf(out, "%s %s\n", prefix, msg)
}

// Warn prints a non-fatal warning.
//
//	shipyard warn network "edge" already exists, skipping creation
func Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := colorize(yellow, "warn")
	fmt.Fprintf(out, "%s %s\n", prefix, msg)
}

// Error prints an error to stderr. Does not exit.
//
//	shipyard error failed to connect to Docker daemon
func Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := colorize(red, "error")
	fmt.Fprintf(os.Stderr, "%s %s\n", prefix, msg)
}

// Fatal prints an error to stderr and exits with code 1.
func Fatal(format string, args ...any) {
	Error(format, args...)
	os.Exit(1)
}

// Step prints a named action step, useful for multi-phase operations.
//
//	shipyard >> build   building linux/arm64 image
//	shipyard >> push    pushing to registry
func Step(name, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	arrow := colorize(cyan, ">>")
	label := colorize(bold, fmt.Sprintf("%-8s", name))
	fmt.Fprintf(out, "%s %s%s\n", arrow, label, msg)
}

// Section prints a section header — use sparingly, between logical blocks.
//
//	shipyard --- deploy -------------------------------------------------------
func Section(name string) {
	label := colorize(bold+white, strings.ToUpper(name))
	line := colorize(dim, strings.Repeat("-", max(0, 72-len(name)-5)))
	fmt.Fprintf(out, "\n%s %s\n", label, line)
}

// Detail prints an indented subordinate line, for extra context.
//
//	compose  /home/user/.shipyard/apps/my-app/docker-compose.yaml
func Detail(key, value string) {
	k := colorize(dim, fmt.Sprintf("  %-10s", key))
	fmt.Fprintf(out, "%s%s\n", k, value)
}

// ---------------------------------------------------------------------------
// Structured output helpers
// ---------------------------------------------------------------------------

// Confirm prompts the user for a yes/no answer and returns true on "y"/"yes".
// Returns false if stdin is not a TTY (non-interactive mode).
func Confirm(prompt string) bool {
	fi, _ := os.Stdin.Stat()
	if (fi.Mode() & os.ModeCharDevice) == 0 {
		// Non-interactive: treat as confirmed to allow piped usage.
		return false
	}

	q := colorize(yellow, "?")
	fmt.Fprintf(out, "%s %s [y/N] ", q, prompt)

	var answer string
	fmt.Fscan(os.Stdin, &answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// Table prints a simple two-column table with aligned keys and values.
// Commonly used for `shipyard list` output.
//
//	NAME            IMAGE                           MODE      DOMAIN
//	my-app          ghcr.io/user/my-app:latest      traefik   app.local
func Table(headers []string, rows [][]string) {
	if len(rows) == 0 {
		Info("no entries found")
		return
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Header row
	var hdr strings.Builder
	for i, h := range headers {
		hdr.WriteString(fmt.Sprintf("%-*s", widths[i]+2, strings.ToUpper(h)))
	}
	fmt.Fprintln(out, colorize(bold, strings.TrimRight(hdr.String(), " ")))

	// Data rows
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			pad := 0
			if i < len(widths) {
				pad = widths[i] + 2
			}
			line.WriteString(fmt.Sprintf("%-*s", pad, cell))
		}
		fmt.Fprintln(out, strings.TrimRight(line.String(), " "))
	}
}

// ---------------------------------------------------------------------------
// Doctor / check output
// ---------------------------------------------------------------------------

// CheckOk prints a passing dependency check.
//
//	[ok]  git     /usr/bin/git
func CheckOk(name, detail string) {
	tag := colorize(green, "[ok]")
	n := fmt.Sprintf("%-8s", name)
	fmt.Fprintf(out, "  %s  %s%s\n", tag, colorize(bold, n), colorize(dim, detail))
}

// CheckFail prints a failing dependency check.
//
//	[!!]  ssh     not found in PATH
func CheckFail(name, detail string) {
	tag := colorize(red, "[!!]")
	n := fmt.Sprintf("%-8s", name)
	fmt.Fprintf(out, "  %s  %s%s\n", tag, colorize(bold, n), colorize(dim, detail))
}

// ---------------------------------------------------------------------------
// Public color helper
// ---------------------------------------------------------------------------

// Colorize wraps s with the named ANSI color for inline use in Detail values
// or custom output. Supported names: red, green, yellow, cyan, blue, dim, bold.
// Returns s unchanged when color is disabled.
func Colorize(name, s string) string {
	codes := map[string]string{
		"red":    red,
		"green":  green,
		"yellow": yellow,
		"cyan":   cyan,
		"blue":   blue,
		"dim":    dim,
		"bold":   bold,
	}
	code, ok := codes[name]
	if !ok {
		return s
	}
	return colorize(code, s)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
