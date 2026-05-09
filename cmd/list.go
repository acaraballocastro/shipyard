package cmd

import (
	"fmt"

	"shipyard/internal/state"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use: "list",
	Run: func(cmd *cobra.Command, args []string) {

		st, _ := state.Load()

		fmt.Println("📦 Apps desplegadas:")

		for name, app := range st.Apps {
			fmt.Println("─")
			fmt.Println("Name:", name)
			fmt.Println("Repo:", app.Repo)
			fmt.Println("Image:", app.Image)
			fmt.Println("Mode:", app.Mode)
			fmt.Println("Path:", app.Path)
		}
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
