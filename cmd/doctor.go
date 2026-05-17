package cmd

import (
	"os/exec"

	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system dependencies",
	Run: func(cmd *cobra.Command, args []string) {

		ui.Section("doctor")
		checkDep("git")
		checkDep("docker")
		checkDep("ssh")
	},
}

// checkDep verifies that a binary is reachable in PATH and prints the result.
func checkDep(name string) {
	path, err := exec.LookPath(name)
	if err != nil {
		ui.CheckFail(name, "not found in PATH")
	} else {
		ui.CheckOk(name, path)
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
