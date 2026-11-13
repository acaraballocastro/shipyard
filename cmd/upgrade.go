package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"shipyard/internal/api"
	"shipyard/internal/config"
	"shipyard/internal/events"
	"shipyard/internal/remote"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

// installCmd installs the latest release on the server. Run as root (sudo
// -n in the UI, sudo with a prompt in the terminal) because install.sh
// moves the binary to /usr/local/bin.
var installCmd = []string{"sh", "-c", "curl -fsSL " + installURL + " | bash"}

// restartServeCmd restarts shipyard serve if it is installed as a service.
const restartServeCmd = "if systemctl is-enabled --quiet shipyard 2>/dev/null; then systemctl restart shipyard; fi"

// waitForAPI polls the server API until it answers at the current level.
func waitForAPI(ctx context.Context, b api.Remote) (*api.Info, error) {
	var last error
	var info *api.Info
	for i := 0; i < 20; i++ {
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		info, last = b.Info(c)
		cancel()
		if last == nil && info.API >= api.APILevel {
			return info, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if last != nil {
		return nil, fmt.Errorf("the API does not answer after the update: %w", last)
	}
	return info, fmt.Errorf("the latest published release (%s, API level %d) is still older than this shipyard (level %d); publish a new release and retry",
		info.Version, info.API, api.APILevel)
}

// =========================
// GATEWAY (web UI)
// =========================

// gatewayRemote is a remote server seen from `shipyard ui`: the HTTP API,
// plus updates over SSH that run as jobs of this process. Those job ids
// carry the gateway prefix, so job lookups are routed here instead of to
// the server.
type gatewayRemote struct {
	api.Remote
	name string
	host string
	jobs *api.Jobs
}

func (g gatewayRemote) local(id string) bool { return strings.HasPrefix(id, g.jobs.Prefix) }

func (g gatewayRemote) Job(ctx context.Context, id string) (*api.Job, error) {
	if g.local(id) {
		return g.jobs.Get(id)
	}
	return g.Remote.Job(ctx, id)
}

func (g gatewayRemote) CancelJob(ctx context.Context, id string) error {
	if g.local(id) {
		return g.jobs.Cancel(id)
	}
	return g.Remote.CancelJob(ctx, id)
}

func (g gatewayRemote) JobEvents(ctx context.Context, id string, emit func(kind, data string) error) error {
	if g.local(id) {
		return g.jobs.Stream(ctx, id, emit)
	}
	return g.Remote.JobEvents(ctx, id, emit)
}

// Upgrade installs the latest shipyard on the server and restarts its API.
// Without a terminal sudo cannot ask for a password, so it needs
// passwordless sudo; otherwise the error says to use the CLI.
func (g gatewayRemote) Upgrade(context.Context) (*api.Job, error) {
	return g.jobs.Launch("shipyard@"+g.name, "upgrade", func(ctx context.Context, p events.Progress) error {
		p.Step("install", "installing the latest shipyard on %s", g.name)
		var out strings.Builder
		w := teeWriter{p.Writer(), &out}
		if err := remote.Stream(ctx, g.host, w, append([]string{"sudo", "-n"}, installCmd...)...); err != nil {
			if strings.Contains(out.String(), "password is required") {
				return fmt.Errorf("sudo on %s asks for a password; run in a terminal: shipyard server upgrade %s", g.name, g.name)
			}
			return fmt.Errorf("install: %w", err)
		}
		p.Step("restart", "restarting shipyard serve")
		if err := remote.Stream(ctx, g.host, p.Writer(), "sudo", "-n", "sh", "-c", restartServeCmd); err != nil {
			return fmt.Errorf("restart: %w", err)
		}
		p.Step("verify", "waiting for the API")
		info, err := waitForAPI(ctx, g.Remote)
		if err != nil {
			return err
		}
		p.Ok("%s now runs shipyard %s", g.name, info.Version)
		return nil
	})
}

type teeWriter struct {
	a interface{ Write([]byte) (int, error) }
	b *strings.Builder
}

func (t teeWriter) Write(p []byte) (int, error) {
	t.b.Write(p)
	return t.a.Write(p)
}

// =========================
// CLI
// =========================

var serverUpgradeCmd = &cobra.Command{
	Use:   "upgrade [name]",
	Short: "Install the latest shipyard on a server and restart its API",
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
			name = pickServer(cfg, "Server to upgrade", false)
		}
		srv, ok := cfg.Servers[name]
		if !ok {
			ui.Fatal("unknown server %s (see shipyard server list)", name)
		}

		ui.Step("install", "installing the latest shipyard on %s", name)
		if err := remote.Run(srv.Host, remote.Options{TTY: true}, append([]string{"sudo"}, installCmd...)...); err != nil {
			ui.Fatal("install failed: %s", err)
		}
		ui.Step("restart", "restarting shipyard serve")
		if err := remote.Run(srv.Host, remote.Options{TTY: true}, "sudo", "sh", "-c", restartServeCmd); err != nil {
			ui.Fatal("restart failed: %s", err)
		}
		if srv.URL == "" {
			ui.Ok("%s upgraded (web UI/API not enabled)", name)
			return
		}
		b, closeRoutes := remoteBackend(srv)
		info, err := waitForAPI(cmd.Context(), b)
		closeRoutes() // before any exit: ui.Fatal skips defers
		if err != nil {
			if errors.Is(err, context.Canceled) {
				exitQuiet()
			}
			ui.Fatal("%s", err)
		}
		ui.Ok("%s now runs shipyard %s", name, info.Version)
	},
}

func init() {
	serverCmd.AddCommand(serverUpgradeCmd)
}
