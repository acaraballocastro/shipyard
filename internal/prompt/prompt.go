// Package prompt holds the interactive helpers used when a command is run
// without the arguments it needs and a terminal is attached.
package prompt

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/huh"

	"shipyard/internal/state"
)

// ErrAborted is returned when the user cancels a prompt (Ctrl+C / Esc).
var ErrAborted = errors.New("aborted")

// Disabled is set by the global --no-input flag.
var Disabled bool

// Interactive reports whether prompts may be shown: stdin and stdout must
// be terminals, and neither --no-input nor SHIPYARD_NO_INPUT may be set.
func Interactive() bool {
	if Disabled || os.Getenv("SHIPYARD_NO_INPUT") != "" || os.Getenv("CI") != "" {
		return false
	}
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Run executes a form, mapping a user abort to ErrAborted.
func Run(f *huh.Form) error {
	err := f.WithTheme(huh.ThemeCharm()).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrAborted
	}
	return err
}

// SelectApp lets the user choose one of the deployed services.
func SelectApp(title string, apps map[string]state.App) (string, error) {
	if len(apps) == 0 {
		return "", errors.New("no services deployed yet (try: shipyard deploy)")
	}

	names := make([]string, 0, len(apps))
	for n := range apps {
		names = append(names, n)
	}
	sort.Strings(names)

	opts := make([]huh.Option[string], 0, len(names))
	for _, n := range names {
		app := apps[n]
		label := n
		if app.Domain != "" {
			label = fmt.Sprintf("%s  (%s)", n, app.Domain)
		} else if app.Mode != "" {
			label = fmt.Sprintf("%s  (%s)", n, app.Mode)
		}
		opts = append(opts, huh.NewOption(label, n))
	}

	var choice string
	err := Run(huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Options(opts...).
			Filtering(true).
			Value(&choice),
	)))
	return choice, err
}
