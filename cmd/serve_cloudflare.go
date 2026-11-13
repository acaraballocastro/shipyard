package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"shipyard/internal/api"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// serveConfig is ~/.shipyard/serve.yaml: settings of `shipyard serve`
// that must survive reinstalls and upgrades.
type serveConfig struct {
	Cloudflare *cloudflareConfig `yaml:"cloudflare,omitempty"`
}

type cloudflareConfig struct {
	// Team is <team>.cloudflareaccess.com (or just <team>).
	Team string `yaml:"team"`
	// Audience is the Access application's AUD tag.
	Audience string `yaml:"audience"`
	// Login lets verified Access users in without the shipyard token.
	Login bool `yaml:"login,omitempty"`
	// AllowedEmails restricts Login (empty: whoever Access lets through).
	AllowedEmails []string `yaml:"allowed_emails,omitempty"`
	// Hostname is informational (shown in instructions).
	Hostname string `yaml:"hostname,omitempty"`
}

func serveConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".shipyard", "serve.yaml")
}

func loadServeConfig() (*serveConfig, error) {
	b, err := os.ReadFile(serveConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return &serveConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c serveConfig
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", serveConfigPath(), err)
	}
	return &c, nil
}

func saveServeConfig(c *serveConfig) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(serveConfigPath()), 0700); err != nil {
		return err
	}
	return os.WriteFile(serveConfigPath(), b, 0600)
}

// applyServeConfig fills the Cloudflare options of the API handler.
func applyServeConfig(o *api.Options) error {
	c, err := loadServeConfig()
	if err != nil {
		return err
	}
	if cf := c.Cloudflare; cf != nil {
		o.Access = api.NewAccessVerifier(cf.Team, cf.Audience)
		o.AccessLogin = cf.Login
		o.AllowedEmails = cf.AllowedEmails
		mode := "Access token + shipyard token"
		if cf.Login {
			mode = "Access sign-in (no shipyard token in the browser)"
		}
		ui.Info("cloudflare access: %s, audience %s… (%s)", o.Access.TeamDomain, short(cf.Audience), mode)
	}
	return nil
}

func short(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

var (
	cfTeam     string
	cfAudience string
	cfLogin    bool
	cfAllow    []string
	cfHostname string
	cfDisable  bool
)

var serveCloudflareCmd = &cobra.Command{
	Use:   "cloudflare",
	Short: "Protect shipyard serve behind Cloudflare Tunnel + Access",
	Long: `Makes shipyard serve verify the Cloudflare Access token of every request
that arrives through Cloudflare, so the public hostname only works through
your Access policy, even if it is misconfigured. Requests over the LAN or
an SSH tunnel are unaffected (they still need the shipyard token), which
keeps them usable as fallback routes when Cloudflare is unreachable.

Find the values in Cloudflare Zero Trust:
  --team  Settings → Custom Pages → Team domain (<team>.cloudflareaccess.com)
  --aud   Access → Applications → your app → Overview → Application Audience (AUD) Tag`,
	Example: `  shipyard serve cloudflare --team myteam --aud 4714c1358e65fe4b... --hostname shipyard.example.com
  shipyard serve cloudflare --team myteam --aud 4714... --login --allow me@example.com
  shipyard serve cloudflare --disable`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		c, err := loadServeConfig()
		if err != nil {
			ui.Fatal("%s", err)
		}
		if cfDisable {
			c.Cloudflare = nil
			if err := saveServeConfig(c); err != nil {
				ui.Fatal("saving %s: %s", serveConfigPath(), err)
			}
			ui.Ok("cloudflare access verification disabled")
			restartServe()
			return
		}
		if cfTeam == "" || cfAudience == "" {
			ui.Fatal("--team and --aud are required (see --help for where to find them)")
		}
		v := api.NewAccessVerifier(cfTeam, cfAudience)
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		err = v.Check(ctx)
		cancel()
		if err != nil {
			ui.Fatal("cannot read the Access keys of %s: %s", v.TeamDomain, err)
		}
		ui.CheckOk("team", v.TeamDomain)

		c.Cloudflare = &cloudflareConfig{Team: cfTeam, Audience: cfAudience, Login: cfLogin,
			AllowedEmails: cfAllow, Hostname: cfHostname}
		if err := saveServeConfig(c); err != nil {
			ui.Fatal("saving %s: %s", serveConfigPath(), err)
		}
		ui.Ok("saved %s", serveConfigPath())
		if cfLogin && len(cfAllow) == 0 {
			ui.Warn("--login without --allow: anyone your Access policy admits gets full control of this server")
		}
		restartServe()
		printTunnelHelp(cfHostname)
	},
}

// restartServe restarts the systemd service when installed, so the new
// settings apply.
func restartServe() {
	if _, err := exec.LookPath("systemctl"); err != nil {
		ui.Info("restart shipyard serve to apply")
		return
	}
	if exec.Command("systemctl", "is-enabled", "--quiet", "shipyard").Run() != nil {
		ui.Info("restart shipyard serve to apply")
		return
	}
	c := exec.Command("sudo", "systemctl", "restart", "shipyard")
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		ui.Warn("restart failed (%s): run sudo systemctl restart shipyard", err)
		return
	}
	ui.Ok("shipyard serve restarted")
}

func printTunnelHelp(hostname string) {
	if hostname == "" {
		hostname = "shipyard.example.com"
	}
	fmt.Println()
	ui.Info("in your cloudflared config (or the tunnel's Public Hostnames in Zero Trust), point the hostname here:")
	fmt.Printf(`
  ingress:
    - hostname: %s
      service: http://localhost:%s
    # … your other services …
    - service: http_status:404

`, hostname, DefaultServePort)
	ui.Info("then, on your workstation, add it as the preferred route (service token from Access → Service Auth):")
	fmt.Printf(`
  shipyard server route add <server> cloudflare https://%s --first \
      --cf-client-id <id>.access --cf-client-secret <secret>

`, hostname)
	ui.Info("and add the service token to the Access application's policy (action: Service Auth).")
}

func init() {
	serveCmd.AddCommand(serveCloudflareCmd)
	f := serveCloudflareCmd.Flags()
	f.StringVar(&cfTeam, "team", "", "Zero Trust team name or <team>.cloudflareaccess.com")
	f.StringVar(&cfAudience, "aud", "", "Access application AUD tag")
	f.BoolVar(&cfLogin, "login", false, "Let Access-verified users in without the shipyard token (browser)")
	f.StringArrayVar(&cfAllow, "allow", nil, "Email allowed to sign in with --login (repeatable)")
	f.StringVar(&cfHostname, "hostname", "", "Public hostname of shipyard (for the printed instructions)")
	f.BoolVar(&cfDisable, "disable", false, "Remove the Cloudflare settings")
}
