package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"shipyard/internal/state"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete [app]",
	Short: "Elimina un servicio desplegado",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		appName := args[0]

		fmt.Println("🗑️ Shipyard delete:", appName)

		// =========================
		// LOAD STATE
		// =========================

		st, err := state.Load()
		if err != nil {
			fmt.Println("❌ Error cargando state:", err)
			return
		}

		app, ok := st.Apps[appName]
		if !ok {
			fmt.Println("❌ App no encontrada en state:", appName)
			return
		}

		// =========================
		// CONFIRMATION
		// =========================

		fmt.Printf("⚠️ Vas a eliminar: %s\n", appName)
		fmt.Print("¿Estás seguro? (y/N): ")

		var response string
		fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			fmt.Println("❌ Cancelado")
			return
		}

		// =========================
		// STOP CONTAINER
		// =========================

		fmt.Println("🛑 Deteniendo servicio...")

		cmdDown := exec.Command("docker", "compose", "down")
		cmdDown.Dir = app.Path
		cmdDown.Stdout = os.Stdout
		cmdDown.Stderr = os.Stderr

		if err := cmdDown.Run(); err != nil {
			fmt.Println("❌ Error en docker compose down:", err)
			return
		}

		// =========================
		// DELETE FILES
		// =========================

		fmt.Println("🧹 Eliminando archivos...")

		if err := os.RemoveAll(app.Path); err != nil {
			fmt.Println("❌ Error borrando carpeta:", err)
			return
		}

		// =========================
		// UPDATE STATE
		// =========================

		delete(st.Apps, appName)

		if err := state.Save(st); err != nil {
			fmt.Println("❌ Error guardando state:", err)
			return
		}

		fmt.Println("✅ Servicio eliminado correctamente:", appName)
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)
}
