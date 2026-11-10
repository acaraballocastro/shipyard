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
// from the deployed services. Exits when neither is possible.
func resolveApp(cmd *cobra.Command, args []string, title string) string {
	if len(args) == 1 {
		return args[0]
	}
	if !prompt.Interactive() {
		ui.Fatal("missing service name (usage: shipyard %s)", cmd.Use)
	}
	name, err := prompt.SelectApp(title)
	if errors.Is(err, prompt.ErrAborted) {
		ui.Info("aborted")
		exitQuiet()
	}
	if err != nil {
		ui.Fatal("%s", err)
	}
	return name
}
