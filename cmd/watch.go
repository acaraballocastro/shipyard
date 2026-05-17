package cmd

import (
	"fmt"
	"time"

	"shipyard/internal/git"
	"shipyard/internal/state"
	"shipyard/internal/ui"
	shipyardUpdate "shipyard/internal/update"

	"github.com/spf13/cobra"
)

var interval int

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Poll repositories and redeploy on new commits",

	Run: func(cmd *cobra.Command, args []string) {

		ui.Info("watch started, polling every %d minute(s)", interval)

		for {
			st, err := state.Load()
			if err != nil {
				ui.Error("state: %s", err)
				time.Sleep(time.Minute)
				continue
			}

			for name, app := range st.Apps {
				ui.Info("checking %s", name)

				latest, err := git.GetLatestCommit(app.Repo)
				if err != nil {
					ui.Error("%s: commit fetch: %s", name, err)
					continue
				}

				// first run — seed commit hash
				if app.LastCommit == "" {
					app.LastCommit = latest
					st.Apps[name] = app
					state.Save(st)
					continue
				}

				// no change
				if latest == app.LastCommit {
					continue
				}

				// new commit detected
				ui.Step("deploy", "%s — new commit %s", name, fmt.Sprintf("%.8s", latest))

				if err := shipyardUpdate.Run(name); err != nil {
					ui.Error("%s: update: %s", name, err)
					continue
				}

				app.LastCommit = latest
				st.Apps[name] = app
				state.Save(st)

				ui.Ok("%s redeployed", name)
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
		"Polling interval in minutes",
	)
}
