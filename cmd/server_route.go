package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"shipyard/internal/api"
	"shipyard/internal/config"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var (
	routeHeaders  []string
	routeCFID     string
	routeCFSecret string
	routeFirst    bool
)

var serverRouteCmd = &cobra.Command{
	Use:   "route",
	Short: "Manage the ways to reach a server's API (failover)",
	Long: `A server can be reached through several routes, tried in order: for
example a public URL behind Cloudflare, then a VPN address, then the LAN.
When one is unreachable (an ISP blocking Cloudflare IPs, being away from
home…) the web UI moves to the next and returns to the preferred one when
it works again. An SSH tunnel is always the last resort unless disabled
with no_ssh_fallback in ~/.shipyard.yaml.`,
}

var serverRouteListCmd = &cobra.Command{
	Use:   "list [server]",
	Short: "Show the routes of a server and test each one",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, name, srv := routeServer(args)
		routes := srv.EffectiveRoutes()
		if len(routes) == 0 {
			ui.Info("%s has no API routes (enable the web UI with: shipyard server add %s)", name, name)
			return
		}
		for i, r := range routes {
			rt, closeFn := buildRoute(srv, r)
			b := api.Remote{Token: srv.Token, Routes: api.NewRoutes(rt)}
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			start := time.Now()
			info, err := b.Info(ctx)
			cancel()
			closeFn()
			label := fmt.Sprintf("%d. %-11s %s", i+1, r.Name, describeRoute(r))
			if err != nil {
				ui.CheckFail(label, err.Error())
				continue
			}
			ui.CheckOk(label, fmt.Sprintf("%s · shipyard %s", time.Since(start).Round(time.Millisecond), info.Version))
		}
	},
}

var serverRouteAddCmd = &cobra.Command{
	Use:   "add <server> <name> <url>",
	Short: "Add a route (appended last unless --first)",
	Example: `  shipyard server route add pi cloudflare https://shipyard.example.com --first \
      --cf-client-id XXX.access --cf-client-secret YYY
  shipyard server route add pi tailscale http://pi.tailnet-name.ts.net:7878`,
	Args: cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		cfg, name, srv := routeServer(args[:1])
		rname, raw := args[1], args[2]
		if rname == config.SSHRoute {
			ui.Fatal("%q is reserved for the automatic SSH tunnel", rname)
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			ui.Fatal("invalid URL %q: expected http(s)://host[:port]", raw)
		}
		r := config.Route{Name: rname, URL: strings.TrimRight(raw, "/"), Headers: map[string]string{}}
		for _, h := range routeHeaders {
			k, v, ok := strings.Cut(h, "=")
			if !ok || k == "" {
				ui.Fatal("invalid --header %q: expected Name=value", h)
			}
			r.Headers[k] = v
		}
		if routeCFID != "" || routeCFSecret != "" {
			if routeCFID == "" || routeCFSecret == "" {
				ui.Fatal("--cf-client-id and --cf-client-secret go together")
			}
			r.Headers["CF-Access-Client-Id"] = routeCFID
			r.Headers["CF-Access-Client-Secret"] = routeCFSecret
		}
		if len(r.Headers) == 0 {
			r.Headers = nil
		}

		// The first explicit route materializes the implicit LAN one.
		if len(srv.Routes) == 0 && srv.URL != "" {
			srv.Routes = []config.Route{{Name: "lan", URL: srv.URL}}
		}
		for _, x := range srv.Routes {
			if x.Name == rname {
				ui.Fatal("%s already has a route named %s (remove it first)", name, rname)
			}
		}
		if routeFirst {
			srv.Routes = append([]config.Route{r}, srv.Routes...)
		} else {
			srv.Routes = append(srv.Routes, r)
		}

		// Check it before saving, but save anyway: a route may only work
		// from elsewhere (a VPN that is off, being at home…).
		rt, closeFn := buildRoute(srv, r)
		b := api.Remote{Token: srv.Token, Routes: api.NewRoutes(rt)}
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		info, err := b.Info(ctx)
		cancel()
		closeFn()
		if err != nil {
			ui.Warn("%s does not answer from here right now: %s", r.URL, err)
		} else {
			ui.CheckOk(rname, "shipyard "+info.Version)
		}

		cfg.Servers[name] = srv
		if err := config.Save(cfg); err != nil {
			ui.Fatal("saving config: %s", err)
		}
		ui.Ok("route %s added to %s", rname, name)
		printOrder(srv)
	},
}

var serverRouteRemoveCmd = &cobra.Command{
	Use:   "remove <server> <name>",
	Short: "Remove a route",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		cfg, name, srv := routeServer(args[:1])
		var kept []config.Route
		for _, r := range srv.Routes {
			if r.Name != args[1] {
				kept = append(kept, r)
			}
		}
		if len(kept) == len(srv.Routes) {
			ui.Fatal("%s has no route named %s", name, args[1])
		}
		srv.Routes = kept
		cfg.Servers[name] = srv
		if err := config.Save(cfg); err != nil {
			ui.Fatal("saving config: %s", err)
		}
		ui.Ok("route %s removed from %s", args[1], name)
		printOrder(srv)
	},
}

func routeServer(args []string) (*config.Config, string, config.Server) {
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		ui.Fatal("config: %s", err)
	}
	name := ""
	if len(args) > 0 {
		name = args[0]
	} else {
		name = pickServer(cfg, "Server", false)
	}
	srv, ok := cfg.Servers[name]
	if !ok {
		ui.Fatal("unknown server %s (see shipyard server list)", name)
	}
	if srv.Token == "" {
		ui.Fatal("%s has no API token: enable the web UI with shipyard server add %s", name, name)
	}
	return cfg, name, srv
}

func describeRoute(r config.Route) string {
	if r.Name == config.SSHRoute && r.URL == "" {
		return "(ssh tunnel)"
	}
	s := r.URL
	if r.Headers["CF-Access-Client-Id"] != "" {
		s += " (Cloudflare Access)"
	}
	return s
}

func printOrder(srv config.Server) {
	var names []string
	for _, r := range srv.EffectiveRoutes() {
		names = append(names, r.Name)
	}
	ui.Info("order: %s", strings.Join(names, " → "))
}

func init() {
	serverCmd.AddCommand(serverRouteCmd)
	serverRouteCmd.AddCommand(serverRouteListCmd, serverRouteAddCmd, serverRouteRemoveCmd)
	serverRouteAddCmd.Flags().StringArrayVar(&routeHeaders, "header", nil, "Extra request header Name=value (repeatable)")
	serverRouteAddCmd.Flags().StringVar(&routeCFID, "cf-client-id", "", "Cloudflare Access service token client ID")
	serverRouteAddCmd.Flags().StringVar(&routeCFSecret, "cf-client-secret", "", "Cloudflare Access service token client secret")
	serverRouteAddCmd.Flags().BoolVar(&routeFirst, "first", false, "Make it the preferred route")
}
