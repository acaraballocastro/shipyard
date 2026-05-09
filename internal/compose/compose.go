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

	service := Service{
		Image:         image,
		ContainerName: serviceName,
		Restart:       "unless-stopped",
	}

	compose := Compose{
		Version: "3.9",
		Services: map[string]Service{
			serviceName: service,
		},
	}

	// =========================
	// MODE: TRAEFIK
	// =========================
	if mode == "traefik" {

		if network != "" {
			service.Networks = []string{network}
		} else {
			service.Networks = []string{"edge"}
		}

		service.Labels = BuildTraefikLabels(serviceName, domain, internalPort)
		compose.Services[serviceName] = service

		compose.Networks = map[string]Network{
			"edge": {
				External: true,
			},
		}
	}

	// =========================
	// MODE: LOCAL PORTS
	// =========================
	if mode == "local" {
		service.Ports = ports
		service.Volumes = volumes
		compose.Services[serviceName] = service
	}

	data, err := yaml.Marshal(compose)
	if err != nil {
		return err
	}

	file := filepath.Join(dir, "docker-compose.yml")
	return os.WriteFile(file, data, 0644)
}
