package cmd

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"

	"shipyard/internal/api"
	"shipyard/internal/config"
	"shipyard/internal/core"
	"shipyard/internal/ui"
	"shipyard/web"

	"github.com/spf13/cobra"
)

var uiPort int
var uiNoOpen bool

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the web UI for this machine and your servers",
	Long: `Starts the web UI on localhost and opens it in the browser. It shows
this machine and every server added with "shipyard server add" that has
its API enabled. Server tokens stay in this process; the browser only
talks to localhost.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", uiPort))
		if err != nil {
			ui.Fatal("%s", err)
		}
		token := api.NewToken() // per run: only this browser session gets in
		h := api.Handler(api.Options{
			Registry:     gatewayRegistry{svc: core.New()},
			Token:        token,
			UI:           web.FS(),
			LoopbackOnly: true,
		})

		url := fmt.Sprintf("http://%s/#token=%s", ln.Addr(), token)
		ui.Ok("shipyard ui running at http://%s (Ctrl+C to stop)", ln.Addr())
		if !uiNoOpen {
			openBrowser(url)
		} else {
			ui.Info("open: %s", url)
		}
		if err := serveOn(cmd.Context(), ln, h); err != nil {
			ui.Fatal("%s", err)
		}
	},
}

// gatewayRegistry is this machine plus the servers with an API URL.
// The config is read on every call, so servers added meanwhile appear.
type gatewayRegistry struct{ svc *core.Service }

func (g gatewayRegistry) Servers() []api.ServerInfo {
	cfg, _ := config.LoadOrEmpty()
	out := []api.ServerInfo{{Name: api.LocalServer, Kind: "local", Default: cfg.DefaultServer == ""}}
	for _, n := range sortedServerNames(cfg.Servers) {
		s := cfg.Servers[n]
		if s.URL == "" {
			continue
		}
		out = append(out, api.ServerInfo{Name: n, Kind: "remote", URL: s.URL, Default: cfg.DefaultServer == n})
	}
	return out
}

func (g gatewayRegistry) Backend(name string) (api.Backend, bool) {
	if name == api.LocalServer {
		return api.Local{Svc: g.svc}, true
	}
	cfg, _ := config.LoadOrEmpty()
	s, ok := cfg.Servers[name]
	if !ok || s.URL == "" {
		return nil, false
	}
	return api.Remote{URL: s.URL, Token: s.Token}, true
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	if err := c.Start(); err != nil {
		ui.Info("open: %s", url)
	}
}

func init() {
	rootCmd.AddCommand(uiCmd)
	uiCmd.Flags().IntVar(&uiPort, "port", 0, "Port on 127.0.0.1 (default: random free port)")
	uiCmd.Flags().BoolVar(&uiNoOpen, "no-open", false, "Do not open the browser")
}
