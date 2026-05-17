package cmd

import (
	"shipyard/internal/ui"
	shipyardUpdate "shipyard/internal/update"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [app]",
	Short: "Rebuild and redeploy a running service",
	Args:  cobra.ExactArgs(1),

	Run: func(cmd *cobra.Command, args []string) {

		appName := args[0]

		ui.Section("update")
		ui.Info("updating %s", appName)

		if err := shipyardUpdate.Run(appName); err != nil {
			ui.Fatal("%s", err)
		}

		ui.Ok("%s updated", appName)
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
