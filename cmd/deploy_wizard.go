package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"shipyard/internal/config"
	"shipyard/internal/core"
	"shipyard/internal/deployment"
	"shipyard/internal/docker"
	"shipyard/internal/prompt"
	"shipyard/internal/remote"
	"shipyard/internal/ui"

	"github.com/charmbracelet/huh"
	"github.com/spf13/pflag"
)

const (
	extraVolumes     = "volumes"
	extraEnv         = "env"
	extraEnvFile     = "env-file"
	extraHealthcheck = "healthcheck"
	extraBuild       = "build"
)

// wizardAnswers collects the values asked by runDeployWizard.
type wizardAnswers struct {
	source       string // "repo" | "image"
	repo         string
	image        string
	name         string
	mode         string
	domain       string
	network      string
	internalPort string
	tls          bool
	certResolver string
	ports        string
	extras       []string
	volumes      string
	env          string
	envFile      string
	healthCmd    string
	platform     string
	buildArgs    string
	createNet    bool
	server       string
}

// runDeployWizard asks for the deploy settings interactively and applies
// them to the deploy flags, so the regular deploy flow runs afterwards.
// Flags already given on the command line are kept and not asked again.
func runDeployWizard(fs *pflag.FlagSet) error {
	a := wizardAnswers{
		source:       "repo",
		mode:         mode,
		domain:       domain,
		network:      network,
		internalPort: internalPort,
		tls:          tls,
		certResolver: certResolver,
		name:         serviceNameFlag,
		platform:     platform,
	}
	if fs.Changed("port") {
		a.ports = strings.Join(ports, ", ")
	}

	// Servers: only asked when some are configured and --server was not given.
	scfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	a.server = serverFlag
	askServer := serverFlag == "" && len(scfg.Servers) > 0
	if a.server == "" {
		a.server = scfg.DefaultServer
	}
	if a.server == "" {
		a.server = localTarget
	}
	serverOpts := []huh.Option[string]{huh.NewOption("This machine (local)", localTarget)}
	for _, n := range sortedServerNames(scfg.Servers) {
		serverOpts = append(serverOpts, huh.NewOption(fmt.Sprintf("%s  (%s)", n, scfg.Servers[n].Host), n))
	}

	fmt.Println()
	fmt.Println(ui.Colorize("bold", "Shipyard deploy wizard") + ui.Colorize("dim", "  (Esc/Ctrl+C to cancel)"))
	fmt.Println()

	isRepo := func() bool { return a.source == "repo" }
	isTraefik := func() bool { return a.mode == "traefik" }
	has := func(x string) bool { return contains(a.extras, x) }

	extraOpts := []huh.Option[string]{
		huh.NewOption("Volumes", extraVolumes),
		huh.NewOption("Environment variables", extraEnv),
		huh.NewOption("Env file", extraEnvFile),
		huh.NewOption("Healthcheck", extraHealthcheck),
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Where do you want to deploy?").
				Options(serverOpts...).
				Value(&a.server),
		).WithHideFunc(func() bool { return !askServer }),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What do you want to deploy?").
				Options(
					huh.NewOption("A GitHub repository (build & push to GHCR)", "repo"),
					huh.NewOption("An existing Docker image", "image"),
				).
				Value(&a.source),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("GitHub repository").
				Description("Format: owner/name").
				Placeholder("user/my-app").
				Value(&a.repo).
				Validate(deployment.ValidateRepo),
		).WithHideFunc(func() bool { return !isRepo() }),
		huh.NewGroup(
			huh.NewInput().
				Title("Docker image").
				Placeholder("traefik/whoami").
				Value(&a.image).
				Validate(required("image")),
		).WithHideFunc(isRepo),
		huh.NewGroup(
			huh.NewInput().
				Title("Service name").
				Description("Leave empty to derive it from the repo or image").
				Value(&a.name).
				Validate(func(s string) error {
					if s != "" && !deployment.ValidName(s) {
						return errors.New("use lowercase letters, numbers and dashes")
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("How should it be exposed?").
				Options(
					huh.NewOption("Local: publish ports on this host", "local"),
					huh.NewOption("Traefik: route a domain through the reverse proxy", "traefik"),
				).
				Value(&a.mode),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Ports").
				Description("host:container, comma separated (empty for none)").
				Placeholder("3000:3000").
				Value(&a.ports),
		).WithHideFunc(isTraefik),
		huh.NewGroup(
			huh.NewInput().
				Title("Domain").
				Placeholder("app.example.com").
				Value(&a.domain).
				Validate(required("domain")),
			huh.NewInput().
				Title("Docker network").
				Description("The network Traefik is attached to").
				Placeholder("edge").
				Value(&a.network).
				Validate(required("network")),
			huh.NewInput().
				Title("Container port").
				Description("Port the app listens on inside the container").
				Value(&a.internalPort).
				Validate(required("port")),
			huh.NewConfirm().
				Title("Enable TLS (HTTPS)?").
				Value(&a.tls),
		).WithHideFunc(func() bool { return !isTraefik() }),
		huh.NewGroup(
			huh.NewInput().
				Title("Certificate resolver").
				Description("Optional, e.g. letsencrypt").
				Value(&a.certResolver),
		).WithHideFunc(func() bool { return !isTraefik() || !a.tls }),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Additional options").
				Description("Space to select, Enter to continue (none is fine)").
				OptionsFunc(func() []huh.Option[string] {
					if isRepo() {
						return append(extraOpts, huh.NewOption("Build platform / build args", extraBuild))
					}
					return extraOpts
				}, &a.source).
				Value(&a.extras),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Volumes").
				Description("host:container, comma separated").
				Placeholder("/srv/data:/app/data").
				Value(&a.volumes),
		).WithHideFunc(func() bool { return !has(extraVolumes) }),
		huh.NewGroup(
			huh.NewText().
				Title("Environment variables").
				Description("One KEY=VALUE per line (stored in the service .env, not in yaml)").
				Value(&a.env).
				Validate(func(s string) error {
					_, err := core.ParseEnvVars(lines(s))
					return err
				}),
		).WithHideFunc(func() bool { return !has(extraEnv) }),
		huh.NewGroup(
			huh.NewInput().
				Title("Env file path").
				Value(&a.envFile).
				Validate(func(s string) error {
					if s == "" {
						return errors.New("path is required")
					}
					if _, err := os.Stat(s); err != nil {
						return fmt.Errorf("cannot read %s", s)
					}
					return nil
				}),
		).WithHideFunc(func() bool { return !has(extraEnvFile) }),
		huh.NewGroup(
			huh.NewInput().
				Title("Healthcheck command").
				Description("Run inside the container; exit 0 means healthy").
				Placeholder("curl -f http://localhost/ || exit 1").
				Value(&a.healthCmd).
				Validate(required("command")),
		).WithHideFunc(func() bool { return !has(extraHealthcheck) }),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Build platform").
				Options(
					huh.NewOption("linux/arm64 (Raspberry Pi 4/5, Apple Silicon)", "linux/arm64"),
					huh.NewOption("linux/amd64 (most servers)", "linux/amd64"),
					huh.NewOption("linux/arm/v7 (older ARM boards)", "linux/arm/v7"),
				).
				Value(&a.platform),
			huh.NewText().
				Title("Build args").
				Description("One KEY=VALUE (or KEY to read from env) per line, optional. Not for secrets.").
				Value(&a.buildArgs).
				Validate(func(s string) error {
					_, _, err := core.ParseBuildArgs(lines(s))
					return err
				}),
		).WithHideFunc(func() bool { return !isRepo() || !has(extraBuild) }),
	)

	if err := prompt.Run(form); err != nil {
		return err
	}

	serverFlag = a.server
	t := resolveTarget()

	// Offer to create a missing Traefik network.
	if a.mode == "traefik" && !createNetwork && !networkExists(t, a.network) {
		a.createNet = true
		err := prompt.Run(huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Network %q does not exist. Create it?", a.network)).
				Value(&a.createNet),
		)))
		if err != nil {
			return err
		}
	}

	if err := a.apply(fs); err != nil {
		return err
	}

	fmt.Println()
	ui.Info("equivalent command:")
	fmt.Println("  " + ui.Colorize("cyan", a.command()))
	fmt.Println()

	ok := true
	if err := prompt.Run(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Deploy now?").Value(&ok),
	))); err != nil {
		return err
	}
	if !ok {
		return prompt.ErrAborted
	}
	return nil
}

