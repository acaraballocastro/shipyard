package registry

import "fmt"

// Provider abstracts a container image registry.
// Implement this interface to add a new registry.
type Provider interface {
	// Login authenticates against the registry.
	// Should be idempotent — safe to call before every push.
	Login() error

	// ImageName returns the full image reference for a given user and service.
	// Example (ghcr): "ghcr.io/myuser/my-app"
	// Example (dockerhub): "myuser/my-app"
	// Example (local): "localhost:5000/my-app"
	ImageName(user, service string) string

	// Push pushes the local image (already built and tagged) to the registry.
	Push(image string) error
}

// registry holds the registered providers by name.
var registry = map[string]func(cfg ProviderConfig) Provider{}

// Register makes a provider available under name.
func Register(name string, factory func(cfg ProviderConfig) Provider) {
	registry[name] = factory
}

// Get returns the Provider registered under name, configured with cfg.
func Get(name string, cfg ProviderConfig) (Provider, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown registry %q — available: %v", name, Available())
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

// ProviderConfig carries the configuration for a registry provider.
type ProviderConfig struct {
	// Host is the registry hostname, used by local/generic providers.
	// Example: "registry.homelab.local:5000"
	Host string

	// User is the registry username.
	User string

	// Token is the registry password or access token.
	Token string

	// Insecure allows pushing to registries without TLS.
	// Passes --insecure to docker where supported.
	Insecure bool
}
