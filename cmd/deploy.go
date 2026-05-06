package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Despliega una aplicación",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🚀 Deploy en progreso...")
		fmt.Println("⚠️ Aún no implementado")
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}