package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustInspect(t *testing.T, s string) inspected {
	var c inspected
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPlanLocalContainer(t *testing.T) {
	c := mustInspect(t, `{
	  "Name": "/Cloudflared",
	  "Config": {"Image": "cloudflare/cloudflared:latest",
	    "Env": ["PATH=/usr/bin", "TUNNEL_TOKEN=secret"],
	    "Cmd": ["tunnel", "run"], "Entrypoint": ["cloudflared"],
	    "Labels": {"com.docker.compose.project": "tunnel"}},
	  "HostConfig": {"NetworkMode": "bridge",
	    "PortBindings": {"2000/tcp": [{"HostIp": "", "HostPort": "2000"}], "53/udp": [{"HostIp": "127.0.0.1", "HostPort": "5353"}]}},
	  "Mounts": [{"Type": "volume", "Name": "cfdata", "Destination": "/etc/cloudflared", "RW": true},
	             {"Type": "bind", "Source": "/srv/x", "Destination": "/x", "RW": false},
	             {"Type": "tmpfs", "Destination": "/tmp"}],
	  "NetworkSettings": {"Networks": {"bridge": {}, "backend": {}}}
	}`)
	var img imageConfig
	img.Config.Env = []string{"PATH=/usr/bin"}
	img.Config.Cmd = []string{"version"}
	img.Config.Entrypoint = []string{"cloudflared"}

	p := planFrom(c, img, nil)
	r := p.Request
	if r.Name != "cloudflared" || r.Image != "cloudflare/cloudflared:latest" || *r.Mode != "local" {
		t.Fatalf("basics: %+v", r)
	}
	if len(r.Env) != 1 || r.Env["TUNNEL_TOKEN"] != "secret" {
		t.Errorf("env should only keep what the container adds: %v", r.Env)
	}
	if r.Command == nil || strings.Join(*r.Command, " ") != "tunnel run" {
		t.Errorf("command: %v", r.Command)
	}
	if got := strings.Join(*r.Ports, ","); got != "127.0.0.1:5353:53/udp,2000:2000" {
		t.Errorf("ports: %s", got)
	}
	if got := strings.Join(*r.Volumes, ","); got != "/srv/x:/x:ro,cfdata:/etc/cloudflared" {
		t.Errorf("volumes: %s", got)
	}
	w := strings.Join(p.Warnings, "\n")
	for _, want := range []string{"named \"cloudflared\"", "tmpfs", "backend", "compose project"} {
		if !strings.Contains(w, want) {
			t.Errorf("missing warning %q in:\n%s", want, w)
		}
	}
	if len(p.Blockers) != 0 {
		t.Errorf("blockers: %v", p.Blockers)
	}
}

func TestPlanTraefikAndBlockers(t *testing.T) {
	c := mustInspect(t, `{
	  "Name": "/whoami",
	  "Config": {"Image": "traefik/whoami", "Labels": {
	    "traefik.http.routers.w.rule": "Host(`+"`who.example.com`"+`)",
	    "traefik.http.routers.w.entrypoints": "websecure",
	    "traefik.http.routers.w.tls.certresolver": "le",
	    "traefik.http.services.w.loadbalancer.server.port": "8080"}},
	  "HostConfig": {"NetworkMode": "edge"},
	  "NetworkSettings": {"Networks": {"edge": {}}}
	}`)
	p := planFrom(c, imageConfig{}, []App{{Name: "whoami"}})
	r := p.Request
	if *r.Mode != "traefik" || *r.Domain != "who.example.com" || *r.InternalPort != "8080" ||
		*r.Entrypoint != "websecure" || !*r.TLS || *r.CertResolver != "le" || *r.Network != "edge" {
		t.Fatalf("traefik: %+v", r)
	}
	if len(p.Blockers) == 0 {
		t.Fatal("existing service with the same name must block")
	}

	host := mustInspect(t, `{"Name": "/h", "Config": {"Image": "x"}, "HostConfig": {"NetworkMode": "host"}}`)
	if len(planFrom(host, imageConfig{}, nil).Blockers) == 0 {
		t.Fatal("host networking must block")
	}
}
