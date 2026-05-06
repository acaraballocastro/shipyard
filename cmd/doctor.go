package cmd

import (
	"fmt"

	"shipyard/internal/system"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Verifica dependencias del sistema",
	Run: func(cmd *cobra.Command, args []string) {

		check("git")
		check("docker")
		check("ssh")
	},
}

func check(name string) {
	err := system.CheckCommand(name)
	if err != nil {
		fmt.Printf("❌ %s\n", err)
	} else {
		fmt.Printf("✅ %s instalado\n", name)
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}