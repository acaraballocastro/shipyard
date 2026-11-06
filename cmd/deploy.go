package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"shipyard/internal/config"
	"shipyard/internal/deployment"
	"shipyard/internal/docker"
	"shipyard/internal/envfile"
	"shipyard/internal/git"
	"shipyard/internal/state"
	"shipyard/internal/ui"
	shipyardUpdate "shipyard/internal/update"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var repo string
var image string
var serviceNameFlag string

var ports []string
var volumes []string
var composePath string
var mode string
var domain string
var internalPort string
var network string
var createNetwork bool

var envFiles []string
var envVars []string
var buildArgs []string
var platform string

var entrypoint string
var tls bool
var certResolver string
var routerName string
var middlewares []string

var healthcheckCmd string
var healthcheckInterval string
var healthcheckTimeout string
var healthcheckRetries int
var healthcheckStartPeriod string
var noHealthcheck bool

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy from a GitHub repo or Docker image",
	Long: `Deploy from a GitHub repo or Docker image.

Running deploy again for an existing service starts from its saved
deployment.yaml: flags you pass override the saved values, everything
else is preserved. --env merges into the existing variables.`,
	Run: func(cmd *cobra.Command, args []string) {

		// =========================
		// VALIDATION
		// =========================

		if repo == "" && image == "" {
			ui.Fatal("specify --repo or --image")
		}
		if repo != "" {
			if err := deployment.ValidateRepo(repo); err != nil {
				ui.Fatal("%s", err)
			}
		}

		ui.Section("deploy")

		// =========================
		// RESOLVE SOURCE
		// =========================

		isImage := image != "" && repo == ""

		var serviceName string
		var finalImage string

		if isImage {
			ui.Info("mode image: %s", image)
			serviceName = docker.AdvancedNormalizeName(image)
			finalImage = image
		} else {
			cfg, err := config.Load()
			if err != nil {
				ui.Fatal("config: %s", err)
			}
			imgName := image
			if imgName == "" {
				imgName = git.ExtractRepoName(repo)
			}
			serviceName = docker.NormalizeName(imgName)
			finalImage = fmt.Sprintf("ghcr.io/%s/%s", strings.ToLower(cfg.GitHubUser), serviceName)
			ui.Info("mode repo: %s", repo)
		}

		if serviceNameFlag != "" {
			serviceName = serviceNameFlag
		}
		if !deployment.ValidName(serviceName) {
			ui.Fatal("invalid service name %q (use --name to choose one)", serviceName)
		}

		dir := filepath.Join(composePath, serviceName)

		// =========================
		// BUILD CONFIGURATION
		// =========================

		cfg, existing, err := loadExisting(serviceName, dir)
		if err != nil {
			ui.Fatal("%s", err)
		}
		if existing {
			ui.Info("existing deployment found, unspecified settings are preserved")
		}

		setEnv, err := applyFlags(cmd.Flags(), cfg, existing, serviceName, finalImage, isImage)
		if err != nil {
			ui.Fatal("%s", err)
		}

		ui.Detail("image", cfg.Image)
		ui.Detail("path", dir)
		if cfg.Deploy.Network != "" {
			ui.Detail("network", cfg.Deploy.Network)
		}
		if cfg.Deploy.Domain != "" {
			ui.Detail("domain", cfg.Deploy.Domain)
		}
		if len(cfg.Env.Files) > 0 {
			ui.Detail("env files", strings.Join(cfg.Env.Files, ", "))
		}
		if len(setEnv) > 0 {
			ui.Detail("env", strings.Join(sortedKeys(setEnv), ", "))
		}

		// =========================
		// NETWORK
		// =========================

		if cfg.Deploy.Network != "" && createNetwork {
			if !docker.NetworkExists(cfg.Deploy.Network) {
				ui.Step("network", "creating %s", cfg.Deploy.Network)
				if err := docker.CreateNetwork(cfg.Deploy.Network); err != nil {
					ui.Fatal("network: %s", err)
				}
			}
		}

		// =========================
		// BUILD + COMPOSE + UP + VERIFY
		// =========================

		cs, err := shipyardUpdate.DefaultRunner.Apply(dir, cfg, shipyardUpdate.ApplyOptions{
			Build:  true,
			SetEnv: setEnv,
		})
		if err != nil {
			ui.Fatal("deploy: %s", err)
		}
		if cs.Health != "" {
			ui.Detail("health", cs.Health)
		}

		// =========================
		// STATE
		// =========================

		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}
		st.Apps[serviceName] = shipyardUpdate.MergeState(st.Apps[serviceName], cfg, dir)
		if err := state.Save(st); err != nil {
			ui.Fatal("saving state: %s", err)
		}

		ui.Ok("%s deployed", serviceName)
	},
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