// apply writes the answers into the deploy flags through the FlagSet, so
// they count as explicitly set (fs.Changed) for existing deployments.
func (a *wizardAnswers) apply(fs *pflag.FlagSet) error {
	set := func(name, val string) error { return fs.Set(name, val) }
	setList := func(name string, vals []string) error {
		for _, v := range vals {
			if err := fs.Set(name, v); err != nil {
				return err
			}
		}
		return nil
	}

	var errs []error
	if a.source == "repo" {
		errs = append(errs, set("repo", a.repo))
	} else {
		errs = append(errs, set("image", a.image))
	}
	if a.name != "" {
		errs = append(errs, set("name", a.name))
	}
	errs = append(errs, set("mode", a.mode))

	if a.mode == "traefik" {
		errs = append(errs,
			set("domain", a.domain),
			set("network", a.network),
			set("internal-port", a.internalPort),
			set("tls", fmt.Sprint(a.tls)),
		)
		if a.tls && a.certResolver != "" {
			errs = append(errs, set("cert-resolver", a.certResolver))
		}
		if a.createNet {
			errs = append(errs, set("create-network", "true"))
		}
	} else if p := splitList(a.ports); len(p) > 0 {
		errs = append(errs, setList("port", p))
	}

	if v := splitList(a.volumes); len(v) > 0 {
		errs = append(errs, setList("volume", v))
	}
	if e := lines(a.env); len(e) > 0 {
		errs = append(errs, setList("env", e))
	}
	if a.envFile != "" {
		errs = append(errs, set("env-file", a.envFile))
	}
	if a.healthCmd != "" {
		errs = append(errs, set("healthcheck-cmd", a.healthCmd))
	}
	if a.source == "repo" && contains(a.extras, extraBuild) {
		errs = append(errs, set("platform", a.platform))
		if b := lines(a.buildArgs); len(b) > 0 {
			errs = append(errs, setList("build-arg", b))
		}
	}
	return errors.Join(errs...)
}

