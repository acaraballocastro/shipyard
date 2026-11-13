package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"shipyard/internal/config"
	"shipyard/internal/deployment"
	"shipyard/internal/docker"
	"shipyard/internal/envfile"
	"shipyard/internal/events"
	"shipyard/internal/git"
	"shipyard/internal/state"
	"shipyard/internal/update"
)

// DeployRequest describes a deployment. Pointer (and nil slice-pointer)
// fields are optional: nil keeps the saved value of an existing service,
// or the default for a new one. This is what lets a redeploy change only
// what was asked, from the CLI or the API alike.
type DeployRequest struct {
	// Repo (owner/name) is cloned and built on this host. Image alone is
	// an existing image to run; with Repo it names the built image.
	Repo  string `json:"repo,omitempty"`
	Image string `json:"image,omitempty"`
	// Name defaults to one derived from Repo or Image.
	Name string `json:"name,omitempty"`
	// Root is the base directory for services (default /srv/apps).
	Root string `json:"root,omitempty"`

	Mode          *string   `json:"mode,omitempty"`
	Domain        *string   `json:"domain,omitempty"`
	Network       *string   `json:"network,omitempty"`
	CreateNetwork bool      `json:"createNetwork,omitempty"`
	InternalPort  *string   `json:"internalPort,omitempty"`
	Ports         *[]string `json:"ports,omitempty"`
	Volumes       *[]string `json:"volumes,omitempty"`

	// EnvFiles replaces the env file list (paths on this host).
	EnvFiles *[]string `json:"envFiles,omitempty"`
	// Env is merged into the service .env (0600); never stored in yaml.
	Env map[string]string `json:"env,omitempty"`

	// BuildArgs are KEY=VALUE, or KEY to read from the environment.
	BuildArgs *[]string `json:"buildArgs,omitempty"`
	Platform  *string   `json:"platform,omitempty"`

	Entrypoint   *string   `json:"entrypoint,omitempty"`
	TLS          *bool     `json:"tls,omitempty"`
	CertResolver *string   `json:"certResolver,omitempty"`
	RouterName   *string   `json:"routerName,omitempty"`
	Middlewares  *[]string `json:"middlewares,omitempty"`

	HealthcheckCmd         *string `json:"healthcheckCmd,omitempty"`
	HealthcheckInterval    *string `json:"healthcheckInterval,omitempty"`
	HealthcheckTimeout     *string `json:"healthcheckTimeout,omitempty"`
	HealthcheckRetries     *int    `json:"healthcheckRetries,omitempty"`
	HealthcheckStartPeriod *string `json:"healthcheckStartPeriod,omitempty"`
	NoHealthcheck          bool    `json:"noHealthcheck,omitempty"`
}

// DeployResult summarizes a successful deployment.
type DeployResult struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Path     string `json:"path"`
	Health   string `json:"health,omitempty"`
	Existing bool   `json:"existing"`
}

