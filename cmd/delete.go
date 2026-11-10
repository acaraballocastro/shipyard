package cmd

import (
	"os"
	"os/exec"
	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete [app]",
	Short: "Elimina un servicio desplegado",
	Args:  appArg,
	Run: func(cmd *cobra.Command, args []string) {

		appName := resolveApp(cmd, args, "Which service do you want to delete?")

		// =========================
		// LOAD STATE
		// =========================

		st, err := state.Load()
		if err != nil {
			ui.Fatal("Error loading state: %v", err)
			return
		}

		app, ok := st.Apps[appName]
		if !ok {
			ui.Fatal("App not found in state: %s", appName)
			return
		}

		// =========================
		// CONFIRMATION
		// =========================

		if !ui.Confirm("this will remove " + appName + " and its compose file. continue?") {
			ui.Info("aborted")
			return
		}

		ui.Section("delete")

		// =========================
		// STOP CONTAINER
		// =========================

		ui.Step("down", "stopping containers")

		cmdDown := exec.Command("docker", "compose", "down")
		cmdDown.Dir = app.Path
		cmdDown.Stdout = os.Stdout
		cmdDown.Stderr = os.Stderr

		if err := cmdDown.Run(); err != nil {
			ui.Fatal("docker compose down: %s", err)
		}

		// =========================
		// DELETE FILES
		// =========================

		ui.Step("clean", "removing files")

		if err := os.RemoveAll(app.Path); err != nil {
			ui.Fatal("removing directory: %s", err)
		}

		// =========================
		// UPDATE STATE
		// =========================

		delete(st.Apps, appName)

		if err := state.Save(st); err != nil {
			ui.Fatal("saving state: %s", err)
		}

		ui.Ok("%s removed", appName)

	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)
}
