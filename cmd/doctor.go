package cmd

import (
	"os"

	"shipyard/internal/bootstrap"
	"shipyard/internal/config"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that this machine (or --server) can run shipyard",
	Run: func(cmd *cobra.Command, args []string) {

		if t := resolveTarget(); t.remote() {
			forward(t, cmd, nil)
		}

		ui.Section("doctor")

		cfg, _ := config.LoadOrEmpty()
		if cfg.GitHubUser == "" {
			ui.CheckFail("config", "GitHub user not set")
		} else {
			ui.CheckOk("config", "github_user: "+cfg.GitHubUser)
		}

		failing := checkSteps(bootstrap.Steps(func() string { return cfg.GitHubUser }, bootstrap.Inputs{}))
		if len(failing) > 0 || cfg.GitHubUser == "" {
			ui.Info("run shipyard setup to fix")
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
