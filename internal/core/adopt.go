package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"shipyard/internal/docker"
	"shipyard/internal/events"
	"shipyard/internal/system"
)

// AdoptPlan is how an existing container would become a shipyard service:
// the deploy request that reproduces it, what will differ (warnings) and
// what makes it impossible (blockers).
type AdoptPlan struct {
	Container string        `json:"container"`
	Request   DeployRequest `json:"request"`
	Warnings  []string      `json:"warnings"`
	Blockers  []string      `json:"blockers"`
}

// inspected is the part of `docker inspect` adoption needs.
type inspected struct {
	Name   string
	Config struct {
		Image       string
		Env         []string
		Cmd         []string
		Entrypoint  []string
		Labels      map[string]string
		Healthcheck *struct {
			Test        []string
			Interval    int64
			Timeout     int64
			Retries     int
			StartPeriod int64
		}
	}
	HostConfig struct {
		NetworkMode  string
		PortBindings map[string][]struct{ HostIp, HostPort string }
		Privileged   bool
		Devices      []json.RawMessage
		CapAdd       []string
	}
	Mounts []struct {
		Type, Name, Source, Destination string
		RW                              bool
	}
	NetworkSettings struct {
		Networks map[string]json.RawMessage
	}
}

type imageConfig struct {
	Config struct {
		Env         []string
		Cmd         []string
		Entrypoint  []string
		Healthcheck *struct{ Test []string }
	}
}

// PlanAdoption inspects a container and proposes how to adopt it.
func (s *Service) PlanAdoption(ctx context.Context, name string) (*AdoptPlan, error) {
	if err := containerExists(ctx, name); err != nil {
		return nil, err
	}
	raw, err := exec.CommandContext(ctx, "docker", "inspect", "--type", "container", "--", name).Output()
	if err != nil {
		return nil, fmt.Errorf("docker inspect: %w", err)
	}
	var cs []inspected
	if err := json.Unmarshal(raw, &cs); err != nil || len(cs) == 0 {
		return nil, fmt.Errorf("parsing docker inspect: %v", err)
	}
	var img imageConfig
	if raw, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--", cs[0].Config.Image).Output(); err == nil {
		var is []imageConfig
		if json.Unmarshal(raw, &is) == nil && len(is) > 0 {
			img = is[0]
		}
	}
	apps, err := s.List()
	if err != nil {
		return nil, err
	}
	return planFrom(cs[0], img, apps), nil
}

var hostRule = regexp.MustCompile("Host\\(`([^`]+)`\\)")

