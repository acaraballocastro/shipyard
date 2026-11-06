package docker

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ContainerState is the subset of docker inspect used by shipyard.
type ContainerState struct {
	Status  string // running, exited, restarting, ...
	Running bool
	Health  string // healthy, unhealthy, starting, or "" when no healthcheck
}

// Inspect returns the state of a container by name.
func Inspect(name string) (ContainerState, error) {
	out, err := exec.Command("docker", "inspect", "--format", "{{json .State}}", "--", name).Output()
	if err != nil {
		return ContainerState{}, fmt.Errorf("container %s not found", name)
	}
	var raw struct {
		Status  string `json:"Status"`
		Running bool   `json:"Running"`
		Health  *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &raw); err != nil {
		return ContainerState{}, fmt.Errorf("parsing inspect output: %w", err)
	}
	s := ContainerState{Status: raw.Status, Running: raw.Running}
	if raw.Health != nil {
		s.Health = raw.Health.Status
	}
	return s, nil
}

// WaitReady polls the container until it is running (and healthy when a
// healthcheck is configured) or the timeout expires.
func WaitReady(name string, timeout time.Duration) (ContainerState, error) {
	return waitReady(Inspect, name, timeout, 2*time.Second)
}

func waitReady(inspect func(string) (ContainerState, error), name string, timeout, every time.Duration) (ContainerState, error) {
	deadline := time.Now().Add(timeout)
	var last ContainerState
	var err error
	for {
		last, err = inspect(name)
		if err == nil {
			switch {
			case last.Health == "unhealthy":
				return last, fmt.Errorf("container %s is unhealthy", name)
			case last.Running && (last.Health == "" || last.Health == "healthy"):
				return last, nil
			case !last.Running && last.Status == "exited":
				return last, fmt.Errorf("container %s exited", name)
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return last, err
			}
			return last, fmt.Errorf("container %s not ready after %s (status %s, health %s)",
				name, timeout, last.Status, orDash(last.Health))
		}
		time.Sleep(every)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
