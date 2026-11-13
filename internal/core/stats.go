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

// Stats samples every running container of the registered services in a
// single `docker stats` call, keyed by service name. Stopped services are
// absent from the result.
func (s *Service) Stats(ctx context.Context) (map[string]Stats, error) {
	apps, err := s.List()
	if err != nil {
		return nil, err
	}
	out := map[string]Stats{}
	if len(apps) == 0 {
		return out, nil
	}
	known := make(map[string]bool, len(apps))
	for _, a := range apps {
		known[a.Name] = true
	}
	raw, err := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker stats: %w", err)
	}
	return parseStats(raw, known), nil
}

func parseStats(raw []byte, known map[string]bool) map[string]Stats {
	out := map[string]Stats{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		var r struct {
			Name, CPUPerc, MemPerc, MemUsage, NetIO, BlockIO, PIDs string
		}
		if json.Unmarshal(sc.Bytes(), &r) != nil || !known[r.Name] {
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
