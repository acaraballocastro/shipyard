// Package core implements shipyard's operations on services (deploy,
// update, status, logs…) independently of how they are triggered. The CLI
// and the HTTP API are thin layers over it: core never prints, never exits
// and reports progress through events.Progress.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"shipyard/internal/deployment"
	"shipyard/internal/docker"
	"shipyard/internal/events"
	"shipyard/internal/state"
	"shipyard/internal/system"
	"shipyard/internal/update"
)

// DefaultRoot is where services are deployed unless told otherwise.
const DefaultRoot = "/srv/apps"

// ErrNotFound is returned (wrapped) when a service does not exist.
var ErrNotFound = errors.New("app not found")

// Service runs operations against the local Docker host.
type Service struct {
	Runner update.Runner
}

// New returns a Service using the real git and docker implementations.
func New() *Service {
	return &Service{Runner: update.DefaultRunner}
}

// App is a deployed service as registered in state.
type App struct {
	Name    string `json:"name"`
	Repo    string `json:"repo,omitempty"`
	Image   string `json:"image"`
	Mode    string `json:"mode"`
	Domain  string `json:"domain,omitempty"`
	Network string `json:"network,omitempty"`
	Path    string `json:"path"`
}

// List returns the deployed services sorted by name.
func (s *Service) List() ([]App, error) {
	st, err := state.Load()
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	apps := make([]App, 0, len(st.Apps))
	for name, a := range st.Apps {
		apps = append(apps, App{Name: name, Repo: a.Repo, Image: a.Image, Mode: a.Mode,
			Domain: a.Domain, Network: a.Network, Path: a.Path})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps, nil
}

func lookup(name string) (state.App, error) {
	st, err := state.Load()
	if err != nil {
		return state.App{}, fmt.Errorf("state: %w", err)
	}
	app, ok := st.Apps[name]
	if !ok {
		return state.App{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return app, nil
}

// =========================
// STATUS
// =========================

// Status is the runtime and configuration view of a service.
type Status struct {
	App
	// Container is false when no container exists (never started, removed).
	Container     bool               `json:"container"`
	State         string             `json:"state,omitempty"`
	Running       bool               `json:"running"`
	Health        string             `json:"health,omitempty"`
	FailingStreak int                `json:"failingStreak,omitempty"`
	StartedAt     *time.Time         `json:"startedAt,omitempty"`
	Ports         []string           `json:"ports,omitempty"`
	CPU           string             `json:"cpu,omitempty"`
	Memory        string             `json:"memory,omitempty"`
	Config        *deployment.Config `json:"config,omitempty"`
}

// Status inspects a service. A missing container is not an error.
func (s *Service) Status(ctx context.Context, name string) (*Status, error) {
	app, err := lookup(name)
	if err != nil {
		return nil, err
	}
	out := &Status{App: App{Name: name, Repo: app.Repo, Image: app.Image, Mode: app.Mode,
		Domain: app.Domain, Network: app.Network, Path: app.Path}}
	if cfg, err := deployment.Load(app.Path); err == nil {
		out.Config = cfg
	}

	raw, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .}}", "--", name).Output()
	if err != nil {
		return out, nil
	}
	var inspect struct {
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
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal(raw, &inspect); err != nil {
		return nil, fmt.Errorf("parsing inspect output: %w", err)
	}

	out.Container = true
	out.State = inspect.State.Status
	out.Running = inspect.State.Running
	if h := inspect.State.Health; h != nil {
		out.Health = h.Status
		out.FailingStreak = h.FailingStreak
	}
	if out.Running {
		if t, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
			out.StartedAt = &t
		}
	}
	for port, bindings := range inspect.NetworkSettings.Ports {
		for _, b := range bindings {
			if b.HostPort != "" {
				out.Ports = append(out.Ports, b.HostPort+" -> "+port)
			}
		}
	}
	sort.Strings(out.Ports)

	if out.Running {
		stats, err := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format",
			`{"CPUPerc":"{{.CPUPerc}}","MemUsage":"{{.MemUsage}}"}`, name).Output()
		if err == nil {
			var st struct{ CPUPerc, MemUsage string }
			if json.Unmarshal(stats, &st) == nil {
				out.CPU, out.Memory = st.CPUPerc, st.MemUsage
			}
		}
	}
	return out, nil
}

