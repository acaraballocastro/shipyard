package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"shipyard/internal/system"
)

// Container is any Docker container on the host, managed by shipyard or not.
type Container struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`  // running, exited, restarting, paused, created…
	Status  string `json:"status"` // "Up 3 hours (healthy)", "Exited (0) 2 days ago"
	Ports   string `json:"ports,omitempty"`
	Project string `json:"project,omitempty"` // docker compose project
	// Managed is true for containers of a shipyard service.
	Managed bool `json:"managed"`
	// App is the shipyard service it belongs to, when managed.
	App string `json:"app,omitempty"`
}

// ErrContainerNotFound is returned (wrapped) for unknown containers.
var ErrContainerNotFound = errors.New("container not found")

// Containers lists every container on the host (running or not), sorted
// by name, marking those that belong to shipyard services.
func (s *Service) Containers(ctx context.Context) ([]Container, error) {
	apps, err := s.List()
	if err != nil {
		return nil, err
	}
	raw, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	return parseContainers(raw, apps), nil
}

func parseContainers(raw []byte, apps []App) []Container {
	byName := map[string]string{} // container or project name -> app
	for _, a := range apps {
		byName[a.Name] = a.Name
	}
	out := []Container{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var r struct{ Names, Image, State, Status, Ports, Labels string }
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Names == "" {
			continue
		}
		c := Container{
			Name:    strings.Split(r.Names, ",")[0],
			Image:   r.Image,
			State:   r.State,
			Status:  r.Status,
			Ports:   r.Ports,
			Project: label(r.Labels, "com.docker.compose.project"),
		}
		if app, ok := byName[c.Name]; ok {
			c.Managed, c.App = true, app
		} else if app, ok := byName[c.Project]; ok && c.Project != "" {
			c.Managed, c.App = true, app
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// label reads one key from docker's "k=v,k=v" label string.
func label(labels, key string) string {
	for _, kv := range strings.Split(labels, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

// ContainerActions are what can be done to any container.
var ContainerActions = map[string]bool{"start": true, "stop": true, "restart": true}

// ContainerAction starts, stops or restarts a container by name.
func (s *Service) ContainerAction(ctx context.Context, name, action string, out io.Writer) error {
	if !ContainerActions[action] {
		return fmt.Errorf("unknown container action %q", action)
	}
	if err := containerExists(ctx, name); err != nil {
		return err
	}
	if err := system.Run(ctx, out, "docker", action, "--", name); err != nil {
		return fmt.Errorf("docker %s: %w", action, err)
	}
	return nil
}

// ContainerLogs writes a container's logs to out until done or ctx ends.
func (s *Service) ContainerLogs(ctx context.Context, name string, opts LogsOptions, out io.Writer) error {
	if err := containerExists(ctx, name); err != nil {
		return err
	}
	args := []string{"logs"}
	if opts.Follow {
		args = append(args, "-f")
	}
	if opts.Tail > 0 {
		args = append(args, "--tail", strconv.Itoa(opts.Tail))
	}
	err := system.Run(ctx, out, "docker", append(args, "--", name)...)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("docker logs: %w", err)
	}
	return nil
}

func containerExists(ctx context.Context, name string) error {
	if strings.HasPrefix(name, "-") || name == "" {
		return fmt.Errorf("%w: %q", ErrContainerNotFound, name)
	}
	if exec.CommandContext(ctx, "docker", "inspect", "--type", "container", "--", name).Run() != nil {
		return fmt.Errorf("%w: %s", ErrContainerNotFound, name)
	}
	return nil
}
