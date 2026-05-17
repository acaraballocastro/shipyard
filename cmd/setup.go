package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"shipyard/internal/config"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure Shipyard for first use",
	Run: func(cmd *cobra.Command, args []string) {

		ui.Section("setup")

		reader := bufio.NewReader(os.Stdin)

		prompt := func(label string) string {
			fmt.Fprintf(os.Stdout, "  %-20s", label+": ")
			val, _ := reader.ReadString('\n')
			return strings.TrimSpace(val)
		}

		githubUser := prompt("GitHub user")
		githubToken := prompt("GitHub token")
		host := prompt("device host (IP)")
		user := prompt("SSH user")
		sshKey := prompt("SSH key path")

		cfg := &config.Config{
			GitHubUser:  githubUser,
			GitHubToken: githubToken,
		}

		cfg.Device.Host = host
		cfg.Device.User = user
		cfg.Device.SSHKey = sshKey

		if err := config.Save(cfg); err != nil {
			ui.Fatal("saving config: %s", err)
		}

		ui.Ok("configuration saved to ~/.shipyard.yaml")
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