// =========================
// ACTIONS
// =========================

// Restart restarts the containers of a service.
func (s *Service) Restart(ctx context.Context, name string, p events.Progress) error {
	app, err := lookup(name)
	if err != nil {
		return err
	}
	p.Step("restart", "restarting %s", name)
	if err := system.Run(ctx, p.Writer(), "docker", "compose", "-f", composeFile(app), "restart"); err != nil {
		return fmt.Errorf("docker compose restart: %w", err)
	}
	p.Ok("%s restarted", name)
	return nil
}

// Update rebuilds (git) or re-pulls (image) a service with its saved settings.
func (s *Service) Update(ctx context.Context, name string, p events.Progress) error {
	if _, err := lookup(name); err != nil {
		return err
	}
	p.Info("updating %s", name)
	if err := s.Runner.Update(ctx, name, p); err != nil {
		return err
	}
	p.Ok("%s updated", name)
	return nil
}

// Delete stops a service and removes its files and state entry. Named
// volumes and bind-mounted data are kept.
func (s *Service) Delete(ctx context.Context, name string, p events.Progress) error {
	app, err := lookup(name)
	if err != nil {
		return err
	}
	p.Step("down", "stopping containers")
	if _, err := os.Stat(app.Path); err == nil {
		if err := docker.ComposeDown(ctx, app.Path, p.Writer()); err != nil {
			return fmt.Errorf("docker compose down: %w", err)
		}
	} else {
		// Directory removed by hand: stop the project by name and remove
		// any leftover container, so the service can still be deleted.
		p.Warn("%s is missing; removing the containers by name", app.Path)
		if err := docker.ComposeDownProject(ctx, filepath.Base(app.Path), p.Writer()); err != nil {
			p.Warn("docker compose -p %s down: %s", filepath.Base(app.Path), err)
		}
		if err := docker.RemoveContainer(ctx, name, p.Writer()); err != nil {
			return fmt.Errorf("removing container %s: %w", name, err)
		}
	}
	p.Step("clean", "removing files")
	if err := os.RemoveAll(app.Path); err != nil {
		return fmt.Errorf("removing directory: %w", err)
	}
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}
	delete(st.Apps, name)
	if err := state.Save(st); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}
	p.Ok("%s removed", name)
	return nil
}

// LogsOptions controls Logs.
type LogsOptions struct {
	Follow bool
	// Tail limits the initial lines; 0 means all.
	Tail int
}

// Logs writes the logs of a service to out until done or ctx is cancelled.
func (s *Service) Logs(ctx context.Context, name string, opts LogsOptions, out io.Writer) error {
	app, err := lookup(name)
	if err != nil {
		return err
	}
	args := []string{"compose", "-f", composeFile(app), "logs", "--no-color"}
	if opts.Follow {
		args = append(args, "-f")
	}
	if opts.Tail > 0 {
		args = append(args, "--tail", strconv.Itoa(opts.Tail))
	}
	err = system.Run(ctx, out, "docker", args...)
	if ctx.Err() != nil {
		return nil // stopped by the caller
	}
	if err != nil {
		return fmt.Errorf("docker compose logs: %w", err)
	}
	return nil
}

// Cleanup removes directories under root that are not registered services.
func (s *Service) Cleanup(root string, p events.Progress) ([]string, error) {
	st, err := state.Load()
	if err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}
	var removed []string
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := st.Apps[e.Name()]; ok {
			continue
		}
		path := filepath.Join(root, e.Name())
		p.Step("remove", "%s", e.Name())
		p.Detail("path", path)
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", e.Name(), err))
			continue
		}
		removed = append(removed, e.Name())
		p.Ok("%s removed", e.Name())
	}
	return removed, errors.Join(errs...)
}

func composeFile(app state.App) string {
	return filepath.Join(app.Path, deployment.ComposeFileName)
}

// FormatDuration renders an uptime: "3d 2h", "45m 10s", "5h 30m".
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	var parts []string
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
