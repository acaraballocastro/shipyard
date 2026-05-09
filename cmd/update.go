package cmd

import (
	"fmt"

	shipyardUpdate "shipyard/internal/update"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update [app]",
	Short: "Actualiza una aplicación desplegada",
	Args:  cobra.ExactArgs(1),

	Run: func(cmd *cobra.Command, args []string) {

		appName := args[0]

		fmt.Println("🔄 Shipyard update:", appName)

		err := shipyardUpdate.Run(appName)
		if err != nil {
			fmt.Println("❌ Error:", err)
			return
		}

		fmt.Println("✅ Update completado")
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