// Deploy creates or redeploys a service.
func (s *Service) Deploy(ctx context.Context, req DeployRequest, p events.Progress) (*DeployResult, error) {

	// =========================
	// RESOLVE SOURCE + NAME
	// =========================

	if req.Repo == "" && req.Image == "" {
		return nil, errors.New("specify a repository or an image")
	}
	isImage := req.Repo == ""

	name := DeployName(req)
	image := req.Image
	if isImage {
		p.Info("mode image: %s", req.Image)
	} else {
		if err := deployment.ValidateRepo(req.Repo); err != nil {
			return nil, err
		}
		cfg, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("config (run shipyard setup): %w", err)
		}
		if cfg.GitHubUser == "" {
			return nil, errors.New("github_user is not set (run shipyard setup)")
		}
		// The built image is named after Image when given, else the repo.
		imgName := req.Image
		if imgName == "" {
			imgName = git.ExtractRepoName(req.Repo)
		}
		image = fmt.Sprintf("ghcr.io/%s/%s", strings.ToLower(cfg.GitHubUser), docker.NormalizeName(imgName))
		p.Info("mode repo: %s", req.Repo)
	}
	if !deployment.ValidName(name) {
		return nil, fmt.Errorf("invalid service name %q (choose one explicitly)", name)
	}

	root := req.Root
	if root == "" {
		root = DefaultRoot
	}
	dir := filepath.Join(root, name)

	// =========================
	// BUILD CONFIGURATION
	// =========================

	cfg, existing, err := loadExisting(name, dir)
	if err != nil {
		return nil, err
	}
	if existing {
		p.Info("existing deployment found, unspecified settings are preserved")
	}
	if err := req.apply(cfg, existing, name, image, isImage); err != nil {
		return nil, err
	}

	p.Detail("image", cfg.Image)
	p.Detail("path", dir)
	if cfg.Deploy.Network != "" {
		p.Detail("network", cfg.Deploy.Network)
	}
	if cfg.Deploy.Domain != "" {
		p.Detail("domain", cfg.Deploy.Domain)
	}
	if len(cfg.Env.Files) > 0 {
		p.Detail("env files", strings.Join(cfg.Env.Files, ", "))
	}
	if len(req.Env) > 0 {
		p.Detail("env", strings.Join(sortedKeys(req.Env), ", "))
	}

	// =========================
	// NETWORK
	// =========================

	if cfg.Deploy.Network != "" && req.CreateNetwork && !docker.NetworkExists(cfg.Deploy.Network) {
		p.Step("network", "creating %s", cfg.Deploy.Network)
		if err := docker.CreateNetwork(ctx, cfg.Deploy.Network, p.Writer()); err != nil {
			return nil, fmt.Errorf("network: %w", err)
		}
	}

	// =========================
	// BUILD + COMPOSE + UP + VERIFY
	// =========================

	cs, err := s.Runner.Apply(ctx, dir, cfg, update.ApplyOptions{Build: true, SetEnv: req.Env}, p)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}
	if cs.Health != "" {
		p.Detail("health", cs.Health)
	}

	// =========================
	// STATE
	// =========================

	st, err := state.Load()
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	st.Apps[name] = update.MergeState(st.Apps[name], cfg, dir)
	if err := state.Save(st); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	p.Ok("%s deployed", name)
	return &DeployResult{Name: name, Image: cfg.Image, Path: dir, Health: cs.Health, Existing: existing}, nil
}

// DeployName is the service name a request deploys to: Name, or one
// derived from Repo (or Image, when it names the built image) or Image.
func DeployName(req DeployRequest) string {
	switch {
	case req.Name != "":
		return req.Name
	case req.Repo == "":
		return docker.AdvancedNormalizeName(req.Image)
	case req.Image != "":
		return docker.NormalizeName(req.Image)
	default:
		return docker.NormalizeName(git.ExtractRepoName(req.Repo))
	}
}

// loadExisting returns the saved configuration of a service or a fresh one.
func loadExisting(name, dir string) (*deployment.Config, bool, error) {
	cfg, err := deployment.Load(dir)
	if err == nil {
		if cfg.Name != name {
			return nil, false, fmt.Errorf("%s/%s describes %q, not %q", dir, deployment.FileName, cfg.Name, name)
		}
		return cfg, true, nil
	}
	if !errors.Is(err, deployment.ErrNotFound) {
		return nil, false, err
	}

	// A legacy deployment registered in state: migrate its settings.
	if st, serr := state.Load(); serr == nil {
		if app, ok := st.Apps[name]; ok && app.Path == dir {
			if cfg, merr := deployment.FromLegacy(name, app); merr == nil {
				return cfg, true, nil
			}
		}
	}

	return &deployment.Config{Version: deployment.CurrentVersion, Name: name}, false, nil
}

