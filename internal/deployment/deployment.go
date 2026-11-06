// Package deployment models the persistent, non-secret configuration of a
// deployed service (deployment.yaml) and turns it into a Compose spec.
package deployment

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"shipyard/internal/compose"
	"shipyard/internal/envfile"
)

const (
	// FileName is the per-service configuration file.
	FileName = "deployment.yaml"
	// ComposeFileName is the generated Compose file.
	ComposeFileName = "docker-compose.yml"
	// LocalEnvFileName holds variables passed with --env (0600).
	LocalEnvFileName = ".env"

	CurrentVersion = 1

	SourceGit   = "git"
	SourceImage = "image"

	ModeLocal   = "local"
	ModeTraefik = "traefik"

	DefaultPlatform     = "linux/arm64"
	DefaultEntrypoint   = "web"
	DefaultNetwork      = "edge"
	DefaultInternalPort = "80"
)

// Config is the content of deployment.yaml. It must never hold secrets:
// env files are stored as paths and --env values live in the 0600 .env file.
type Config struct {
	Version     int         `yaml:"version"`
	Name        string      `yaml:"name"`
	Source      Source      `yaml:"source"`
	Image       string      `yaml:"image"`
	Build       Build       `yaml:"build,omitempty"`
	Deploy      Deploy      `yaml:"deploy"`
	Env         Env         `yaml:"env,omitempty"`
	Healthcheck Healthcheck `yaml:"healthcheck"`
}

type Source struct {
	Type       string `yaml:"type"`
	Repository string `yaml:"repository,omitempty"`
}

type Build struct {
	Platform string `yaml:"platform,omitempty"`
	// Args are non-secret build arguments persisted for future updates.
	Args map[string]string `yaml:"args,omitempty"`
	// ArgsFromEnv are build argument names whose values are read from the
	// environment of the shell running shipyard at build time. Only the
	// names are stored.
	ArgsFromEnv []string `yaml:"args_from_env,omitempty"`
}

type Deploy struct {
	Mode         string   `yaml:"mode"`
	Domain       string   `yaml:"domain,omitempty"`
	Network      string   `yaml:"network,omitempty"`
	InternalPort string   `yaml:"internal_port,omitempty"`
	Entrypoint   string   `yaml:"entrypoint,omitempty"`
	TLS          bool     `yaml:"tls,omitempty"`
	CertResolver string   `yaml:"cert_resolver,omitempty"`
	RouterName   string   `yaml:"router_name,omitempty"`
	Middlewares  []string `yaml:"middlewares,omitempty"`
	Ports        []string `yaml:"ports,omitempty"`
	Volumes      []string `yaml:"volumes,omitempty"`
}

type Env struct {
	// Files are absolute paths to env files referenced by Compose.
	Files []string `yaml:"files,omitempty"`
	// Keys lists the variable names stored in the local .env file.
	// Values are intentionally not recorded here.
	Keys []string `yaml:"keys,omitempty"`
}

type Healthcheck struct {
	Enabled     bool   `yaml:"enabled"`
	Command     string `yaml:"command,omitempty"`
	Interval    string `yaml:"interval,omitempty"`
	Timeout     string `yaml:"timeout,omitempty"`
	Retries     int    `yaml:"retries,omitempty"`
	StartPeriod string `yaml:"start_period,omitempty"`
}

var (
	namePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,62}$`)
	repoPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]+$`)
	platformPattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9_]+(/v[0-9]+)?$`)
	routerPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	middlewarePat   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(@[A-Za-z0-9_-]+)?$`)
	domainPattern   = regexp.MustCompile(`^(\*\.)?([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)*[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	networkPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	imagePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/@+-]*$`)
	entryPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
)

// ValidName reports whether s is a safe service name (used as a directory
// and container name).
func ValidName(s string) bool { return namePattern.MatchString(s) }

// ValidateRepo checks the owner/name repository syntax.
func ValidateRepo(s string) error {
	if !repoPattern.MatchString(s) || strings.Contains(s, "..") {
		return fmt.Errorf("invalid repository %q: expected owner/name", s)
	}
	return nil
}

