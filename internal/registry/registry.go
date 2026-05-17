package registry

import (
	"fmt"

	"shipyard/internal/system"
)

// init registers all built-in providers.
func init() {
	Register("ghcr", func(cfg ProviderConfig) Provider { return &ghcrProvider{cfg} })
	Register("dockerhub", func(cfg ProviderConfig) Provider { return &dockerhubProvider{cfg} })
	Register("local", func(cfg ProviderConfig) Provider { return &localProvider{cfg} })
	Register("generic", func(cfg ProviderConfig) Provider { return &genericProvider{cfg} })
}

// ---------------------------------------------------------------------------
// GitHub Container Registry (ghcr.io)
// ---------------------------------------------------------------------------

type ghcrProvider struct{ cfg ProviderConfig }

func (p *ghcrProvider) Login() error {
	cmd := fmt.Sprintf(
		"echo %s | docker login ghcr.io -u %s --password-stdin",
		p.cfg.Token, p.cfg.User,
	)
	return system.RunCommand("sh", "-c", cmd)
}

func (p *ghcrProvider) ImageName(user, service string) string {
	return fmt.Sprintf("ghcr.io/%s/%s", user, service)
}

func (p *ghcrProvider) Push(image string) error {
	return system.RunCommand("docker", "push", image+":latest")
}

// ---------------------------------------------------------------------------
// Docker Hub (docker.io)
// ---------------------------------------------------------------------------

type dockerhubProvider struct{ cfg ProviderConfig }

func (p *dockerhubProvider) Login() error {
	cmd := fmt.Sprintf(
		"echo %s | docker login -u %s --password-stdin",
		p.cfg.Token, p.cfg.User,
	)
	return system.RunCommand("sh", "-c", cmd)
}

func (p *dockerhubProvider) ImageName(user, service string) string {
	// Docker Hub format: user/service (no host prefix)
	return fmt.Sprintf("%s/%s", user, service)
}

func (p *dockerhubProvider) Push(image string) error {
	return system.RunCommand("docker", "push", image+":latest")
}

// ---------------------------------------------------------------------------
// Local registry (no auth, plain HTTP)
// ---------------------------------------------------------------------------

// localProvider targets a self-hosted registry with no authentication,
// typically running on the same LAN (e.g. registry:5000 on a Raspberry Pi).
type localProvider struct{ cfg ProviderConfig }

func (p *localProvider) Login() error {
	// No authentication required for local registries.
	return nil
}

func (p *localProvider) host() string {
	if p.cfg.Host != "" {
		return p.cfg.Host
	}
	return "localhost:5000"
}

func (p *localProvider) ImageName(_, service string) string {
	// Local registries don't use a user namespace.
	return fmt.Sprintf("%s/%s", p.host(), service)
}

func (p *localProvider) Push(image string) error {
	args := []string{"push"}
	if p.cfg.Insecure {
		// Insecure flag is set via daemon config, but we tag the intent here
		// for documentation purposes. Docker itself handles --insecure-registry
		// via /etc/docker/daemon.json, not CLI.
		args = append(args, image+":latest")
	} else {
		args = append(args, image+":latest")
	}
	return system.RunCommand("docker", args...)
}

// ---------------------------------------------------------------------------
// Generic OCI-compatible registry (authenticated, with custom host)
// ---------------------------------------------------------------------------

type genericProvider struct{ cfg ProviderConfig }

func (p *genericProvider) Login() error {
	if p.cfg.Token == "" {
		return nil
	}
	cmd := fmt.Sprintf(
		"echo %s | docker login %s -u %s --password-stdin",
		p.cfg.Token, p.cfg.Host, p.cfg.User,
	)
	return system.RunCommand("sh", "-c", cmd)
}

func (p *genericProvider) ImageName(_, service string) string {
	return fmt.Sprintf("%s/%s", p.cfg.Host, service)
}

func (p *genericProvider) Push(image string) error {
	return system.RunCommand("docker", "push", image+":latest")
}
