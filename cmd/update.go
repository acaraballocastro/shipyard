package cmd

import (
	"shipyard/internal/core"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [app]",
	Short: "Rebuild and redeploy a running service",
	Args:  appArg,

	Run: func(cmd *cobra.Command, args []string) {
		appName := resolveApp(cmd, args, "Which service do you want to update?")

		ui.Section("update")
		if err := core.New().Update(cmd.Context(), appName, ui.Progress()); err != nil {
			ui.Fatal("%s", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
