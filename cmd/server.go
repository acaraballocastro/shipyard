package cmd

import (
	"errors"
	"fmt"
	"strings"

	"shipyard/internal/config"
	"shipyard/internal/deployment"
	"shipyard/internal/prompt"
	"shipyard/internal/remote"
	"shipyard/internal/ui"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

const installURL = "https://raw.githubusercontent.com/acaraballocastro/shipyard/main/install.sh"

var serverHost string
var serverSetDefault bool

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage remote servers that deploy over SSH",
	Long: `Manage remote servers. Commands run with --server <name> (or on the
default server) are executed on that host over SSH: clone, build and
deployment happen there. "--server local" forces this machine.`,
}

var serverAddCmd = &cobra.Command{
	Use:   "add [name]",
	Short: "Add a server, check it and install shipyard on it if needed",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadOrEmpty()
		if err != nil {
			ui.Fatal("config: %s", err)
		}

		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		makeDefault := serverSetDefault || len(cfg.Servers) == 0

		if (name == "" || serverHost == "") && prompt.Interactive() {
			err := prompt.Run(huh.NewForm(huh.NewGroup(
				huh.NewInput().Title("Server name").Placeholder("pi").Value(&name).
					Validate(validServerName),
				huh.NewInput().Title("SSH host").
					Description("user@host, or an alias from ~/.ssh/config").
					Placeholder("pi@192.168.1.50").Value(&serverHost).
					Validate(required("host")),
				huh.NewConfirm().Title("Use it as the default server?").Value(&makeDefault),
			)))
			if errors.Is(err, prompt.ErrAborted) {
				ui.Info("aborted")
				exitQuiet()
			}
			if err != nil {
				ui.Fatal("%s", err)
			}
		}
		if err := validServerName(name); err != nil {
			ui.Fatal("%s", err)
		}
		if serverHost == "" {
			ui.Fatal("--host is required")
		}

		ui.Section("server")
		if !checkServer(name, serverHost) {
			ui.Fatal("server %s is not ready, nothing saved", name)
		}

		if cfg.Servers == nil {
			cfg.Servers = map[string]config.Server{}
		}
		cfg.Servers[name] = config.Server{Host: serverHost}
		if makeDefault {
			cfg.DefaultServer = name
		}
		if err := config.Save(cfg); err != nil {
			ui.Fatal("saving config: %s", err)
		}

		ui.Ok("server %s saved", name)
		if makeDefault {
			ui.Info("default server: %s (use --server local to deploy on this machine)", name)
		} else {
			ui.Info("use it with: shipyard deploy --server %s", name)
		}
	},
}

// checkServer verifies SSH access, Docker and shipyard on host, offering
// to install shipyard and run its setup. Returns false when unusable.
func checkServer(name, host string) bool {
	if _, err := remote.Output(host, "true"); err != nil {
		ui.CheckFail("ssh", err.Error())
		ui.Info("make sure `ssh %s` works without a password prompt (ssh-copy-id %s)", host, host)
		return false
	}
	ui.CheckOk("ssh", host)

	if out, err := remote.Output(host, "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
		ui.CheckFail("docker", "not available for this user on the server")
		return false
	} else {
		ui.CheckOk("docker", strings.TrimSpace(string(out)))
	}

	// list --json under --server local proves a shipyard recent enough.
	if _, err := remote.Output(host, remote.Bin, "--server", localTarget, "list", "--json"); err != nil {
		ui.CheckFail("shipyard", "missing or outdated on the server")
		if !prompt.Interactive() || !ui.Confirm("install the latest shipyard on "+name+" now? (uses sudo)") {
			ui.Info("install it on the server with: curl -sSL %s | bash", installURL)
			return false
		}
		err := remote.Run(host, remote.Options{TTY: true}, "sh", "-c", "curl -sSL "+installURL+" | bash")
		if err != nil {
			ui.Error("install failed: %s", err)
			return false
		}
		if _, err := remote.Output(host, remote.Bin, "--server", localTarget, "list", "--json"); err != nil {
			ui.Error("shipyard installed but still not usable: %s", err)
			ui.Info("the published release may be older than this client; publish a new release and retry")
			return false
		}
	}
	ui.CheckOk("shipyard", "installed")

	// Builds run on the server: it needs its own shipyard setup (github_user),
	// a GHCR login and GitHub SSH access.
	if _, err := remote.Output(host, "test", "-f", ".shipyard.yaml"); err != nil {
		ui.CheckFail("setup", "shipyard setup has not been run on the server")
		if prompt.Interactive() && ui.Confirm("run shipyard setup on "+name+" now?") {
			_ = remote.Run(host, remote.Options{TTY: true}, remote.Bin, "setup")
		}
	} else {
		ui.CheckOk("setup", "~/.shipyard.yaml")
	}

	ui.Info("repo deployments build on the server, so it also needs:")
	ui.Detail("ghcr", "docker login ghcr.io (on the server)")
	ui.Detail("github", "an SSH key with access to your repositories (ssh -T git@github.com)")
	ui.Detail("buildx", "docker buildx (check: ssh "+host+" docker buildx version)")
	return true
}

var serverListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured servers",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadOrEmpty()
		if err != nil {
			ui.Fatal("config: %s", err)
		}
		def := cfg.DefaultServer
		if def == "" {
			def = localTarget
		}
		rows := [][]string{{localTarget, "this machine", mark(def == localTarget)}}
		for _, n := range sortedServerNames(cfg.Servers) {
			rows = append(rows, []string{n, cfg.Servers[n].Host, mark(def == n)})
		}
		ui.Table([]string{"name", "host", "default"}, rows)
	},
}

var serverUseCmd = &cobra.Command{
	Use:   "use [name|local]",
	Short: "Set the default server (local for this machine)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadOrEmpty()
		if err != nil {
			ui.Fatal("config: %s", err)
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		} else {
			name = pickServer(cfg, "Default server", true)
		}
		if name != localTarget {
			if _, ok := cfg.Servers[name]; !ok {
				ui.Fatal("unknown server %q", name)
			}
			cfg.DefaultServer = name
		} else {
			cfg.DefaultServer = ""
		}
		if err := config.Save(cfg); err != nil {
			ui.Fatal("saving config: %s", err)
		}
		ui.Ok("default server: %s", name)
	},
}

var serverRemoveCmd = &cobra.Command{
	Use:   "remove [name]",
	Short: "Forget a server (its services keep running)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadOrEmpty()
		if err != nil {
			ui.Fatal("config: %s", err)
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		} else {
			name = pickServer(cfg, "Server to remove", false)
		}
		if _, ok := cfg.Servers[name]; !ok {
			ui.Fatal("unknown server %q", name)
		}
		delete(cfg.Servers, name)
		if cfg.DefaultServer == name {
			cfg.DefaultServer = ""
		}
		if err := config.Save(cfg); err != nil {
			ui.Fatal("saving config: %s", err)
		}
		ui.Ok("server %s removed", name)
	},
}

func pickServer(cfg *config.Config, title string, withLocal bool) string {
	if !prompt.Interactive() {
		ui.Fatal("missing server name")
	}
	var opts []huh.Option[string]
	if withLocal {
		opts = append(opts, huh.NewOption("local (this machine)", localTarget))
	}
	for _, n := range sortedServerNames(cfg.Servers) {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s)", n, cfg.Servers[n].Host), n))
	}
	if len(opts) == 0 {
		ui.Fatal("no servers configured (try: shipyard server add)")
	}
	var choice string
	err := prompt.Run(huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(title).Options(opts...).Value(&choice),
	)))
	if errors.Is(err, prompt.ErrAborted) {
		ui.Info("aborted")
		exitQuiet()
	}
	if err != nil {
		ui.Fatal("%s", err)
	}
	return choice
}

func validServerName(s string) error {
	if s == localTarget {
		return errors.New(`"local" is reserved for this machine`)
	}
	if !deployment.ValidName(s) {
		return errors.New("use lowercase letters, digits and dashes")
	}
	return nil
}

func mark(b bool) string {
	if b {
		return "*"
	}
	return ""
}

func init() {
	rootCmd.AddCommand(serverCmd)
	serverCmd.AddCommand(serverAddCmd, serverListCmd, serverUseCmd, serverRemoveCmd)
	serverAddCmd.Flags().StringVar(&serverHost, "host", "", "SSH host: user@host or ~/.ssh/config alias")
	serverAddCmd.Flags().BoolVar(&serverSetDefault, "default", false, "Make it the default server")
}
