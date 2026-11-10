package cmd

import (
	"shipyard/internal/core"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart [app]",
	Short: "Restart a deployed service",
	Args:  appArg,

	Run: func(cmd *cobra.Command, args []string) {
		appName := resolveApp(cmd, args, "Which service do you want to restart?")

		if err := core.New().Restart(cmd.Context(), appName, ui.Progress()); err != nil {
			ui.Fatal("%s", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(restartCmd)
}
