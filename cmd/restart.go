package cmd

import (
	"os"
	"os/exec"

	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart [app]",
	Short: "Restart a deployed service",
	Args:  appArg,

	Run: func(cmd *cobra.Command, args []string) {

		appName := resolveApp(cmd, args, "Which service do you want to restart?")

		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}

		app, exists := st.Apps[appName]
		if !exists {
			ui.Fatal("app not found: %s", appName)
		}

		ui.Step("restart", "restarting %s", appName)

		cmdExec := exec.Command(
			"docker", "compose",
			"-f", app.Path+"/docker-compose.yml",
			"restart",
		)

		cmdExec.Stdout = os.Stdout
		cmdExec.Stderr = os.Stderr

		if err := cmdExec.Run(); err != nil {
			ui.Fatal("docker compose restart: %s", err)
		}

		ui.Ok("%s restarted", appName)
	},
}

func init() {
	rootCmd.AddCommand(restartCmd)
}
