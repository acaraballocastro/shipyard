package cmd

import (
	"os"
	"os/exec"

	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs [app]",
	Short: "Stream logs from a deployed service",
	Args:  appArg,

	Run: func(cmd *cobra.Command, args []string) {

		appName := resolveApp(cmd, args, "Which service do you want to see logs for?")

		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}

		app, exists := st.Apps[appName]
		if !exists {
			ui.Fatal("app not found: %s", appName)
		}

		ui.Info("streaming logs for %s", appName)

		cmdExec := exec.Command(
			"docker", "compose",
			"-f", app.Path+"/docker-compose.yml",
			"logs", "-f",
		)

		cmdExec.Stdout = os.Stdout
		cmdExec.Stderr = os.Stderr

		if err := cmdExec.Run(); err != nil {
			ui.Fatal("docker compose logs: %s", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
}
