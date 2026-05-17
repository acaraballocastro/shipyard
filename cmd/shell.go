package cmd

import (
	"os"
	"os/exec"

	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var shellUser string

var shellCmd = &cobra.Command{
	Use:   "shell [app]",
	Short: "Open an interactive shell inside a running container",
	Args:  cobra.ExactArgs(1),

	Run: func(cmd *cobra.Command, args []string) {

		appName := args[0]

		// =========================
		// LOAD STATE
		// =========================

		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}

		if _, exists := st.Apps[appName]; !exists {
			ui.Fatal("app not found: %s", appName)
		}

		// =========================
		// RESOLVE SHELL
		// =========================

		// Try bash first, fall back to sh.
		// We probe with a no-op exec inside the container before handing
		// over the TTY, so the error message is clean.
		shell := resolveShell(appName)

		ui.Info("opening shell in %s (%s)", appName, shell)

		// =========================
		// EXEC — hand over the TTY
		// =========================

		c := exec.Command(
			"docker", "exec",
			"-it",
			"-u", shellUser,
			appName,
			shell,
		)

		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr

		if err := c.Run(); err != nil {
			// Exit code 130 = Ctrl+C / normal exit from shell — not an error.
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				if code == 130 || code == 0 {
					return
				}
			}
			ui.Fatal("shell exited with error: %s", err)
		}
	},
}

// resolveShell probes the container for bash; falls back to sh.
func resolveShell(container string) string {
	probe := exec.Command(
		"docker", "exec",
		container,
		"which", "bash",
	)
	if err := probe.Run(); err == nil {
		return "bash"
	}
	return "sh"
}

func init() {
	rootCmd.AddCommand(shellCmd)

	shellCmd.Flags().StringVarP(
		&shellUser,
		"user", "u",
		"root",
		"User to run the shell as",
	)
}
