package cmd

import (
	"fmt"
	"os/exec"

	"shipyard/internal/state"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs [app]",
	Short: "Muestra logs de un servicio",
	Args:  cobra.ExactArgs(1),

	Run: func(cmd *cobra.Command, args []string) {

		appName := args[0]

		st, err := state.Load()
		if err != nil {
			fmt.Println("❌ Error cargando state:", err)
			return
		}

		app, exists := st.Apps[appName]
		if !exists {
			fmt.Println("❌ App no encontrada:", appName)
			return
		}

		fmt.Println("📜 Logs de:", appName)

		cmdExec := exec.Command(
			"docker",
			"compose",
			"-f",
			app.Path+"/docker-compose.yml",
			"logs",
			"-f",
		)

		cmdExec.Stdout = nil
		cmdExec.Stderr = nil

		if err := cmdExec.Run(); err != nil {
			fmt.Println("❌ Error mostrando logs:", err)
			return
		}
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
}
