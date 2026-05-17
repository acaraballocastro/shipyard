package cmd

import (
	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List deployed applications",
	Run: func(cmd *cobra.Command, args []string) {

		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}

		headers := []string{"name", "image", "mode", "domain", "path"}
		rows := make([][]string, 0, len(st.Apps))

		for name, app := range st.Apps {
			domain := app.Domain
			if domain == "" {
				domain = "-"
			}
			rows = append(rows, []string{name, app.Image, app.Mode, domain, app.Path})
		}

		ui.Table(headers, rows)
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
