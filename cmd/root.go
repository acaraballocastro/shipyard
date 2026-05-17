package cmd

import (
	"os"

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