// apply overlays the request onto cfg. A new deployment starts from the
// defaults; an existing one only changes the fields that were given.
func (r DeployRequest) apply(cfg *deployment.Config, existing bool, name, image string, isImage bool) error {
	cfg.Name = name
	cfg.Image = image
	if isImage {
		cfg.Source = deployment.Source{Type: deployment.SourceImage}
		cfg.Build = deployment.Build{}
	} else {
		cfg.Source = deployment.Source{Type: deployment.SourceGit, Repository: r.Repo}
	}
	if !existing {
		cfg.Deploy.Mode = deployment.ModeLocal
		cfg.Deploy.InternalPort = deployment.DefaultInternalPort
		cfg.Deploy.Entrypoint = deployment.DefaultEntrypoint
		if !isImage {
			cfg.Build.Platform = deployment.DefaultPlatform
		}
	}

	setStr := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setList := func(dst *[]string, v *[]string) {
		if v != nil {
			*dst = *v
		}
	}
	setStr(&cfg.Deploy.Mode, r.Mode)
	setStr(&cfg.Deploy.Domain, r.Domain)
	setStr(&cfg.Deploy.Network, r.Network)
	setStr(&cfg.Deploy.InternalPort, r.InternalPort)
	setList(&cfg.Deploy.Ports, r.Ports)
	setList(&cfg.Deploy.Volumes, r.Volumes)
	setStr(&cfg.Deploy.Entrypoint, r.Entrypoint)
	if r.TLS != nil {
		cfg.Deploy.TLS = *r.TLS
	}
	setStr(&cfg.Deploy.CertResolver, r.CertResolver)
	setStr(&cfg.Deploy.RouterName, r.RouterName)
	setList(&cfg.Deploy.Middlewares, r.Middlewares)

	// Build
	if !isImage {
		if r.Platform != nil {
			if err := deployment.ValidatePlatform(*r.Platform); err != nil {
				return err
			}
			cfg.Build.Platform = *r.Platform
		}
		if r.BuildArgs != nil {
			args, fromEnv, err := ParseBuildArgs(*r.BuildArgs)
			if err != nil {
				return err
			}
			cfg.Build.Args = args
			cfg.Build.ArgsFromEnv = fromEnv
		}
	} else if r.BuildArgs != nil || r.Platform != nil {
		return errors.New("build args and platform only apply to repository deployments")
	}

	// Env files: stored as absolute paths, replaced when given.
	if r.EnvFiles != nil {
		files := make([]string, 0, len(*r.EnvFiles))
		seen := map[string]bool{}
		for _, f := range *r.EnvFiles {
			abs, err := filepath.Abs(f)
			if err != nil {
				return fmt.Errorf("env file %s: %w", f, err)
			}
			if err := envfile.Validate(abs); err != nil {
				return err
			}
			if !seen[abs] {
				seen[abs] = true
				files = append(files, abs)
			}
		}
		cfg.Env.Files = files
	}

	// Healthcheck
	if r.NoHealthcheck {
		cfg.Healthcheck = deployment.Healthcheck{Enabled: false}
	} else {
		if r.HealthcheckCmd != nil {
			cfg.Healthcheck.Enabled = *r.HealthcheckCmd != ""
			cfg.Healthcheck.Command = *r.HealthcheckCmd
		}
		setStr(&cfg.Healthcheck.Interval, r.HealthcheckInterval)
		setStr(&cfg.Healthcheck.Timeout, r.HealthcheckTimeout)
		if r.HealthcheckRetries != nil {
			if *r.HealthcheckRetries < 0 {
				return errors.New("healthcheck retries must be >= 0")
			}
			cfg.Healthcheck.Retries = *r.HealthcheckRetries
		}
		setStr(&cfg.Healthcheck.StartPeriod, r.HealthcheckStartPeriod)
	}

	for k, v := range r.Env {
		if _, _, err := envfile.ParseAssignment(k + "=" + v); err != nil {
			return fmt.Errorf("env: %w", err)
		}
	}

	cfg.Normalize()
	return cfg.Validate(true)
}

// ParseEnvVars parses KEY=VALUE assignments.
func ParseEnvVars(vals []string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range vals {
		k, val, err := envfile.ParseAssignment(v)
		if err != nil {
			return nil, fmt.Errorf("--env: %w", err)
		}
		out[k] = val
	}
	return out, nil
}

// ParseBuildArgs parses build arguments. KEY=VALUE is stored; a bare KEY
// is read from the environment at build time.
func ParseBuildArgs(vals []string) (map[string]string, []string, error) {
	args := map[string]string{}
	var fromEnv []string
	for _, v := range vals {
		if !strings.Contains(v, "=") {
			k := strings.TrimSpace(v)
			if !envfile.ValidKey(k) {
				return nil, nil, fmt.Errorf("--build-arg: invalid name %q", k)
			}
			fromEnv = append(fromEnv, k)
			continue
		}
		k, val, err := envfile.ParseAssignment(v)
		if err != nil {
			return nil, nil, fmt.Errorf("--build-arg: %w", err)
		}
		args[k] = val
	}
	if len(args) == 0 {
		args = nil
	}
	return args, fromEnv, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
