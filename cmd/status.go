package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"shipyard/internal/deployment"
	"shipyard/internal/state"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

// dockerInspect mirrors the fields we need from `docker inspect`.
type dockerInspect struct {
	State struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status        string `json:"Status"`
			FailingStreak int    `json:"FailingStreak"`
		} `json:"Health"`
	} `json:"State"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

// dockerStats mirrors the fields we need from `docker stats --no-stream`.
type dockerStats struct {
	MemUsage string `json:"MemUsage"`
	CPUPerc  string `json:"CPUPerc"`
}

var statusCmd = &cobra.Command{
	Use:   "status [app]",
	Short: "Show runtime status of a deployed service",
	Args:  appArg,

	Run: func(cmd *cobra.Command, args []string) {

		appName := resolveApp(cmd, args, "Show status of which service?")

		// =========================
		// LOAD STATE
		// =========================

		st, err := state.Load()
		if err != nil {
			ui.Fatal("state: %s", err)
		}

		app, exists := st.Apps[appName]
		if !exists {
			ui.Fatal("app not found: %s", appName)
		}

		ui.Section("status")

		// =========================
		// DOCKER INSPECT
		// =========================

		inspectOut, err := exec.Command(
			"docker", "inspect",
			"--format", "{{json .}}",
			"--", appName,
		).Output()

		if err != nil {
			ui.Error("container not found — is it running?")
			ui.Detail("app", appName)
			ui.Detail("path", app.Path)
			ui.Detail("image", app.Image)
			return
		}

		var inspect dockerInspect
		if err := json.Unmarshal(inspectOut, &inspect); err != nil {
			ui.Fatal("parsing inspect output: %s", err)
		}

		// =========================
		// DOCKER STATS
		// =========================

		statsOut, err := exec.Command(
			"docker", "stats",
			"--no-stream",
			"--format", `{"CPUPerc":"{{.CPUPerc}}","MemUsage":"{{.MemUsage}}"}`,
			appName,
		).Output()

		var stats dockerStats
		if err == nil {
			_ = json.Unmarshal(statsOut, &stats)
		}

		// =========================
		// FORMAT UPTIME
		// =========================

		uptime := "-"
		if inspect.State.Running {
			t, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt)
			if err == nil {
				uptime = formatDuration(time.Since(t))
			}
		}

		// =========================
		// FORMAT PORTS
		// =========================

		portList := []string{}
		for containerPort, bindings := range inspect.NetworkSettings.Ports {
			for _, b := range bindings {
				if b.HostPort != "" {
					portList = append(portList, fmt.Sprintf("%s -> %s", b.HostPort, containerPort))
				}
			}
		}

		// =========================
		// PRINT
		// =========================

		statusLabel := inspect.State.Status
		if inspect.State.Running {
			statusLabel = ui.Colorize("green", statusLabel)
		} else {
			statusLabel = ui.Colorize("red", statusLabel)
		}

		fmt.Println()
		ui.Detail("app", appName)
		ui.Detail("status", statusLabel)
		if h := inspect.State.Health; h != nil {
			health := h.Status
			switch h.Status {
			case "healthy":
				health = ui.Colorize("green", health)
			case "unhealthy":
				health = ui.Colorize("red", fmt.Sprintf("%s (failing streak %d)", health, h.FailingStreak))
			default:
				health = ui.Colorize("yellow", health)
			}
			ui.Detail("health", health)
		} else {
			ui.Detail("health", "no healthcheck")
		}
		ui.Detail("uptime", uptime)
		ui.Detail("image", app.Image)
		ui.Detail("mode", app.Mode)

		if app.Domain != "" {
			ui.Detail("domain", app.Domain)
		}
		if app.Network != "" {
			ui.Detail("network", app.Network)
		}

		// Settings from deployment.yaml (never values of env vars).
		if cfg, err := deployment.Load(app.Path); err == nil {
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
		if len(portList) > 0 {
			ui.Detail("ports", strings.Join(portList, ", "))
		}
		if stats.CPUPerc != "" {
			ui.Detail("cpu", stats.CPUPerc)
		}
		if stats.MemUsage != "" {
			ui.Detail("mem", stats.MemUsage)
		}

		ui.Detail("path", app.Path)
		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

// formatDuration converts a duration to a human-readable string.
// Examples: "3d 2h", "45m 10s", "5h 30m"
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	parts := []string{}

	if days > 0 {
		parts = append(parts, strconv.Itoa(days)+"d")
	}
	if hours > 0 {
		parts = append(parts, strconv.Itoa(hours)+"h")
	}
	if minutes > 0 && days == 0 {
		parts = append(parts, strconv.Itoa(minutes)+"m")
	}
	if seconds > 0 && days == 0 && hours == 0 {
		parts = append(parts, strconv.Itoa(seconds)+"s")
	}

	if len(parts) == 0 {
		return "just started"
	}

	return strings.Join(parts, " ")
}
