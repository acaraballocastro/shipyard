package cmd

import (
	"os"

	"shipyard/internal/core"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var logsTail int
var logsNoFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs [app]",
	Short: "Stream logs from a deployed service",
	Args:  appArg,

	Run: func(cmd *cobra.Command, args []string) {
		appName := resolveApp(cmd, args, "Which service do you want to see logs for?")

		ui.Info("streaming logs for %s", appName)
		opts := core.LogsOptions{Follow: !logsNoFollow, Tail: logsTail}
		if err := core.New().Logs(cmd.Context(), appName, opts, os.Stdout); err != nil {
			ui.Fatal("%s", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().IntVar(&logsTail, "tail", 0, "Number of lines to show from the end (0 = all)")
	logsCmd.Flags().BoolVar(&logsNoFollow, "no-follow", false, "Print the logs and exit")
}
