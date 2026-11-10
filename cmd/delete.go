package cmd

import (
	"errors"

	"shipyard/internal/core"
	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete [app]",
	Short: "Remove a deployed service",
	Args:  appArg,
	Run: func(cmd *cobra.Command, args []string) {
		appName := resolveApp(cmd, args, "Which service do you want to delete?")

		// Fail early on unknown apps instead of asking for confirmation.
		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}
		if _, ok := st.Apps[appName]; !ok {
			ui.Fatal("%s", errors.Join(core.ErrNotFound, errors.New(appName)))
		}

		if !ui.Confirm("this will remove " + appName + " and its compose file. continue?") {
			ui.Info("aborted")
			return
		}

		ui.Section("delete")
		if err := core.New().Delete(cmd.Context(), appName, ui.Progress()); err != nil {
			ui.Fatal("%s", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)
}
