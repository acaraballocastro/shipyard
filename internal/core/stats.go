package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Stats is a point-in-time resource sample of one container.
type Stats struct {
	CPU      float64 `json:"cpu"`      // percent of one core (can exceed 100)
	MemPerc  float64 `json:"memPerc"`  // percent of the container's limit
	MemUsage string  `json:"memUsage"` // "12.3MiB / 1.9GiB"
	NetIO    string  `json:"netIO"`    // "1.2kB / 3.4kB" (rx / tx)
	BlockIO  string  `json:"blockIO"`  // "0B / 8.19kB" (read / write)
	PIDs     int     `json:"pids"`
}

// Stats samples every running container in a single `docker stats` call,
// keyed by container name (a shipyard service's container is named after
// it). Stopped containers are absent from the result.
func (s *Service) Stats(ctx context.Context) (map[string]Stats, error) {
	raw, err := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker stats: %w", err)
	}
	return parseStats(raw, nil), nil
}

// parseStats reads `docker stats` JSON lines; known, when not nil, keeps
// only those names.
func parseStats(raw []byte, known map[string]bool) map[string]Stats {
	out := map[string]Stats{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		var r struct {
			Name, CPUPerc, MemPerc, MemUsage, NetIO, BlockIO, PIDs string
		}
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Name == "" || (known != nil && !known[r.Name]) {
			continue
		}
		pids, _ := strconv.Atoi(r.PIDs)
		out[r.Name] = Stats{
			CPU:      percent(r.CPUPerc),
			MemPerc:  percent(r.MemPerc),
			MemUsage: r.MemUsage,
			NetIO:    r.NetIO,
			BlockIO:  r.BlockIO,
			PIDs:     pids,
		}
	}
	return out
}

func percent(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	return f
}
