package cmd

import (
	"shipyard/internal/core"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var composeBase string

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove orphaned services not registered in state",
	Run: func(cmd *cobra.Command, args []string) {

		if t := resolveTarget(); t.remote() {
			forward(t, cmd, nil)
		}

		ui.Section("cleanup")

		removed, err := core.New().Cleanup(composeBase, ui.Progress())
		if err != nil {
			ui.Error("%s", err)
		}
		if len(removed) == 0 && err == nil {
			ui.Info("no orphans found")
		}
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)

	cleanupCmd.Flags().StringVar(&composeBase, "compose-path", core.DefaultRoot, "Base directory for deployed apps")
}