// ValidatePlatform checks a single or comma-separated list of platforms
// (os/arch[/variant]), e.g. linux/arm64, linux/amd64, linux/arm/v7.
func ValidatePlatform(s string) error {
	if s == "" {
		return fmt.Errorf("platform must not be empty")
	}
	for _, p := range strings.Split(s, ",") {
		if !platformPattern.MatchString(strings.TrimSpace(p)) {
			return fmt.Errorf("invalid platform %q: expected os/arch[/variant], e.g. linux/arm64", p)
		}
	}
	return nil
}

// ValidateDuration checks a Compose duration such as 30s, 1m30s.
func ValidateDuration(field, s string) error {
	if s == "" {
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("%s: invalid duration %q (use e.g. 30s, 1m)", field, s)
	}
	if d < 0 {
		return fmt.Errorf("%s: duration must not be negative", field)
	}
	return nil
}

// Normalize fills defaults so older or hand-edited files behave like the
// CLI defaults.
func (c *Config) Normalize() {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	if c.Deploy.Mode == "" {
		c.Deploy.Mode = ModeLocal
	}
	if c.Source.Type == SourceGit && c.Build.Platform == "" {
		c.Build.Platform = DefaultPlatform
	}
	if c.Deploy.Mode == ModeTraefik {
		if c.Deploy.Network == "" {
			c.Deploy.Network = DefaultNetwork
		}
		if c.Deploy.Entrypoint == "" {
			c.Deploy.Entrypoint = DefaultEntrypoint
		}
		if c.Deploy.InternalPort == "" {
			c.Deploy.InternalPort = DefaultInternalPort
		}
	}
	sort.Strings(c.Env.Keys)
}