func planFrom(c inspected, img imageConfig, apps []App) *AdoptPlan {
	container := strings.TrimPrefix(c.Name, "/")
	p := &AdoptPlan{Container: container, Warnings: []string{}, Blockers: []string{}}
	req := &p.Request
	req.Image = c.Config.Image
	req.Name = sanitizeName(container)

	for _, a := range apps {
		if a.Name == req.Name {
			p.Blockers = append(p.Blockers, fmt.Sprintf("a shipyard service named %q already exists", req.Name))
		}
		if a.Name == container {
			p.Blockers = append(p.Blockers, "this container already belongs to a shipyard service")
		}
	}
	if req.Name != container {
		p.Warnings = append(p.Warnings, fmt.Sprintf("the service will be named %q (container names must be lowercase)", req.Name))
	}
	if strings.HasPrefix(c.Config.Image, "sha256:") {
		p.Blockers = append(p.Blockers, "the container runs an untagged image; tag it first")
	}

	// Networking mode.
	switch c.HostConfig.NetworkMode {
	case "host":
		p.Blockers = append(p.Blockers, "host networking is not supported by shipyard services")
	case "none":
		p.Blockers = append(p.Blockers, "containers without networking cannot be adopted")
	}
	if strings.HasPrefix(c.HostConfig.NetworkMode, "container:") {
		p.Blockers = append(p.Blockers, "the container shares another container's network")
	}
	if c.HostConfig.Privileged || len(c.HostConfig.Devices) > 0 || len(c.HostConfig.CapAdd) > 0 {
		p.Warnings = append(p.Warnings, "privileged mode, devices and added capabilities are not carried over")
	}

	// Env: only what the container adds on top of the image.
	imageEnv := map[string]bool{}
	for _, e := range img.Config.Env {
		imageEnv[e] = true
	}
	env := map[string]string{}
	for _, e := range c.Config.Env {
		if imageEnv[e] {
			continue
		}
		if k, v, ok := strings.Cut(e, "="); ok && k != "" {
			env[k] = v
		}
	}
	if len(env) > 0 {
		req.Env = env
	}

	// Command / entrypoint.
	if len(c.Config.Cmd) > 0 && !equalStrings(c.Config.Cmd, img.Config.Cmd) {
		cmd := append([]string(nil), c.Config.Cmd...)
		req.Command = &cmd
	}
	if !equalStrings(c.Config.Entrypoint, img.Config.Entrypoint) {
		p.Warnings = append(p.Warnings, "the container overrides the image entrypoint; shipyard will use the image's ("+
			strings.Join(img.Config.Entrypoint, " ")+")")
	}

	// Mounts.
	var vols []string
	for _, m := range c.Mounts {
		ro := ""
		if !m.RW {
			ro = ":ro"
		}
		switch m.Type {
		case "bind":
			vols = append(vols, m.Source+":"+m.Destination+ro)
		case "volume":
			if m.Name != "" && len(m.Name) == 64 && isHex(m.Name) {
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s uses an anonymous volume; it is kept under its id", m.Destination))
			}
			vols = append(vols, m.Name+":"+m.Destination+ro)
		default:
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s mount at %s is not carried over", m.Type, m.Destination))
		}
	}
	sort.Strings(vols)
	if len(vols) > 0 {
		req.Volumes = &vols
	}

	// Traefik labels → traefik mode; otherwise published ports.
	var domain, port, entry, resolver string
	tls := false
	for k, v := range c.Config.Labels {
		switch {
		case strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".rule"):
			if m := hostRule.FindStringSubmatch(v); m != nil && domain == "" {
				domain = m[1]
			}
		case strings.HasPrefix(k, "traefik.http.services.") && strings.HasSuffix(k, ".loadbalancer.server.port"):
			port = v
		case strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".entrypoints"):
			entry = strings.Split(v, ",")[0]
		case strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".tls"):
			tls = tls || v == "true"
		case strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".tls.certresolver"):
			resolver, tls = v, true
		}
	}
	var nets []string
	for n := range c.NetworkSettings.Networks {
		if n != "bridge" && n != "host" && n != "none" {
			nets = append(nets, n)
		}
	}
	sort.Strings(nets)

	if domain != "" {
		mode := "traefik"
		req.Mode = &mode
		req.Domain = &domain
		if port != "" {
			req.InternalPort = &port
		}
		if entry != "" {
			req.Entrypoint = &entry
		}
		if tls {
			req.TLS = &tls
		}
		if resolver != "" {
			req.CertResolver = &resolver
		}
		if len(nets) > 0 {
			n := nets[0]
			req.Network = &n
			if len(nets) > 1 {
				p.Warnings = append(p.Warnings, "only network "+n+" is kept; also attached to: "+strings.Join(nets[1:], ", "))
			}
		}
		if len(c.HostConfig.PortBindings) > 0 {
			p.Warnings = append(p.Warnings, "published ports are dropped in Traefik mode")
		}
	} else {
		mode := "local"
		req.Mode = &mode
		var ports []string
		for cport, binds := range c.HostConfig.PortBindings {
			target, proto, _ := strings.Cut(cport, "/")
			for _, b := range binds {
				if b.HostPort == "" {
					continue
				}
				m := b.HostPort + ":" + target
				if b.HostIp != "" && b.HostIp != "0.0.0.0" && b.HostIp != "::" {
					m = b.HostIp + ":" + m
				}
				if proto != "" && proto != "tcp" {
					m += "/" + proto
				}
				ports = append(ports, m)
			}
		}
		sort.Strings(ports)
		ports = dedupe(ports)
		if len(ports) > 0 {
			req.Ports = &ports
		}
		if len(nets) > 0 {
			p.Warnings = append(p.Warnings, "local-port services are not attached to custom networks; it will leave: "+strings.Join(nets, ", "))
		}
	}

	// Healthcheck defined on the container (not inherited from the image).
	if hc := c.Config.Healthcheck; hc != nil && len(hc.Test) > 1 && hc.Test[0] != "NONE" &&
		(img.Config.Healthcheck == nil || !equalStrings(hc.Test, img.Config.Healthcheck.Test)) {
		cmd := strings.Join(hc.Test[1:], " ")
		req.HealthcheckCmd = &cmd
		if hc.Interval > 0 {
			v := time.Duration(hc.Interval).String()
			req.HealthcheckInterval = &v
		}
		if hc.Timeout > 0 {
			v := time.Duration(hc.Timeout).String()
			req.HealthcheckTimeout = &v
		}
		if hc.Retries > 0 {
			r := hc.Retries
			req.HealthcheckRetries = &r
		}
		if hc.StartPeriod > 0 {
			v := time.Duration(hc.StartPeriod).String()
			req.HealthcheckStartPeriod = &v
		}
	}

	if proj := c.Config.Labels["com.docker.compose.project"]; proj != "" {
		p.Warnings = append(p.Warnings, fmt.Sprintf("it belongs to the compose project %q: its other containers stay as they are, and `docker compose` for that project will no longer manage this one", proj))
	}
	return p
}