// applyFlags overlays CLI flags onto cfg. For a new deployment every flag
// (including defaults) applies; for an existing one only flags the user
// set explicitly. It returns the --env variables to merge.
func applyFlags(fs *pflag.FlagSet, cfg *deployment.Config, existing bool, name, finalImage string, isImage bool) (map[string]string, error) {
	set := func(flag string) bool { return !existing || fs.Changed(flag) }

	cfg.Name = name
	cfg.Image = finalImage
	if isImage {
		cfg.Source = deployment.Source{Type: deployment.SourceImage}
		cfg.Build = deployment.Build{}
	} else {
		cfg.Source = deployment.Source{Type: deployment.SourceGit, Repository: repo}
	}

	if set("mode") {
		cfg.Deploy.Mode = mode
	}
	if set("domain") {
		cfg.Deploy.Domain = domain
	}
	if set("network") {
		cfg.Deploy.Network = network
	}
	if set("internal-port") {
		cfg.Deploy.InternalPort = internalPort
	}
	if set("port") {
		cfg.Deploy.Ports = ports
	}
	if set("volume") {
		cfg.Deploy.Volumes = volumes
	}
	if set("entrypoint") {
		cfg.Deploy.Entrypoint = entrypoint
	}
	if set("tls") {
		cfg.Deploy.TLS = tls
	}
	if set("cert-resolver") {
		cfg.Deploy.CertResolver = certResolver
	}
	if set("router-name") {
		cfg.Deploy.RouterName = routerName
	}
	if set("middleware") {
		cfg.Deploy.Middlewares = middlewares
	}

	// Build
	if !isImage {
		if set("platform") {
			if err := deployment.ValidatePlatform(platform); err != nil {
				return nil, err
			}
			cfg.Build.Platform = platform
		}
		if set("build-arg") {
			args, fromEnv, err := ParseBuildArgs(buildArgs)
			if err != nil {
				return nil, err
			}
			cfg.Build.Args = args
			cfg.Build.ArgsFromEnv = fromEnv
		}
	} else if fs.Changed("build-arg") || fs.Changed("platform") {
		return nil, fmt.Errorf("--build-arg and --platform only apply to --repo deployments")
	}

	// Env files: stored as absolute paths, replaced when passed.
	if set("env-file") {
		files := make([]string, 0, len(envFiles))
		seen := map[string]bool{}
		for _, f := range envFiles {
			abs, err := filepath.Abs(f)
			if err != nil {
				return nil, fmt.Errorf("env file %s: %w", f, err)
			}
			if err := envfile.Validate(abs); err != nil {
				return nil, err
			}
			if !seen[abs] {
				seen[abs] = true
				files = append(files, abs)
			}
		}
		cfg.Env.Files = files
	}

	// Healthcheck
	if noHealthcheck {
		cfg.Healthcheck = deployment.Healthcheck{Enabled: false}
	} else {
		if fs.Changed("healthcheck-cmd") {
			cfg.Healthcheck.Enabled = healthcheckCmd != ""
			cfg.Healthcheck.Command = healthcheckCmd
		}
		if fs.Changed("healthcheck-interval") {
			cfg.Healthcheck.Interval = healthcheckInterval
		}
		if fs.Changed("healthcheck-timeout") {
			cfg.Healthcheck.Timeout = healthcheckTimeout
		}
		if fs.Changed("healthcheck-retries") {
			if healthcheckRetries < 0 {
				return nil, fmt.Errorf("--healthcheck-retries must be >= 0")
			}
			cfg.Healthcheck.Retries = healthcheckRetries
		}
		if fs.Changed("healthcheck-start-period") {
			cfg.Healthcheck.StartPeriod = healthcheckStartPeriod
		}
	}

	// --env values: merged into the local .env, never stored in yaml.
	setEnv, err := ParseEnvVars(envVars)
	if err != nil {
		return nil, err
	}

	cfg.Normalize()
	if err := cfg.Validate(true); err != nil {
		return nil, err
	}
	return setEnv, nil
}

