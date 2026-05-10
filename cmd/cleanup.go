package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"shipyard/internal/state"

	"github.com/spf13/cobra"
)

var composeBase string

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Elimina servicios huérfanos (no registrados en state)",
	Run: func(cmd *cobra.Command, args []string) {

		fmt.Println("🧹 Shipyard cleanup iniciado")

		// =========================
		// LOAD STATE
		// =========================

		st, err := state.Load()
		if err != nil {
			fmt.Println("❌ state error:", err)
			return
		}

		// =========================
		// READ DIRECTORY
		// =========================

		entries, err := os.ReadDir(composeBase)
		if err != nil {
			fmt.Println("❌ error leyendo directorio:", err)
			return
		}

		// =========================
		// CHECK ORPHANS
		// =========================

		for _, e := range entries {

			if !e.IsDir() {
				continue
			}

			name := e.Name()

			if _, exists := st.Apps[name]; !exists {

				fmt.Println("🧹 Orphan detectado:", name)

				path := filepath.Join(composeBase, name)

				err := os.RemoveAll(path)
				if err != nil {
					fmt.Println("❌ error borrando:", name, err)
					continue
				}

				fmt.Println("❌ eliminado:", name)
			}
		}

		fmt.Println("✅ cleanup terminado")
	},
}

func init() {
	rootCmd.AddCommand(cleanupCmd)

	cleanupCmd.Flags().StringVar(
		&composeBase,
		"compose-path",
		"/srv/apps",
		"Directorio base de apps",
	)
}
