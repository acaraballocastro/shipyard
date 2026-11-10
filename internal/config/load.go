package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

func Load() (*Config, error) {
	path := GetConfigPath()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

// LoadOrEmpty is like Load but returns an empty config when the file does
// not exist yet.
func LoadOrEmpty() (*Config, error) {
	cfg, err := Load()
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	return cfg, err
}
