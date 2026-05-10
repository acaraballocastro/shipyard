package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Generate crea un docker-compose.yml según modo local o traefik
func Generate(
	basePath string,
	name string,
	image string,
	mode string,
	domain string,
	network string,
	ports []string,
	volumes []string,
) error {

	dir := filepath.Join(basePath, name)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	filePath := filepath.Join(dir, "docker-compose.yml")

	var b strings.Builder

	// =========================
	// HEADER
	// =========================

	b.WriteString(`version: "3.9"

services:
`)

	// =========================
	// SERVICE
	// =========================

	b.WriteString(fmt.Sprintf(`  %s:
    image: %s
    container_name: %s
    restart: unless-stopped
`, name, image, name))

	// =========================
	// NETWORKS (service level)
	// =========================

	if network != "" {
		b.WriteString(`    networks:
      - ` + network + `
`)
	}

	// =========================
	// PORTS (LOCAL MODE)
	// =========================

	if mode == "local" && len(ports) > 0 {
		b.WriteString(`    ports:
`)
		for _, p := range ports {
			b.WriteString("      - \"" + p + "\"\n")
		}
	}

	// =========================
	// VOLUMES
	// =========================

	if len(volumes) > 0 {
		b.WriteString(`    volumes:
`)
		for _, v := range volumes {
			b.WriteString("      - " + v + "\n")
		}
	}

	// =========================
	// TRAEFIK MODE
	// =========================

	if mode == "traefik" {

		if domain == "" {
			return fmt.Errorf("traefik mode requiere domain")
		}

		b.WriteString(`    labels:
`)
		b.WriteString("      - \"traefik.enable=true\"\n")
		b.WriteString(fmt.Sprintf("      - \"traefik.http.routers.%s.rule=Host(`%s`)\"\n", name, domain))
		b.WriteString(fmt.Sprintf("      - \"traefik.http.services.%s.loadbalancer.server.port=80\"\n", name))
	}

	// =========================
	// NETWORK DECLARATION
	// =========================

	if network != "" {
		b.WriteString(`
			networks:
			` + network + `:
				external: true
			`)
	}

	// =========================
	// WRITE FILE
	// =========================

	return os.WriteFile(filePath, []byte(b.String()), 0644)
}