// Adopt replaces a container with an equivalent shipyard service. The
// original is renamed and stopped first; if the new deployment fails it is
// renamed back and started again, so a failed adoption changes nothing.
func (s *Service) Adopt(ctx context.Context, container string, req DeployRequest, p events.Progress) error {
	plan, err := s.PlanAdoption(ctx, container)
	if err != nil {
		return err
	}
	if len(plan.Blockers) > 0 {
		return fmt.Errorf("cannot adopt %s: %s", container, strings.Join(plan.Blockers, "; "))
	}
	if req.Repo != "" {
		return errors.New("adoption deploys the container's image, not a repository")
	}
	name := DeployName(req)
	root := req.Root
	if root == "" {
		root = DefaultRoot
	}
	dir := filepath.Join(root, name)
	_, statErr := os.Stat(dir)
	dirExisted := statErr == nil

	parked := container + "-pre-shipyard"
	w := p.Writer()
	p.Step("park", "stopping %s (kept as %s until the service is up)", container, parked)
	if err := system.Run(ctx, w, "docker", "rename", "--", container, parked); err != nil {
		return fmt.Errorf("docker rename: %w", err)
	}
	restore := func(cause error) error {
		p.Warn("adoption failed, restoring %s", container)
		bg := context.Background() // restore even if the job was canceled
		if name != parked {
			_ = docker.RemoveContainer(bg, name, w)
		}
		if !dirExisted {
			_ = os.RemoveAll(dir)
		}
		if err := system.Run(bg, w, "docker", "rename", "--", parked, container); err != nil {
			return fmt.Errorf("%w (and restoring the original failed: %v; it is %s)", cause, err, parked)
		}
		if err := system.Run(bg, w, "docker", "start", "--", container); err != nil {
			return fmt.Errorf("%w (original restored but not started: %v)", cause, err)
		}
		return cause
	}
	if err := system.Run(ctx, w, "docker", "stop", "--", parked); err != nil {
		return restore(fmt.Errorf("docker stop: %w", err))
	}

	if _, err := s.Deploy(ctx, req, p); err != nil {
		return restore(err)
	}

	p.Step("clean", "removing the original container")
	if err := system.Run(ctx, w, "docker", "rm", "--", parked); err != nil {
		p.Warn("could not remove %s: %s (remove it by hand)", parked, err)
	}
	p.Ok("%s is now the shipyard service %s", container, name)
	return nil
}

var invalidNameChars = regexp.MustCompile(`[^a-z0-9_.-]+`)

func sanitizeName(s string) string {
	s = invalidNameChars.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.TrimLeft(s, "-_.")
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func dedupe(xs []string) []string {
	out := xs[:0]
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
