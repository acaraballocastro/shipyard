package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"shipyard/internal/api"
	"shipyard/internal/core"
	"shipyard/internal/ui"
	"shipyard/web"

	"github.com/spf13/cobra"
)

// DefaultServePort is where `shipyard serve` listens by default.
const DefaultServePort = "7878"

var serveListen string
var serveRotate bool

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the shipyard API and web UI on this server",
	Long: `Runs the HTTP API (/api/v1) and the web UI for this machine.
Requests need the token from "shipyard serve token". Usually installed
as a systemd service with "shipyard serve install" (done by server add).`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		token, err := api.LoadOrCreateToken()
		if err != nil {
			ui.Fatal("token: %s", err)
		}
		h := api.Handler(api.Options{
			Registry: localRegistry{svc: core.New()},
			Token:    token,
			UI:       web.FS(),
		})
		ui.Info("shipyard serve listening on %s", serveListen)
		if err := listen(cmd.Context(), serveListen, h); err != nil {
			ui.Fatal("%s", err)
		}
	},
}

var serveTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Print the API token of this server",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		var t string
		var err error
		if serveRotate {
			t, err = api.RotateToken()
		} else {
			t, err = api.LoadOrCreateToken()
		}
		if err != nil {
			ui.Fatal("token: %s", err)
		}
		fmt.Println(t)
	},
}

var serveInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install shipyard serve as a systemd service (uses sudo)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if _, err := exec.LookPath("systemctl"); err != nil {
			ui.Fatal("systemd not found: run shipyard serve manually or with your init system")
		}
		u, err := user.Current()
		if err != nil {
			ui.Fatal("%s", err)
		}
		bin, err := os.Executable()
		if err != nil {
			ui.Fatal("%s", err)
		}
		if _, err := api.LoadOrCreateToken(); err != nil {
			ui.Fatal("token: %s", err)
		}

		unit := fmt.Sprintf(`[Unit]
Description=Shipyard API and web UI
After=network-online.target docker.service
Wants=network-online.target

[Service]
User=%s
Environment=HOME=%s
ExecStart=%s serve --listen %s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`, u.Username, u.HomeDir, bin, serveListen)

		ui.Step("systemd", "installing shipyard.service (listen %s)", serveListen)
		tee := exec.Command("sudo", "tee", "/etc/systemd/system/shipyard.service")
		tee.Stdin = strings.NewReader(unit)
		tee.Stderr = os.Stderr
		if err := tee.Run(); err != nil {
			ui.Fatal("writing unit: %s", err)
		}
		for _, args := range [][]string{
			{"systemctl", "daemon-reload"},
			{"systemctl", "enable", "shipyard"},
			{"systemctl", "restart", "shipyard"},
		} {
			c := exec.Command("sudo", args...)
			c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
			if err := c.Run(); err != nil {
				ui.Fatal("%s: %s", strings.Join(args, " "), err)
			}
		}
		ui.Ok("shipyard serve running (token: shipyard serve token)")
	},
}

// listen serves h until ctx is done or SIGINT/SIGTERM, then shuts down.
func listen(ctx context.Context, addr string, h http.Handler) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return serveOn(ctx, ln, h)
}

func serveOn(ctx context.Context, ln net.Listener, h http.Handler) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// localRegistry exposes only this machine.
type localRegistry struct{ svc *core.Service }

func (l localRegistry) Servers() []api.ServerInfo {
	return []api.ServerInfo{{Name: api.LocalServer, Kind: "local", Default: true}}
}

func (l localRegistry) Backend(name string) (api.Backend, bool) {
	if name != api.LocalServer {
		return nil, false
	}
	return api.Local{Svc: l.svc}, true
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.AddCommand(serveTokenCmd, serveInstallCmd)
	serveCmd.PersistentFlags().StringVar(&serveListen, "listen", "0.0.0.0:"+DefaultServePort, "Address to listen on")
	serveTokenCmd.Flags().BoolVar(&serveRotate, "rotate", false, "Generate a new token (invalidates the old one)")
}
