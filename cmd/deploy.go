package cmd

import (
	"fmt"

	"shipyard/internal/config"
	"shipyard/internal/docker"
	"shipyard/internal/git"

	"github.com/spf13/cobra"
)

var repo string
var image string
var tag string

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Build y push de una imagen desde un repo",
	Run: func(cmd *cobra.Command, args []string) {

		cfg, err := config.Load()
		if err != nil {
			fmt.Println("❌ Error cargando config:", err)
			return
		}

		if repo == "" {
			fmt.Println("❌ Debes indicar --repo")
			return
		}

		if image == "" {
			image = repo // default
		}

		fullImage := fmt.Sprintf("ghcr.io/%s/%s", cfg.GitHubUser, image)

		fmt.Println("🚀 Deploy iniciado")
		fmt.Println("Repo:", repo)
		fmt.Println("Imagen:", fullImage)

		// 1. Clone
		dir, err := git.Clone(repo)
		if err != nil {
			fmt.Println("❌ Error clonando:", err)
			return
		}

		// 2. Build
		err = docker.Build(fullImage, tag, dir)
		if err != nil {
			fmt.Println("❌ Error en build:", err)
			return
		}

		// 3. Login GHCR
		err = docker.Login(cfg.GitHubUser, cfg.GitHubToken)
		if err != nil {
			fmt.Println("❌ Error login GHCR:", err)
			return
		}

		// 4. Push
		err = docker.Push(fullImage, tag)
		if err != nil {
			fmt.Println("❌ Error push:", err)
			return
		}

		fmt.Println("✅ Imagen subida correctamente")
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringVar(&repo, "repo", "", "Repositorio GitHub (user/repo)")
	deployCmd.Flags().StringVar(&image, "image", "", "Nombre de imagen")
	deployCmd.Flags().StringVar(&tag, "tag", "latest", "Tag de la imagen")
}
