package cmd

import (
	"fmt"
	"os/exec"

	"shipyard/internal/state"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart [app]",
	Short: "Reinicia un servicio desplegado",
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

		fmt.Println("🔄 Reiniciando:", appName)

		cmdExec := exec.Command(
			"docker",
			"compose",
			"-f",
			app.Path+"/docker-compose.yml",
			"restart",
		)

		cmdExec.Stdout = nil
		cmdExec.Stderr = nil

		if err := cmdExec.Run(); err != nil {
			fmt.Println("❌ Error reiniciando:", err)
			return
		}

		fmt.Println("✅ Restart completado")
	},
}

func init() {
	rootCmd.AddCommand(restartCmd)
}
