package compose

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Compose struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
}

type Service struct {
	Image         string   `yaml:"image"`
	ContainerName string   `yaml:"container_name"`
	Restart       string   `yaml:"restart"`
	Ports         []string `yaml:"ports,omitempty"`
	Volumes       []string `yaml:"volumes,omitempty"`
}

func Generate(path string, serviceName string, image string, ports []string, volumes []string) error {

	dir := filepath.Join(path, serviceName)

	// crear carpeta
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	compose := Compose{
		Version: "3.8",
		Services: map[string]Service{
			serviceName: {
				Image:         image,
				ContainerName: serviceName,
				Restart:       "unless-stopped",
				Ports:         ports,
				Volumes:       volumes,
			},
		},
	}

	data, err := yaml.Marshal(compose)
	if err != nil {
		return err
	}

	file := filepath.Join(dir, "docker-compose.yml")

	return os.WriteFile(file, data, 0644)
}
