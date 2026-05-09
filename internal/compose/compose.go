package compose

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Compose struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
	Networks map[string]Network `yaml:"networks,omitempty"`
}

type Network struct {
	External bool `yaml:"external"`
}

type Service struct {
	Image         string   `yaml:"image"`
	ContainerName string   `yaml:"container_name"`
	Restart       string   `yaml:"restart"`
	Ports         []string `yaml:"ports,omitempty"`
	Volumes       []string `yaml:"volumes,omitempty"`
	Networks      []string `yaml:"networks,omitempty"`
	Labels        []string `yaml:"labels,omitempty"`
}

func Generate(
	path string,
	serviceName string,
	image string,
	mode string,
	domain string,
	network string,
	internalPort string,
	ports []string,
	volumes []string,
) error {

	dir := filepath.Join(path, serviceName)
	os.MkdirAll(dir, 0755)

	// =========================
	// 1. BUILD SERVICE FIRST
	// =========================
	service := Service{
		Image:         image,
		ContainerName: serviceName,
		Restart:       "unless-stopped",
	}

	// =========================
	// 2. MODE LOGIC
	// =========================
	switch mode {

	case "traefik":

		effectiveNetwork := network
		if effectiveNetwork == "" {
			effectiveNetwork = "edge"
		}

		service.Networks = []string{effectiveNetwork}
		service.Labels = BuildTraefikLabels(serviceName, domain, internalPort)

	case "local":

		service.Ports = ports
		service.Volumes = volumes

		if network != "" {
			service.Networks = []string{network}
		}
	}

	// =========================
	// 3. BUILD COMPOSE (ONCE)
	// =========================
	compose := Compose{
		Version: "3.9",
		Services: map[string]Service{
			serviceName: service,
		},
	}

	// =========================
	// 4. NETWORKS (GLOBAL)
	// =========================
	if mode == "traefik" {

		effectiveNetwork := network
		if effectiveNetwork == "" {
			effectiveNetwork = "edge"
		}

		compose.Networks = map[string]Network{
			effectiveNetwork: {
				External: true,
			},
		}
	}

	// =========================
	// 5. WRITE FILE
	// =========================
	data, err := yaml.Marshal(compose)
	if err != nil {
		return err
	}

	file := filepath.Join(dir, "docker-compose.yml")
	return os.WriteFile(file, data, 0644)
}
