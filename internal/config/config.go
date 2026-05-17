package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level Shipyard configuration.
// Stored at ~/.shipyard/config.yaml
type Config struct {
	// Default provider names used when --git-provider / --registry are not set.
	DefaultGitProvider string `yaml:"default_git_provider"`
	DefaultRegistry    string `yaml:"default_registry"`

	// GitProviders is the list of configured git providers.
	GitProviders []GitProvider `yaml:"git_providers"`

	// Registries is the list of configured container registries.
	Registries []Registry `yaml:"registries"`

	// Device holds SSH connection details for the target server.
	Device DeviceConfig `yaml:"device"`
}

type GitProvider struct {
	// Name is the identifier used with --git-provider.
	// Must be unique. Example: "github", "mygitea"
	Name string `yaml:"name"`

	// Type is the provider implementation to use.
	// Built-in values: "github", "gitlab", "gitea", "generic"
	Type string `yaml:"type"`

	// Host is required for self-hosted providers (gitea, gitlab self-managed).
	Host string `yaml:"host,omitempty"`

	// Protocol is "ssh" (default) or "https".
	Protocol string `yaml:"protocol,omitempty"`

	// SSHKey overrides the default SSH key for this provider.
	SSHKey string `yaml:"ssh_key,omitempty"`

	// Token is used for HTTPS authenticated clones.
	Token string `yaml:"token,omitempty"`

	// User is used for HTTPS authenticated clones.
	User string `yaml:"user,omitempty"`
}

type Registry struct {
	// Name is the identifier used with --registry.
	// Must be unique. Example: "ghcr", "myregistry"
	Name string `yaml:"name"`

	// Type is the registry implementation to use.
	// Built-in values: "ghcr", "dockerhub", "local", "generic"
	Type string `yaml:"type"`

	// Host is required for local/generic registries.
	Host string `yaml:"host,omitempty"`

	// User is the registry username.
	User string `yaml:"user,omitempty"`

	// Token is the registry password or access token.
	Token string `yaml:"token,omitempty"`

	// Insecure allows pushing to HTTP registries (no TLS).
	Insecure bool `yaml:"insecure,omitempty"`
}

type DeviceConfig struct {
	Host   string `yaml:"host"`
	User   string `yaml:"user"`
	SSHKey string `yaml:"ssh_key"`
}

// ---------------------------------------------------------------------------
// Load / Save
// ---------------------------------------------------------------------------

func path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".shipyard", "config.yaml")
}

func Load() (*Config, error) {
	data, err := os.ReadFile(path())
	if err != nil {
		return nil, fmt.Errorf("reading config: %w — run 'shipyard setup' first", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

func Save(cfg *Config) error {
	p := path()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(p, data, 0600)
}

// ---------------------------------------------------------------------------
// Lookup helpers
// ---------------------------------------------------------------------------

// GetGitProvider returns the GitProvider config for the given name.
func (c *Config) GetGitProvider(name string) (GitProvider, error) {
	for _, p := range c.GitProviders {
		if p.Name == name {
			return p, nil
		}
	}
	return GitProvider{}, fmt.Errorf("git provider %q not found in config", name)
}

// GetRegistry returns the Registry config for the given name.
func (c *Config) GetRegistry(name string) (Registry, error) {
	for _, r := range c.Registries {
		if r.Name == name {
			return r, nil
		}
	}
	return Registry{}, fmt.Errorf("registry %q not found in config", name)
}

// GitProviderName returns the effective git provider name,
// preferring the explicit value over the config default.
func (c *Config) GitProviderName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if c.DefaultGitProvider != "" {
		return c.DefaultGitProvider
	}
	return "github"
}

// RegistryName returns the effective registry name,
// preferring the explicit value over the config default.
func (c *Config) RegistryName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if c.DefaultRegistry != "" {
		return c.DefaultRegistry
	}
	return "ghcr"
}
