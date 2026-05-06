package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func GetConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".shipyard.yaml")
}

func Save(cfg *Config) error {
	path := GetConfigPath()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
