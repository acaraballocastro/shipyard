package cmd

import (
	"os"
	"path/filepath"

	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var composeBase string

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove orphaned services not registered in state",
	Run: func(cmd *cobra.Command, args []string) {

		ui.Section("cleanup")

		// =========================
		// LOAD STATE
		// =========================

		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}

		// =========================
		// READ DIRECTORY
		// =========================

		entries, err := os.ReadDir(composeBase)
		if err != nil {
			ui.Fatal("reading %s: %s", composeBase, err)
		}

		// =========================
		// CHECK ORPHANS
		// =========================

		found := 0

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}

			name := e.Name()
			if _, exists := st.Apps[name]; exists {
				continue
			}

			found++
			path := filepath.Join(composeBase, name)

			ui.Step("remove", "%s", name)
			ui.Detail("path", path)

			if err := os.RemoveAll(path); err != nil {
				ui.Error("removing %s: %s", name, err)
				continue
			}

			ui.Ok("%s removed", name)
		}

		if found == 0 {
			ui.Info("no orphans found")
		}
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)

	cleanupCmd.Flags().StringVar(
		&composeBase,
		"compose-path",
		"/srv/apps",
		"Base directory for deployed apps",
	)
}
