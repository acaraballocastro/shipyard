package compose

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildEnvFiles(t *testing.T) {
	c, err := Build(Spec{
		ServiceName: "api", Image: "ghcr.io/u/api", Mode: "local",
		EnvFiles: []string{"/srv/config/a.env", "/srv/apps/api/.env"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := c.Services["api"].EnvFile
	if !reflect.DeepEqual(got, []string{"/srv/config/a.env", "/srv/apps/api/.env"}) {
		t.Fatalf("env_file = %v", got)
	}
	data, _ := yaml.Marshal(c)
	if !strings.Contains(string(data), "env_file:\n") {
		t.Fatalf("missing env_file in yaml:\n%s", data)
	}
}

func TestBuildNoEnvFileOmitted(t *testing.T) {
	data, err := Marshal(Spec{ServiceName: "web", Image: "nginx", Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "env_file") || strings.Contains(string(data), "healthcheck") {
		t.Fatalf("unexpected keys:\n%s", data)
	}
}

func TestTraefikDefaultsMatchLegacy(t *testing.T) {
	want := []string{
		"traefik.enable=true",
		"traefik.http.routers.app.rule=Host(`a.example.com`)",
		"traefik.http.routers.app.entrypoints=web",
		"traefik.http.services.app.loadbalancer.server.port=3000",
	}
	if got := BuildTraefikLabels("app", "a.example.com", "3000"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	c, _ := Build(Spec{ServiceName: "app", Image: "x", Mode: "traefik",
		Traefik: TraefikOptions{Domain: "a.example.com", InternalPort: "3000"}})
	s := c.Services["app"]
	if !reflect.DeepEqual(s.Labels, want) || !reflect.DeepEqual(s.Networks, []string{"edge"}) || !c.Networks["edge"].External {
		t.Fatalf("unexpected service %+v networks %+v", s, c.Networks)
	}
}

func TestTraefikTLS(t *testing.T) {
	got := BuildTraefikLabelsWith(TraefikOptions{
		Router: "inariweb", Domain: "inari.example.com", InternalPort: "3000",
		Entrypoint: "websecure", TLS: true, CertResolver: "le",
		Middlewares: []string{"secure-headers@file", "compress"},
	})
	want := []string{
		"traefik.enable=true",
		"traefik.http.routers.inariweb.rule=Host(`inari.example.com`)",
		"traefik.http.routers.inariweb.entrypoints=websecure",
		"traefik.http.routers.inariweb.tls=true",
		"traefik.http.routers.inariweb.tls.certresolver=le",
		"traefik.http.routers.inariweb.middlewares=secure-headers@file,compress",
		"traefik.http.services.inariweb.loadbalancer.server.port=3000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%v\nwant\n%v", got, want)
	}
}

func TestHealthcheck(t *testing.T) {
	data, err := Marshal(Spec{ServiceName: "w", Image: "x", Mode: "local",
		Healthcheck: &Healthcheck{
			Test:     []string{"CMD-SHELL", "wget -qO- http://localhost:3000/api/health || exit 1"},
			Interval: "30s", Timeout: "5s", Retries: 3, StartPeriod: "20s",
		}})
	if err != nil {
		t.Fatal(err)
	}
	var c Compose
	yaml.Unmarshal(data, &c)
	h := c.Services["w"].Healthcheck
	if h == nil || h.Test[0] != "CMD-SHELL" || h.Retries != 3 || h.StartPeriod != "20s" {
		t.Fatalf("bad healthcheck: %+v\n%s", h, data)
	}
}

func TestLocalModeIgnoresNetwork(t *testing.T) {
	c, _ := Build(Spec{ServiceName: "a", Image: "x", Mode: "local", Network: "edge", Ports: []string{"8080:80"}})
	if len(c.Networks) != 0 || len(c.Services["a"].Networks) != 0 {
		t.Fatal("local mode should not attach networks")
	}
	if c.Services["a"].Ports[0] != "8080:80" {
		t.Fatal("ports missing")
	}
}

func TestUnknownMode(t *testing.T) {
	if _, err := Build(Spec{ServiceName: "a", Image: "x", Mode: "swarm"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateLegacyAPI(t *testing.T) {
	dir := t.TempDir()
	if err := Generate(dir, "app", "img", "traefik", "a.com", "", "80", nil, []string{"/d:/d"}); err != nil {
		t.Fatal(err)
	}
	c, err := Parse(filepath.Join(dir, "app", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Services["app"].Volumes[0] != "/d:/d" {
		t.Fatal("volume lost")
	}
	_ = os.RemoveAll(dir)
}

func TestNamedVolumesDeclaredWithExactName(t *testing.T) {
	c, err := Build(Spec{ServiceName: "app", Image: "x", Mode: "local",
		Volumes: []string{"pgdata:/var/lib/postgresql/data", "/srv/x:/x:ro", "./rel:/r"},
		Command: []string{"tunnel", "run"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Volumes) != 1 || c.Volumes["pgdata"].Name != "pgdata" {
		t.Fatalf("volumes: %+v", c.Volumes)
	}
	if got := c.Services["app"].Command; len(got) != 2 || got[0] != "tunnel" {
		t.Fatalf("command: %v", got)
	}
}