// ParseEnvVars parses repeated --env KEY=VALUE flags.
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

// ParseBuildArgs parses repeated --build-arg flags. KEY=VALUE is stored;
// a bare KEY is read from the environment at build time.
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

func init() {
	rootCmd.AddCommand(deployCmd)

	f := deployCmd.Flags()

	f.StringVar(&repo, "repo", "", "GitHub repo (user/repo)")
	f.StringVar(&image, "image", "", "Docker image (e.g. traefik/whoami)")
	f.StringVar(&serviceNameFlag, "name", "", "Service name (default: derived from repo or image)")

	f.StringSliceVar(&ports, "port", []string{}, "Port mappings")
	f.StringSliceVar(&volumes, "volume", []string{}, "Volume mounts")

	f.StringVar(&composePath, "compose-path", "/srv/apps", "Base path for compose files")
	f.StringVar(&mode, "mode", "local", "Deployment mode: local | traefik")
	f.StringVar(&domain, "domain", "", "Traefik domain")
	f.StringVar(&network, "network", "", "Docker network")
	f.BoolVar(&createNetwork, "create-network", false, "Create network if it does not exist")
	f.StringVar(&internalPort, "internal-port", "80", "Internal container port")

	// Environment
	f.StringArrayVar(&envFiles, "env-file", nil, "Env file referenced by the container (repeatable, stored as absolute path)")
	f.StringArrayVar(&envVars, "env", nil, "Environment variable KEY=VALUE (repeatable, stored in the 0600 .env of the service)")

	// Build
	f.StringArrayVar(&buildArgs, "build-arg", nil, "Build argument KEY=VALUE, or KEY to read it from the environment (repeatable; not for secrets)")
	f.StringVar(&platform, "platform", deployment.DefaultPlatform, "Build platform, e.g. linux/arm64, linux/amd64, linux/arm/v7")

	// Traefik
	f.StringVar(&entrypoint, "entrypoint", deployment.DefaultEntrypoint, "Traefik entrypoint (e.g. web, websecure)")
	f.BoolVar(&tls, "tls", false, "Enable TLS on the Traefik router")
	f.StringVar(&certResolver, "cert-resolver", "", "Traefik certificate resolver (requires --tls)")
	f.StringVar(&routerName, "router-name", "", "Traefik router/service name (default: service name)")
	f.StringArrayVar(&middlewares, "middleware", nil, "Traefik middleware to attach, e.g. redirect@file (repeatable)")

	// Healthcheck
	f.StringVar(&healthcheckCmd, "healthcheck-cmd", "", "Healthcheck command run with the container shell (CMD-SHELL)")
	f.StringVar(&healthcheckInterval, "healthcheck-interval", "", "Healthcheck interval (e.g. 30s)")
	f.StringVar(&healthcheckTimeout, "healthcheck-timeout", "", "Healthcheck timeout (e.g. 5s)")
	f.IntVar(&healthcheckRetries, "healthcheck-retries", 0, "Healthcheck retries")
	f.StringVar(&healthcheckStartPeriod, "healthcheck-start-period", "", "Healthcheck start period (e.g. 20s)")
	f.BoolVar(&noHealthcheck, "no-healthcheck", false, "Remove a previously configured healthcheck")
}
