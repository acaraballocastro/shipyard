package cmd

import (
	"errors"

	"shipyard/internal/prompt"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

// appArg accepts an optional service name; when omitted it is asked for
// interactively.
var appArg = cobra.MaximumNArgs(1)

// resolveApp returns the service name from args, or lets the user pick one
// from the deployed services. When the target is a remote server the
// command is forwarded there and resolveApp does not return.
func resolveApp(cmd *cobra.Command, args []string, title string) string {
	t := resolveTarget()

	name := ""
	if len(args) == 1 {
		name = args[0]
	} else {
		if !prompt.Interactive() {
			ui.Fatal("missing service name (usage: shipyard %s)", cmd.Use)
		}
		apps, err := loadApps(t)
		if err != nil {
			ui.Fatal("%s", err)
		}
		name, err = prompt.SelectApp(title, apps)
		if errors.Is(err, prompt.ErrAborted) {
			ui.Info("aborted")
			exitQuiet()
		}
		if err != nil {
			ui.Fatal("%s", err)
		}
	}

	if t.remote() {
		forward(t, cmd, []string{name})
	}
	return name
}
