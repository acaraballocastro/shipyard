package cmd

import (
	"os"

	"shipyard/internal/prompt"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "shipyard",
	Short: "Lightweight deployment CLI for Docker and homelab servers",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		ui.Fatal("%s", err)
		os.Exit(1)
	}
}

// exitQuiet ends the process successfully, e.g. after the user cancels a prompt.
func exitQuiet() { os.Exit(0) }

func init() {
	rootCmd.PersistentFlags().StringVar(&serverFlag, "server", "",
		"Run on a configured server (see: shipyard server list), or \"local\"")
	rootCmd.PersistentFlags().BoolVar(&prompt.Disabled, "no-input", false,
		"Never prompt; fail when required arguments are missing (also SHIPYARD_NO_INPUT=1)")
}
