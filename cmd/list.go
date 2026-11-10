package cmd

import (
	"encoding/json"
	"os"
	"sort"

	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var listJSON bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List deployed applications",
	Run: func(cmd *cobra.Command, args []string) {

		if t := resolveTarget(); t.remote() {
			forward(t, cmd, nil)
		}

		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}

		if listJSON {
			if err := json.NewEncoder(os.Stdout).Encode(st.Apps); err != nil {
				ui.Fatal("%s", err)
			}
			return
		}

		names := make([]string, 0, len(st.Apps))
		for name := range st.Apps {
			names = append(names, name)
		}
		sort.Strings(names)

		headers := []string{"name", "image", "mode", "domain", "path"}
		rows := make([][]string, 0, len(st.Apps))

		for _, name := range names {
			app := st.Apps[name]
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
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")
}
