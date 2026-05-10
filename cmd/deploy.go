package cmd

import (
	"fmt"
	"path/filepath"

	"shipyard/internal/compose"
	"shipyard/internal/config"
	"shipyard/internal/docker"
	"shipyard/internal/git"

	"github.com/spf13/cobra"
)

var (
	repo       string
	image      string
	name       string
	ports      []string
	volumes    []string
	composeDir string
	mode       string
	domain     string
	network    string
	createNet  bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy desde repo GitHub o imagen Docker",
	Run: func(cmd *cobra.Command, args []string) {

		fmt.Println("🚀 Shipyard deploy iniciado")

		cfg, err := config.Load()
		if err != nil {
			fmt.Println("❌ config error:", err)
			return
		}

		// =====================================================
		// VALIDACIÓN DE APP NAME
		// =====================================================

		appName := name
		if appName == "" {
			if repo != "" {
				appName = git.ExtractRepoName(repo)
			} else if image != "" {
				appName = docker.NormalizeName(image)
			}
		}

		// =====================================================
		// MODE 1: IMAGE EXTERNA
		// =====================================================

		if image != "" && repo == "" {

			fmt.Println("📦 Modo image:", image)

			err = compose.Generate(
				composeDir,
				appName,
				image,
				mode,
				domain,
				network,
				ports,
				volumes,
			)

			if err != nil {
				fmt.Println("❌ compose error:", err)
				return
			}

			err = docker.ComposeUp(filepath.Join(composeDir, appName))
			if err != nil {
				fmt.Println("❌ deploy error:", err)
				return
			}

			fmt.Println("✅ Image deployed:", image)
			return
		}

		// =====================================================
		// MODE 2: REPO BUILD
		// =====================================================

		if repo == "" {
			fmt.Println("❌ Debes indicar --repo o --image")
			return
		}

		fmt.Println("📦 Repo:", repo)

		dir, err := git.Clone(repo)
		if err != nil {
			fmt.Println("❌ clone error:", err)
			return
		}

		fullImage := fmt.Sprintf("ghcr.io/%s/%s", cfg.GitHubUser, appName)

		fmt.Println("🐳 Image:", fullImage)

		err = docker.BuildAndPush(fullImage, dir)
		if err != nil {
			fmt.Println("❌ build error:", err)
			return
		}

		err = compose.Generate(
			composeDir,
			appName,
			fullImage,
			mode,
			domain,
			network,
			ports,
			volumes,
		)

		if err != nil {
			fmt.Println("❌ compose error:", err)
			return
		}

		err = docker.ComposeUp(filepath.Join(composeDir, appName))
		if err != nil {
			fmt.Println("❌ deploy error:", err)
			return
		}

		fmt.Println("✅ Deployed:", appName)
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringVar(&repo, "repo", "", "GitHub repo (user/repo)")
	deployCmd.Flags().StringVar(&image, "image", "", "Docker image externa (ej: traefik/whoami)")
	deployCmd.Flags().StringVar(&name, "name", "", "Nombre de la app (opcional)")

	deployCmd.Flags().StringSliceVar(&ports, "port", []string{}, "Puertos")
	deployCmd.Flags().StringSliceVar(&volumes, "volume", []string{}, "Volúmenes")

	deployCmd.Flags().StringVar(&composeDir, "compose-path", "/srv/apps", "Base path")
	deployCmd.Flags().StringVar(&mode, "mode", "local", "local | traefik")
	deployCmd.Flags().StringVar(&domain, "domain", "", "Traefik domain")
	deployCmd.Flags().StringVar(&network, "network", "", "Docker network")
	deployCmd.Flags().BoolVar(&createNet, "create-network", false, "Create network if missing")
}
