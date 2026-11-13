package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"shipyard/internal/events"
	"shipyard/internal/system"
)

// Host is a snapshot of the machine running the services. Fields that
// cannot be read on this OS are left zero (the UI shows "–").
type Host struct {
	CPU      float64     `json:"cpu"` // percent of all cores, sampled over ~250ms
	Cores    int         `json:"cores"`
	Load     [3]float64  `json:"load"`
	MemTotal uint64      `json:"memTotal"` // bytes
	MemUsed  uint64      `json:"memUsed"`
	SwapUsed uint64      `json:"swapUsed"`
	Temp     float64     `json:"temp,omitempty"` // °C, SoC/CPU sensor
	Uptime   int64       `json:"uptime"`         // seconds
	Disks    []Disk      `json:"disks"`
	Docker   *DockerDisk `json:"docker,omitempty"`
	// MemoryCgroup is false when containers report no memory usage
	// (Raspberry Pi without cgroup_enable=memory).
	MemoryCgroup bool `json:"memoryCgroup"`
}

// Disk is the usage of one filesystem.
type Disk struct {
	Path  string `json:"path"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

// DockerDisk is `docker system df`, in bytes.
type DockerDisk struct {
	Images            uint64 `json:"images"`
	ImagesReclaim     uint64 `json:"imagesReclaim"`
	Containers        uint64 `json:"containers"`
	Volumes           uint64 `json:"volumes"`
	BuildCache        uint64 `json:"buildCache"`
	BuildCacheReclaim uint64 `json:"buildCacheReclaim"`
}

// HostInfo samples the host. It never fails as a whole: unreadable parts
// are skipped.
func (s *Service) HostInfo(ctx context.Context) *Host {
	h := &Host{Cores: runtime.NumCPU()}
	h.CPU = cpuPercent(250 * time.Millisecond)
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		h.Load = parseLoadavg(string(b))
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		h.MemTotal, h.MemUsed, h.SwapUsed = parseMeminfo(string(b))
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			v, _ := strconv.ParseFloat(f[0], 64)
			h.Uptime = int64(v)
		}
	}
	h.Temp = readTemp()
	h.MemoryCgroup = memoryCgroup()

	seen := map[uint64]bool{}
	for _, p := range []string{"/", DefaultRoot, "/var/lib/docker"} {
		var st syscall.Statfs_t
		if syscall.Statfs(p, &st) != nil {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		key := total ^ uint64(st.Files) // same filesystem → same numbers
		if seen[key] || total == 0 {
			continue
		}
		seen[key] = true
		h.Disks = append(h.Disks, Disk{Path: p, Total: total, Used: total - st.Bfree*uint64(st.Bsize)})
	}

	if raw, err := exec.CommandContext(ctx, "docker", "system", "df", "--format", "{{json .}}").Output(); err == nil {
		h.Docker = parseSystemDF(raw)
	}
	return h
}

func cpuPercent(window time.Duration) float64 {
	a, okA := readCPU()
	if !okA {
		return 0
	}
	time.Sleep(window)
	b, okB := readCPU()
	if !okB {
		return 0
	}
	total, idle := b[0]-a[0], b[1]-a[1]
	if total == 0 {
		return 0
	}
	return 100 * float64(total-idle) / float64(total)
}

// readCPU returns [total, idle] jiffies from /proc/stat.
func readCPU() ([2]uint64, bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return [2]uint64{}, false
	}
	return parseProcStat(string(b))
}

func parseProcStat(s string) ([2]uint64, bool) {
	line, _, _ := strings.Cut(s, "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return [2]uint64{}, false
	}
	var total, idle uint64
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle, iowait
			idle += n
		}
	}
	return [2]uint64{total, idle}, true
}

func parseLoadavg(s string) [3]float64 {
	var out [3]float64
	f := strings.Fields(s)
	for i := 0; i < 3 && i < len(f); i++ {
		out[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return out
}

// parseMeminfo returns total, used (total − available) and swap used, in bytes.
func parseMeminfo(s string) (total, used, swap uint64) {
	v := map[string]uint64{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		k, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(f[0], 10, 64)
		v[k] = n * 1024 // kB
	}
	total = v["MemTotal"]
	if avail, ok := v["MemAvailable"]; ok && avail <= total {
		used = total - avail
	}
	if v["SwapTotal"] >= v["SwapFree"] {
		swap = v["SwapTotal"] - v["SwapFree"]
	}
	return
}

func readTemp() float64 {
	for i := 0; i < 10; i++ {
		b, err := os.ReadFile(fmt.Sprintf("/sys/class/thermal/thermal_zone%d/temp", i))
		if err != nil {
			break
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		if err == nil && n > 0 {
			return n / 1000
		}
	}
	return 0
}

// memoryCgroup reports whether the kernel accounts container memory.
func memoryCgroup() bool {
	if b, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers"); err == nil { // cgroup v2
		return strings.Contains(string(b), "memory")
	}
	if b, err := os.ReadFile("/proc/cgroups"); err == nil { // v1: "memory 0 1 1"
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.Fields(l)
			if len(f) >= 4 && f[0] == "memory" {
				return f[3] == "1"
			}
		}
	}
	return runtime.GOOS != "linux" // not applicable elsewhere
}

func parseSystemDF(raw []byte) *DockerDisk {
	d := &DockerDisk{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		var r struct{ Type, Size, Reclaimable string }
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		size := parseSize(r.Size)
		reclaim := parseSize(strings.Fields(r.Reclaimable + " ")[0])
		switch r.Type {
		case "Images":
			d.Images, d.ImagesReclaim = size, reclaim
		case "Containers":
			d.Containers = size
		case "Local Volumes":
			d.Volumes = size
		case "Build Cache":
			d.BuildCache, d.BuildCacheReclaim = size, reclaim
		}
	}
	return d
}

// parseSize reads docker's human sizes: "1.2GB", "512MB", "0B", "3.4kB".
func parseSize(s string) uint64 {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   float64
	}{{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			f, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0
			}
			return uint64(f * u.mult)
		}
	}
	return 0
}

// Prune frees Docker disk space: dangling images and build cache, plus
// every unused image when all is true. Containers, volumes and networks
// are never touched.
func (s *Service) Prune(ctx context.Context, all bool, p events.Progress) error {
	p.Step("images", "removing %s images", map[bool]string{true: "unused", false: "dangling"}[all])
	args := []string{"image", "prune", "-f"}
	if all {
		args = append(args, "-a")
	}
	if err := system.Run(ctx, p.Writer(), "docker", args...); err != nil {
		return fmt.Errorf("docker image prune: %w", err)
	}
	p.Step("cache", "removing build cache")
	if err := system.Run(ctx, p.Writer(), "docker", "builder", "prune", "-f"); err != nil {
		p.Warn("docker builder prune: %s", err)
	}
	p.Ok("docker disk cleaned up")
	return nil
}