// Validate checks every field. It does not touch the filesystem except to
// check env files when checkFiles is true.
func (c *Config) Validate(checkFiles bool) error {
	if c.Version > CurrentVersion {
		return fmt.Errorf("deployment.yaml version %d is newer than this shipyard supports (%d)", c.Version, CurrentVersion)
	}
	if !ValidName(c.Name) {
		return fmt.Errorf("invalid service name %q: use lowercase letters, digits, '-', '_' or '.'", c.Name)
	}
	if c.Image == "" || !imagePattern.MatchString(c.Image) {
		return fmt.Errorf("invalid image %q", c.Image)
	}

	switch c.Source.Type {
	case SourceGit:
		if err := ValidateRepo(c.Source.Repository); err != nil {
			return err
		}
		if err := ValidatePlatform(c.Build.Platform); err != nil {
			return err
		}
	case SourceImage:
		if len(c.Build.Args) > 0 || len(c.Build.ArgsFromEnv) > 0 {
			return fmt.Errorf("build args are only supported for git deployments")
		}
	default:
		return fmt.Errorf("invalid source type %q: expected git or image", c.Source.Type)
	}

	for k := range c.Build.Args {
		if !envfile.ValidKey(k) {
			return fmt.Errorf("invalid build arg name %q", k)
		}
		if strings.ContainsAny(c.Build.Args[k], "\n\r\x00") {
			return fmt.Errorf("build arg %s: value must not contain newlines", k)
		}
	}
	for _, k := range c.Build.ArgsFromEnv {
		if !envfile.ValidKey(k) {
			return fmt.Errorf("invalid build arg name %q", k)
		}
		if _, dup := c.Build.Args[k]; dup {
			return fmt.Errorf("build arg %s is defined both with and without a value", k)
		}
	}

	d := c.Deploy
	switch d.Mode {
	case ModeLocal:
	case ModeTraefik:
		if d.Domain == "" {
			return fmt.Errorf("traefik mode requires --domain")
		}
		if !domainPattern.MatchString(d.Domain) {
			return fmt.Errorf("invalid domain %q", d.Domain)
		}
		if !entryPattern.MatchString(d.Entrypoint) {
			return fmt.Errorf("invalid entrypoint %q", d.Entrypoint)
		}
		if d.RouterName != "" && !routerPattern.MatchString(d.RouterName) {
			return fmt.Errorf("invalid router name %q", d.RouterName)
		}
		if d.CertResolver != "" {
			if !routerPattern.MatchString(d.CertResolver) {
				return fmt.Errorf("invalid cert resolver %q", d.CertResolver)
			}
			if !d.TLS {
				return fmt.Errorf("--cert-resolver requires --tls")
			}
		}
		for _, m := range d.Middlewares {
			if !middlewarePat.MatchString(m) {
				return fmt.Errorf("invalid middleware %q", m)
			}
		}
	default:
		return fmt.Errorf("invalid mode %q: expected local or traefik", d.Mode)
	}
	if d.Network != "" && !networkPattern.MatchString(d.Network) {
		return fmt.Errorf("invalid network %q", d.Network)
	}
	if d.InternalPort != "" {
		p, err := strconv.Atoi(d.InternalPort)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid internal port %q", d.InternalPort)
		}
	}
	for _, p := range d.Ports {
		if p == "" || strings.ContainsAny(p, " \n") {
			return fmt.Errorf("invalid port mapping %q", p)
		}
	}
	for _, v := range d.Volumes {
		if v == "" || strings.ContainsAny(v, "\n") {
			return fmt.Errorf("invalid volume %q", v)
		}
	}

	for _, f := range c.Env.Files {
		if !filepath.IsAbs(f) {
			return fmt.Errorf("env file %q must be an absolute path", f)
		}
		if checkFiles {
			if err := envfile.Validate(f); err != nil {
				return err
			}
		}
	}
	for _, k := range c.Env.Keys {
		if !envfile.ValidKey(k) {
			return fmt.Errorf("invalid env key %q", k)
		}
	}

	h := c.Healthcheck
	if h.Enabled {
		if strings.TrimSpace(h.Command) == "" {
			return fmt.Errorf("healthcheck enabled but no command set (--healthcheck-cmd)")
		}
		for field, v := range map[string]string{
			"healthcheck interval":     h.Interval,
			"healthcheck timeout":      h.Timeout,
			"healthcheck start period": h.StartPeriod,
		} {
			if err := ValidateDuration(field, v); err != nil {
				return err
			}
		}
		if h.Retries < 0 {
			return fmt.Errorf("healthcheck retries must be >= 0")
		}
	}
	return nil
}

// Router returns the Traefik router/service name.
func (c *Config) Router() string {
	if c.Deploy.RouterName != "" {
		return c.Deploy.RouterName
	}
	return c.Name
}

// ComposeSpec converts the configuration into a compose spec. dir is the
// deployment directory; the local .env file is referenced when it has keys.
func (c *Config) ComposeSpec(dir string) compose.Spec {
	envFiles := append([]string{}, c.Env.Files...)
	if len(c.Env.Keys) > 0 {
		// Listed last so explicit --env values override env files.
		envFiles = append(envFiles, filepath.Join(dir, LocalEnvFileName))
	}

	spec := compose.Spec{
		ServiceName: c.Name,
		Image:       c.Image,
		Mode:        c.Deploy.Mode,
		Network:     c.Deploy.Network,
		Ports:       c.Deploy.Ports,
		Volumes:     c.Deploy.Volumes,
		EnvFiles:    envFiles,
		Traefik: compose.TraefikOptions{
			Router:       c.Router(),
			Domain:       c.Deploy.Domain,
			InternalPort: c.Deploy.InternalPort,
			Entrypoint:   c.Deploy.Entrypoint,
			TLS:          c.Deploy.TLS,
			CertResolver: c.Deploy.CertResolver,
			Middlewares:  c.Deploy.Middlewares,
		},
	}
	if c.Healthcheck.Enabled {
		h := c.Healthcheck
		spec.Healthcheck = &compose.Healthcheck{
			Test:        []string{"CMD-SHELL", h.Command},
			Interval:    h.Interval,
			Timeout:     h.Timeout,
			Retries:     h.Retries,
			StartPeriod: h.StartPeriod,
		}
	}
	return spec
}
