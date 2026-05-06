package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"shipyard/internal/config"

	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configura Shipyard por primera vez",
	Run: func(cmd *cobra.Command, args []string) {

		reader := bufio.NewReader(os.Stdin)

		fmt.Print("GitHub user: ")
		githubUser, _ := reader.ReadString('\n')

		fmt.Print("GitHub token: ")
		githubToken, _ := reader.ReadString('\n')

		fmt.Print("Raspberry host (IP): ")
		host, _ := reader.ReadString('\n')

		fmt.Print("SSH user: ")
		user, _ := reader.ReadString('\n')

		fmt.Print("SSH key path (~/.ssh/id_rsa): ")
		sshKey, _ := reader.ReadString('\n')

		cfg := &config.Config{
			GitHubUser:  strings.TrimSpace(githubUser),
			GitHubToken: strings.TrimSpace(githubToken),
		}

		cfg.Device.Host = strings.TrimSpace(host)
		cfg.Device.User = strings.TrimSpace(user)
		cfg.Device.SSHKey = strings.TrimSpace(sshKey)

		err := config.Save(cfg)
		if err != nil {
			fmt.Println("❌ Error guardando config:", err)
			return
		}

		fmt.Println("✅ Configuración guardada en ~/.shipyard.yaml")
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