// command renders the non-interactive equivalent of the answers. Env
// values are masked so secrets are not echoed to the terminal.
func (a *wizardAnswers) command() string {
	parts := []string{"shipyard deploy"}
	add := func(flag, val string) { parts = append(parts, "--"+flag+" "+shellQuote(val)) }

	if a.server != "" && a.server != localTarget {
		add("server", a.server)
	}

	if a.source == "repo" {
		add("repo", a.repo)
	} else {
		add("image", a.image)
	}
	if a.name != "" {
		add("name", a.name)
	}
	add("mode", a.mode)
	if a.mode == "traefik" {
		add("domain", a.domain)
		add("network", a.network)
		if a.createNet {
			parts = append(parts, "--create-network")
		}
		if a.internalPort != "80" {
			add("internal-port", a.internalPort)
		}
		if a.tls {
			parts = append(parts, "--tls")
			if a.certResolver != "" {
				add("cert-resolver", a.certResolver)
			}
		}
	} else {
		for _, p := range splitList(a.ports) {
			add("port", p)
		}
	}
	for _, v := range splitList(a.volumes) {
		add("volume", v)
	}
	for _, e := range lines(a.env) {
		k, _, _ := strings.Cut(e, "=")
		parts = append(parts, "--env "+shellQuote(strings.TrimSpace(k))+"=***")
	}
	if a.envFile != "" {
		abs, err := filepath.Abs(a.envFile)
		if err != nil {
			abs = a.envFile
		}
		add("env-file", abs)
	}
	if a.healthCmd != "" {
		add("healthcheck-cmd", a.healthCmd)
	}
	if a.source == "repo" && contains(a.extras, extraBuild) {
		if a.platform != deployment.DefaultPlatform {
			add("platform", a.platform)
		}
		for _, b := range lines(a.buildArgs) {
			add("build-arg", b)
		}
	}
	return strings.Join(parts, " \\\n    ")
}

func required(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", what)
		}
		return nil
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

func contains(list []string, x string) bool {
	for _, e := range list {
		if e == x {
			return true
		}
	}
	return false
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'$`\\|&;<>()*?!#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// networkExists checks a Docker network on the target machine.
func networkExists(t target, name string) bool {
	if !t.remote() {
		return docker.NetworkExists(name)
	}
	_, err := remote.Output(t.Server.Host, "docker", "network", "inspect", name)
	return err == nil
}
