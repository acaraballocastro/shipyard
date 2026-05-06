package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configura Shipyard por primera vez",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("⚙️ Ejecutando setup...")

		// Aquí luego pediremos:
		// - usuario GitHub
		// - token
		// - datos SSH

		fmt.Println("✅ Setup completado (placeholder)")
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}