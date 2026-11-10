package cmd

import (
	"errors"
	"os"

	"shipyard/internal/core"
	"shipyard/internal/deployment"
	"shipyard/internal/prompt"
	"shipyard/internal/ui"

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
	Short: "Deploy from a GitHub repo or Docker image (run without flags for a wizard)",
	Long: `Deploy from a GitHub repo or Docker image.

Running deploy again for an existing service starts from its saved
deployment.yaml: flags you pass override the saved values, everything
else is preserved. --env merges into the existing variables.`,
	Run: func(cmd *cobra.Command, args []string) {

		// =========================
		// VALIDATION
		// =========================

		if repo == "" && image == "" {
			if !prompt.Interactive() {
				ui.Fatal("specify --repo or --image (or run in a terminal for the interactive wizard)")
			}
			if err := runDeployWizard(cmd.Flags()); err != nil {
				if errors.Is(err, prompt.ErrAborted) {
					ui.Info("aborted")
					exitQuiet()
				}
				ui.Fatal("%s", err)
			}
		}

		if t := resolveTarget(); t.remote() {
			deployRemote(t, cmd)
		}
		if envStdin {
			if err := readEnvStdin(os.Stdin); err != nil {
				ui.Fatal("reading env from stdin: %s", err)
			}
		}

		ui.Section("deploy")

		if _, err := core.New().Deploy(cmd.Context(), deployRequest(cmd.Flags()), ui.Progress()); err != nil {
			ui.Fatal("%s", err)
		}
	},
}

// deployRequest maps the flags the user set onto a core.DeployRequest:
// unset flags stay nil so a redeploy keeps the saved values.
func deployRequest(fs *pflag.FlagSet) core.DeployRequest {
	str := func(name string, v string) *string {
		if !fs.Changed(name) {
			return nil
		}
		return &v
	}
	list := func(name string, v []string) *[]string {
		if !fs.Changed(name) {
			return nil
		}
		return &v
	}
	req := core.DeployRequest{
		Repo:          repo,
		Image:         image,
		Name:          serviceNameFlag,
		Root:          composePath,
		Mode:          str("mode", mode),
		Domain:        str("domain", domain),
		Network:       str("network", network),
		CreateNetwork: createNetwork,
		InternalPort:  str("internal-port", internalPort),
		Ports:         list("port", ports),
		Volumes:       list("volume", volumes),
		EnvFiles:      list("env-file", envFiles),
		BuildArgs:     list("build-arg", buildArgs),
		Platform:      str("platform", platform),
		Entrypoint:    str("entrypoint", entrypoint),
		CertResolver:  str("cert-resolver", certResolver),
		RouterName:    str("router-name", routerName),
		Middlewares:   list("middleware", middlewares),

		HealthcheckCmd:         str("healthcheck-cmd", healthcheckCmd),
		HealthcheckInterval:    str("healthcheck-interval", healthcheckInterval),
		HealthcheckTimeout:     str("healthcheck-timeout", healthcheckTimeout),
		HealthcheckStartPeriod: str("healthcheck-start-period", healthcheckStartPeriod),
		NoHealthcheck:          noHealthcheck,
	}
	if fs.Changed("tls") {
		req.TLS = &tls
	}
	if fs.Changed("healthcheck-retries") {
		req.HealthcheckRetries = &healthcheckRetries
	}
	env, err := core.ParseEnvVars(envVars)
	if err != nil {
		ui.Fatal("%s", err)
	}
	if len(env) > 0 {
		req.Env = env
	}
	return req
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

	f.BoolVar(&envStdin, "env-stdin", false, "Read KEY=VALUE env lines from stdin (used by remote deploys)")
	_ = f.MarkHidden("env-stdin")
}
