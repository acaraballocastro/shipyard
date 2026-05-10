package cmd

import (
	"fmt"
	"path/filepath"

	"shipyard/internal/compose"
	"shipyard/internal/config"
	"shipyard/internal/docker"
	"shipyard/internal/git"
	"shipyard/internal/state"

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
	Short: "Deploy desde repo o imagen Docker",
	Run: func(cmd *cobra.Command, args []string) {

		fmt.Println("🚀 Shipyard deploy iniciado")

		cfg, err := config.Load()
		if err != nil {
			fmt.Println("❌ config error:", err)
			return
		}

		// =====================================================
		// VALIDACIÓN DE MODO
		// =====================================================

		if repo == "" && image == "" {
			fmt.Println("❌ Debes indicar --repo o --image")
			return
		}

		// =====================================================
		// MODE 2: IMAGE PÚBLICA (SIN BUILD)
		// =====================================================

		if image != "" && repo == "" {

			fmt.Println("📦 Modo image:", image)

			serviceName := docker.NormalizeName(image)

			err = compose.Generate(
				composePath,
				serviceName,
				image,
				mode,
				domain,
				network,
				internalPort,
				ports,
				volumes,
			)

			if err != nil {
				fmt.Println("❌ compose error:", err)
				return
			}

			composeFilePath := filepath.Join(composePath, serviceName)

			err = docker.ComposeUp(composeFilePath)
			if err != nil {
				fmt.Println("❌ deploy error:", err)
				return
			}

			fmt.Println("✅ Imagen pública desplegada:", image)
			return
		}

		// =====================================================
		// MODE 1: REPO BUILD + GHCR
		// =====================================================

		if image == "" {
			image = git.ExtractRepoName(repo)
		}

		image = docker.NormalizeName(image)

		fullImage := fmt.Sprintf("ghcr.io/%s/%s", cfg.GitHubUser, image)

		fmt.Println("📦 Repo:", repo)
		fmt.Println("🐳 Image:", fullImage)

		dir, err := git.Clone(repo)
		if err != nil {
			fmt.Println("❌ git clone error:", err)
			return
		}

		err = docker.BuildAndPush(fullImage, dir)
		if err != nil {
			fmt.Println("❌ build error:", err)
			return
		}

		// network
		if network != "" && createNetwork {
			if !docker.NetworkExists(network) {
				fmt.Println("🌐 creando network:", network)
				_ = docker.CreateNetwork(network)
			}
		}

		// compose
		serviceName := image

		err = compose.Generate(
			composePath,
			serviceName,
			fullImage,
			mode,
			domain,
			network,
			internalPort,
			ports,
			volumes,
		)

		if err != nil {
			fmt.Println("❌ compose error:", err)
			return
		}

		composeFilePath := filepath.Join(composePath, serviceName)

		err = docker.ComposeUp(composeFilePath)
		if err != nil {
			fmt.Println("❌ deploy error:", err)
			return
		}

		// state
		st, err := state.Load()
		if err == nil {
			st.Apps[serviceName] = state.App{
				Repo:    repo,
				Image:   fullImage,
				Mode:    mode,
				Domain:  domain,
				Network: network,
				Path:    composeFilePath,
			}
			_ = state.Save(st)
		}

		fmt.Println("✅ Servicio desplegado correctamente")
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringVar(&repo, "repo", "", "GitHub repo (user/repo)")
	deployCmd.Flags().StringVar(&image, "image", "", "Docker image (ej: traefik/whoami)")

	deployCmd.Flags().StringSliceVar(&ports, "port", []string{}, "Puertos")
	deployCmd.Flags().StringSliceVar(&volumes, "volume", []string{}, "Volúmenes")

	deployCmd.Flags().StringVar(&composePath, "compose-path", "/srv/apps", "Base path")
	deployCmd.Flags().StringVar(&mode, "mode", "local", "local | traefik")
	deployCmd.Flags().StringVar(&domain, "domain", "", "Traefik domain")
	deployCmd.Flags().StringVar(&network, "network", "", "Docker network")
	deployCmd.Flags().BoolVar(&createNetwork, "create-network", false, "Create network if missing")
	deployCmd.Flags().StringVar(&internalPort, "internal-port", "80", "Internal container port")
}
