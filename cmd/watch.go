package cmd

import (
	"fmt"
	"time"

	"shipyard/internal/git"
	"shipyard/internal/state"
	shipyardUpdate "shipyard/internal/update"

	"github.com/spf13/cobra"
)

var interval int

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Auto deploy watcher",

	Run: func(cmd *cobra.Command, args []string) {

		fmt.Println("👀 Shipyard watch iniciado")

		for {

			st, err := state.Load()
			if err != nil {
				fmt.Println("❌ Error state:", err)
				time.Sleep(time.Minute)
				continue
			}

			for name, app := range st.Apps {

				fmt.Println("🔍 Revisando:", name)

				latest, err := git.GetLatestCommit(app.Repo)
				if err != nil {
					fmt.Println("❌ Error commit:", err)
					continue
				}

				// primera vez
				if app.LastCommit == "" {
					app.LastCommit = latest
					st.Apps[name] = app
					state.Save(st)
					continue
				}

				// cambio detectado
				if latest != app.LastCommit {

					fmt.Println("🚀 Nuevo commit detectado:", name)

					// llamar update
					err := shipyardUpdate.Run(name)
					if err != nil {
						fmt.Println("❌ Error update:", err)
						continue
					}

					app.LastCommit = latest
					st.Apps[name] = app
					state.Save(st)

					fmt.Println("✅ Deploy actualizado:", name)
				}
			}

			time.Sleep(time.Duration(interval) * time.Minute)
		}
	},
}

func init() {
	rootCmd.AddCommand(watchCmd)

	watchCmd.Flags().IntVar(
		&interval,
		"interval",
		5,
		"Intervalo en minutos",
	)
}
