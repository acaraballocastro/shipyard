package git

import "fmt"

// Provider abstracts a git hosting service.
// Implement this interface to add a new git provider.
type Provider interface {
	// Clone clones repo into dst and returns the local path.
	// repo is the raw value from --repo (e.g. "user/my-app").
	Clone(repo, dst string) error

	// ResolveURL returns the full clone URL for a given repo slug.
	ResolveURL(repo string) string
}

// registry holds the registered providers by name.
var registry = map[string]func(cfg ProviderConfig) Provider{}

// Register makes a provider available under name.
// Call this from each provider's init() or from providers.go.
func Register(name string, factory func(cfg ProviderConfig) Provider) {
	registry[name] = factory
}

// Get returns the Provider registered under name, configured with cfg.
// Returns an error if name is unknown.
func Get(name string, cfg ProviderConfig) (Provider, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown git provider %q — available: %v", name, Available())
	}
	return factory(cfg), nil
}

// Available returns the names of all registered providers.
func Available() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}

// ProviderConfig carries the configuration for a git provider.
// Not all fields are used by every provider.
type ProviderConfig struct {
	// Host is used by self-hosted providers (Gitea, GitLab self-managed).
	// Example: "git.homelab.local"
	Host string

	// SSHKey is the path to the private key used for SSH clones.
	// Defaults to the system default (~/.ssh/id_rsa) when empty.
	SSHKey string

	// Protocol is "ssh" or "https". Defaults to "ssh".
	Protocol string

	// Token is used for HTTPS clones that require authentication.
	Token string

	// User is the username for HTTPS authenticated clones.
	User string
}
