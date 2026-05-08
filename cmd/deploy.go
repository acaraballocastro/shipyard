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

var repo string
var image string
var ports []string
var volumes []string
var composePath string
var mode string
var domain string
var network string
var createNetwork bool

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Build y push de imagen a GHCR usando buildx",
	Run: func(cmd *cobra.Command, args []string) {

		fmt.Println("🚀 Shipyard deploy iniciado")

		// 1. Cargar configuración
		cfg, err := config.Load()
		if err != nil {
			fmt.Println("❌ Error cargando config:", err)
			return
		}

		// 2. Validación básica
		if repo == "" {
			fmt.Println("❌ Debes indicar --repo user/repo")
			return
		}

		// 3. Derivar nombre de imagen si no se especifica
		if image == "" {
			image = git.ExtractRepoName(repo)
		}

		// 4. Normalizar nombre (Docker + GHCR rules)
		image = docker.NormalizeName(image)

		// 5. Construir nombre final GHCR
		fullImage := fmt.Sprintf("ghcr.io/%s/%s", cfg.GitHubUser, image)

		fmt.Println("📦 Repo:", repo)
		fmt.Println("🐳 Imagen:", fullImage)

		// 6. Clonar repo
		dir, err := git.Clone(repo)
		if err != nil {
			fmt.Println("❌ Error clonando repo:", err)
			return
		}

		// 7. Buildx + push directo a GHCR (ARM64)
		err = docker.BuildAndPush(fullImage, dir)
		if err != nil {
			fmt.Println("❌ Error en buildx:", err)
			return
		}

		fmt.Println("✅ Imagen publicada correctamente en GHCR")

		if network != "" && createNetwork {
			exists := docker.NetworkExists(network)

			if !exists {
				fmt.Println("🌐 Creando red Docker:", network)

				if err := docker.CreateNetwork(network); err != nil {
					fmt.Println("❌ Error creando red:", err)
					return
				}
			}
		}

		if mode == "traefik" && len(ports) > 0 {
			fmt.Println("⚠️ Traefik mode no necesita ports publicados")
		}

		// 8. Generar docker-compose
		serviceName := image

		err = compose.Generate(
			composePath,
			serviceName,
			fullImage,
			mode,
			domain,
			network,
			ports,
			volumes,
		)
		if err != nil {
			fmt.Println("❌ Error generando compose:", err)
			return
		}

		// 9. Ejecutar docker compose
		composeFilePath := filepath.Join(composePath, serviceName)
		err = docker.ComposeUp(composeFilePath)
		if err != nil {
			fmt.Println("❌ Error en docker compose:", err)
			return
		}

		fmt.Println("✅ Servicio desplegado correctamente")

	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringVar(&repo, "repo", "", "Repositorio GitHub (user/repo)")
	deployCmd.Flags().StringVar(&image, "image", "", "Nombre de imagen (opcional)")
	deployCmd.Flags().StringSliceVar(&ports, "port", []string{}, "Puertos (host:container)")
	deployCmd.Flags().StringSliceVar(&volumes, "volume", []string{}, "Volúmenes (host:container)")
	deployCmd.Flags().StringVar(&composePath, "compose-path", "/srv/apps", "Ruta base para docker-compose")
	deployCmd.Flags().StringVar(&mode, "mode", "local", "Modo: local | traefik")
	deployCmd.Flags().StringVar(&domain, "domain", "", "Dominio para Traefik (ej: app.midominio.com)")
	deployCmd.Flags().StringVar(&network, "network", "", "Red Docker a usar (opcional)")
	deployCmd.Flags().BoolVar(&createNetwork, "create-network", false, "Crear red si no existe")
}
