package cmd

import (
	"fmt"
	"strings"
	"time"

	"shipyard/internal/core"
	"shipyard/internal/deployment"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [app]",
	Short: "Show runtime status of a deployed service",
	Args:  appArg,

	Run: func(cmd *cobra.Command, args []string) {
		appName := resolveApp(cmd, args, "Show status of which service?")

		s, err := core.New().Status(cmd.Context(), appName)
		if err != nil {
			ui.Fatal("%s", err)
		}

		ui.Section("status")

		if !s.Container {
			ui.Error("container not found — is it running?")
			ui.Detail("app", s.Name)
			ui.Detail("path", s.Path)
			ui.Detail("image", s.Image)
			return
		}

		statusLabel := ui.Colorize("red", s.State)
		if s.Running {
			statusLabel = ui.Colorize("green", s.State)
		}
		uptime := "-"
		if s.StartedAt != nil {
			uptime = core.FormatDuration(time.Since(*s.StartedAt))
		}

		fmt.Println()
		ui.Detail("app", s.Name)
		ui.Detail("status", statusLabel)
		switch s.Health {
		case "":
			ui.Detail("health", "no healthcheck")
		case "healthy":
			ui.Detail("health", ui.Colorize("green", s.Health))
		case "unhealthy":
			ui.Detail("health", ui.Colorize("red", fmt.Sprintf("%s (failing streak %d)", s.Health, s.FailingStreak)))
		default:
			ui.Detail("health", ui.Colorize("yellow", s.Health))
		}
		ui.Detail("uptime", uptime)
		ui.Detail("image", s.Image)
		ui.Detail("mode", s.Mode)
		if s.Domain != "" {
			ui.Detail("domain", s.Domain)
		}
		if s.Network != "" {
			ui.Detail("network", s.Network)
		}

		// Settings from deployment.yaml (never values of env vars).
		if cfg := s.Config; cfg != nil {
			ui.Detail("source", cfg.Source.Type)
			if cfg.Build.Platform != "" {
				ui.Detail("platform", cfg.Build.Platform)
			}
			if cfg.Deploy.Mode == deployment.ModeTraefik {
				ep := cfg.Deploy.Entrypoint
				if cfg.Deploy.TLS {
					ep += " (tls)"
				}
				ui.Detail("entrypoint", ep)
			}
			if len(cfg.Env.Files) > 0 {
				ui.Detail("env files", strings.Join(cfg.Env.Files, ", "))
			}
			if len(cfg.Env.Keys) > 0 {
				ui.Detail("env keys", strings.Join(cfg.Env.Keys, ", "))
			}
		} else {
			ui.Detail("config", "no deployment.yaml (run shipyard update to migrate)")
		}
		if len(s.Ports) > 0 {
			ui.Detail("ports", strings.Join(s.Ports, ", "))
		}
		if s.CPU != "" {
			ui.Detail("cpu", s.CPU)
		}
		if s.Memory != "" {
			ui.Detail("mem", s.Memory)
		}
		ui.Detail("path", s.Path)
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
