package cmd

import (
	"fmt"
	"path/filepath"

	"shipyard/internal/compose"
	"shipyard/internal/config"
	"shipyard/internal/docker"
	"shipyard/internal/git"
	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var repo string
var image string

var ports []string
var volumes []string
var composePath string
var mode string
var domain string
var internalPort string
var network string
var createNetwork bool

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy from a GitHub repo or Docker image",
	Run: func(cmd *cobra.Command, args []string) {

		// =========================
		// VALIDATION
		// =========================

		if repo == "" && image == "" {
			ui.Fatal("specify --repo or --image")
		}

		// =========================
		// CONFIG
		// =========================

		cfg, err := config.Load()
		if err != nil {
			ui.Fatal("config: %s", err)
		}

		ui.Section("deploy")

		// =========================
		// RESOLVE SOURCE
		// =========================

		isImage := image != "" && repo == ""

		var serviceName string
		var finalImage string

		// =========================
		// IMAGE MODE
		// =========================

		if isImage {
			ui.Info("mode image: %s", image)
			serviceName = docker.AdvancedNormalizeName(image)
			finalImage = image
		}

		// =========================
		// REPO MODE
		// =========================

		if repo != "" {
			if image == "" {
				image = git.ExtractRepoName(repo)
			}

			serviceName = docker.NormalizeName(image)
			finalImage = fmt.Sprintf("ghcr.io/%s/%s", cfg.GitHubUser, serviceName)

			ui.Info("mode repo: %s", repo)
			ui.Detail("image", finalImage)

			ui.Step("clone", "cloning %s", repo)
			dir, err := git.Clone(repo)
			if err != nil {
				ui.Fatal("git clone: %s", err)
			}

			ui.Step("build", "building linux/arm64 image")
			err = docker.BuildAndPush(finalImage, dir)
			if err != nil {
				ui.Fatal("build: %s", err)
			}
		}

		// =========================
		// NETWORK
		// =========================

		if network != "" && createNetwork {
			if !docker.NetworkExists(network) {
				ui.Step("network", "creating %s", network)
				_ = docker.CreateNetwork(network)
			}
		}

		// =========================
		// COMPOSE GENERATION
		// =========================

		ui.Step("compose", "generating docker-compose.yaml")
		err = compose.Generate(
			composePath,
			serviceName,
			finalImage,
			mode,
			domain,
			network,
			internalPort,
			ports,
			volumes,
		)
		if err != nil {
			ui.Fatal("compose: %s", err)
		}

		composeFilePath := filepath.Join(composePath, serviceName)
		ui.Detail("path", composeFilePath)

		if network != "" {
			ui.Detail("network", network)
		}
		if domain != "" {
			ui.Detail("domain", domain)
		}

		// =========================
		// DEPLOY
		// =========================

		ui.Step("up", "starting service")
		err = docker.ComposeUp(composeFilePath)
		if err != nil {
			ui.Fatal("deploy: %s", err)
		}

		// =========================
		// STATE
		// =========================

		st, err := state.Load()
		if err == nil {
			st.Apps[serviceName] = state.App{
				Repo:    repo,
				Image:   finalImage,
				Mode:    mode,
				Domain:  domain,
				Network: network,
				Path:    composeFilePath,
			}
			_ = state.Save(st)
		}

		ui.Ok("%s deployed", serviceName)
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringVar(&repo, "repo", "", "GitHub repo (user/repo)")
	deployCmd.Flags().StringVar(&image, "image", "", "Docker image (e.g. traefik/whoami)")

	deployCmd.Flags().StringSliceVar(&ports, "port", []string{}, "Port mappings")
	deployCmd.Flags().StringSliceVar(&volumes, "volume", []string{}, "Volume mounts")

	deployCmd.Flags().StringVar(&composePath, "compose-path", "/srv/apps", "Base path for compose files")
	deployCmd.Flags().StringVar(&mode, "mode", "local", "Deployment mode: local | traefik")
	deployCmd.Flags().StringVar(&domain, "domain", "", "Traefik domain")
	deployCmd.Flags().StringVar(&network, "network", "", "Docker network")
	deployCmd.Flags().BoolVar(&createNetwork, "create-network", false, "Create network if it does not exist")
	deployCmd.Flags().StringVar(&internalPort, "internal-port", "80", "Internal container port")
}
