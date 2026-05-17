package git

import (
	"fmt"
	"os"

	"shipyard/internal/system"
)

// init registers all built-in providers.
func init() {
	Register("github", func(cfg ProviderConfig) Provider { return &githubProvider{cfg} })
	Register("gitlab", func(cfg ProviderConfig) Provider { return &gitlabProvider{cfg} })
	Register("gitea", func(cfg ProviderConfig) Provider { return &giteaProvider{cfg} })
	Register("generic", func(cfg ProviderConfig) Provider { return &genericProvider{cfg} })
}

// ---------------------------------------------------------------------------
// Shared clone helper
// ---------------------------------------------------------------------------

func cloneURL(url, dst string, sshKey string) error {
	os.RemoveAll(dst)

	args := []string{"clone"}

	if sshKey != "" {
		// Pass a custom identity file via GIT_SSH_COMMAND.
		// This avoids modifying ~/.ssh/config for the user.
		os.Setenv("GIT_SSH_COMMAND", fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no", sshKey))
	}

	args = append(args, url, dst)
	return system.RunCommand("git", args...)
}

// ---------------------------------------------------------------------------
// GitHub
// ---------------------------------------------------------------------------

type githubProvider struct{ cfg ProviderConfig }

func (p *githubProvider) ResolveURL(repo string) string {
	if p.cfg.Protocol == "https" {
		if p.cfg.Token != "" {
			return fmt.Sprintf("https://%s@github.com/%s.git", p.cfg.Token, repo)
		}
		return fmt.Sprintf("https://github.com/%s.git", repo)
	}
	return fmt.Sprintf("git@github.com:%s.git", repo)
}

func (p *githubProvider) Clone(repo, dst string) error {
	return cloneURL(p.ResolveURL(repo), dst, p.cfg.SSHKey)
}

// ---------------------------------------------------------------------------
// GitLab
// ---------------------------------------------------------------------------

type gitlabProvider struct{ cfg ProviderConfig }

func (p *gitlabProvider) host() string {
	if p.cfg.Host != "" {
		return p.cfg.Host
	}
	return "gitlab.com"
}

func (p *gitlabProvider) ResolveURL(repo string) string {
	if p.cfg.Protocol == "https" {
		if p.cfg.Token != "" {
			return fmt.Sprintf("https://oauth2:%s@%s/%s.git", p.cfg.Token, p.host(), repo)
		}
		return fmt.Sprintf("https://%s/%s.git", p.host(), repo)
	}
	return fmt.Sprintf("git@%s:%s.git", p.host(), repo)
}

func (p *gitlabProvider) Clone(repo, dst string) error {
	return cloneURL(p.ResolveURL(repo), dst, p.cfg.SSHKey)
}

// ---------------------------------------------------------------------------
// Gitea / Forgejo (self-hosted)
// ---------------------------------------------------------------------------

type giteaProvider struct{ cfg ProviderConfig }

func (p *giteaProvider) ResolveURL(repo string) string {
	host := p.cfg.Host
	if host == "" {
		host = "localhost:3000"
	}
	if p.cfg.Protocol == "https" {
		if p.cfg.Token != "" {
			return fmt.Sprintf("https://%s@%s/%s.git", p.cfg.Token, host, repo)
		}
		return fmt.Sprintf("https://%s/%s.git", host, repo)
	}
	return fmt.Sprintf("git@%s:%s.git", host, repo)
}

func (p *giteaProvider) Clone(repo, dst string) error {
	return cloneURL(p.ResolveURL(repo), dst, p.cfg.SSHKey)
}

// ---------------------------------------------------------------------------
// Generic — accepts a full URL directly as repo value
// ---------------------------------------------------------------------------

// genericProvider treats repo as a full clone URL.
// Use when none of the named providers fit.
// Example: --repo https://my.host/org/app.git --git-provider generic
type genericProvider struct{ cfg ProviderConfig }

func (p *genericProvider) ResolveURL(repo string) string {
	return repo
}

func (p *genericProvider) Clone(repo, dst string) error {
	return cloneURL(repo, dst, p.cfg.SSHKey)
}
