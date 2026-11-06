package deployment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shipyard/internal/compose"
	"shipyard/internal/state"
)

func gitConfig() *Config {
	c := &Config{
		Name:   "inariweb",
		Image:  "ghcr.io/u/inariweb",
		Source: Source{Type: SourceGit, Repository: "acaraballocastro/inariWeb"},
		Build:  Build{Args: map[string]string{"NEXT_PUBLIC_API_URL": "https://inariback.example.com/api/v1"}},
		Deploy: Deploy{Mode: ModeTraefik, Domain: "inari.example.com", InternalPort: "3000", Entrypoint: "websecure", TLS: true},
	}
	c.Normalize()
	return c
}

func TestNormalizeDefaults(t *testing.T) {
	c := gitConfig()
	if c.Build.Platform != "linux/arm64" || c.Deploy.Network != "edge" || c.Version != 1 {
		t.Fatalf("defaults not applied: %+v", c)
	}
	img := &Config{Name: "nginx", Image: "nginx:alpine", Source: Source{Type: SourceImage}, Deploy: Deploy{Mode: ModeTraefik, Domain: "n.example.com"}}
	img.Normalize()
	if img.Build.Platform != "" || img.Deploy.Entrypoint != "web" || img.Deploy.InternalPort != "80" {
		t.Fatalf("image defaults wrong: %+v", img)
	}
	if err := img.Validate(false); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePlatform(t *testing.T) {
	for _, p := range []string{"linux/arm64", "linux/amd64", "linux/arm/v7", "linux/amd64,linux/arm64"} {
		if err := ValidatePlatform(p); err != nil {
			t.Errorf("%s rejected: %v", p, err)
		}
	}
	for _, p := range []string{"", "arm64", "linux/arm64 --push", "linux/ARM64", "linux/arm/7"} {
		if err := ValidatePlatform(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestValidateErrors(t *testing.T) {
	cases := map[string]func(c *Config){
		"bad name":         func(c *Config) { c.Name = "../etc" },
		"bad mode":         func(c *Config) { c.Deploy.Mode = "swarm" },
		"no domain":        func(c *Config) { c.Deploy.Domain = "" },
		"bad port":         func(c *Config) { c.Deploy.InternalPort = "99999" },
		"bad repo":         func(c *Config) { c.Source.Repository = "-x/y" },
		"relative env":     func(c *Config) { c.Env.Files = []string{".env"} },
		"resolver w/o tls": func(c *Config) { c.Deploy.TLS = false; c.Deploy.CertResolver = "le" },
		"bad middleware":   func(c *Config) { c.Deploy.Middlewares = []string{"a b"} },
		"hc without cmd":   func(c *Config) { c.Healthcheck = Healthcheck{Enabled: true} },
		"hc bad duration":  func(c *Config) { c.Healthcheck = Healthcheck{Enabled: true, Command: "true", Interval: "30"} },
		"hc neg retries":   func(c *Config) { c.Healthcheck = Healthcheck{Enabled: true, Command: "true", Retries: -1} },
		"bad build arg":    func(c *Config) { c.Build.Args = map[string]string{"1X": "y"} },
		"image with args":  func(c *Config) { c.Source = Source{Type: SourceImage} },
		"future version":   func(c *Config) { c.Version = 99 },
		"bad platform":     func(c *Config) { c.Build.Platform = "arm64" },
	}
	for name, mutate := range cases {
		c := gitConfig()
		mutate(c)
		if err := c.Validate(false); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if err := gitConfig().Validate(false); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateChecksEnvFiles(t *testing.T) {
	c := gitConfig()
	c.Env.Files = []string{filepath.Join(t.TempDir(), "missing.env")}
	if err := c.Validate(false); err != nil {
		t.Fatalf("checkFiles=false should not stat: %v", err)
	}
	if err := c.Validate(true); err == nil {
		t.Fatal("expected missing env file error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := gitConfig()
	c.Env = Env{Files: []string{"/srv/config/inari.env"}, Keys: []string{"SECRET_KEY"}}
	c.Healthcheck = Healthcheck{Enabled: true, Command: "wget -qO- http://localhost:3000 || exit 1", Interval: "30s", Retries: 3}
	c.Build.ArgsFromEnv = []string{"NPM_TOKEN"}
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := c.Marshal()
	b, _ := got.Marshal()
	if string(a) != string(b) {
		t.Fatalf("round trip mismatch:\n%s\n---\n%s", a, b)
	}
	if !strings.Contains(string(a), "SECRET_KEY") {
		t.Fatal("keys not stored")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	if _, err := Unmarshal([]byte("name: a\nimage: x\nsource: {type: image}\ndeploy: {mode: local}\nhealthcheck: {enabled: false}\ntypo_field: 1\n")); err == nil {
		t.Fatal("expected unknown field error")
	}
	if _, err := Load(t.TempDir()); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestComposeSpecEnvOrder(t *testing.T) {
	c := gitConfig()
	c.Env = Env{Files: []string{"/a.env", "/b.env"}, Keys: []string{"X"}}
	s := c.ComposeSpec("/srv/apps/inariweb")
	want := []string{"/a.env", "/b.env", "/srv/apps/inariweb/.env"}
	if strings.Join(s.EnvFiles, ",") != strings.Join(want, ",") {
		t.Fatalf("env files %v", s.EnvFiles)
	}
	c.Env.Keys = nil
	if len(c.ComposeSpec("/d").EnvFiles) != 2 {
		t.Fatal("local .env referenced without keys")
	}
	if s.Traefik.Entrypoint != "websecure" || !s.Traefik.TLS || s.Traefik.Router != "inariweb" {
		t.Fatalf("traefik %+v", s.Traefik)
	}
}

func TestFromLegacy(t *testing.T) {
	base := t.TempDir()
	if err := compose.Generate(base, "inariweb", "ghcr.io/u/inariweb", "traefik", "inari.example.com", "", "3000", nil, []string{"/data:/data"}); err != nil {
		t.Fatal(err)
	}
	app := state.App{Repo: "u/inariWeb", Image: "ghcr.io/u/inariweb", Mode: "traefik", Domain: "inari.example.com", Path: filepath.Join(base, "inariweb")}
	c, err := FromLegacy("inariweb", app)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source.Type != SourceGit || c.Build.Platform != "linux/arm64" || c.Deploy.InternalPort != "3000" ||
		c.Deploy.Network != "edge" || c.Deploy.Entrypoint != "web" || c.Deploy.Volumes[0] != "/data:/data" {
		t.Fatalf("migration lost settings: %+v", c)
	}
	if err := c.Validate(false); err != nil {
		t.Fatal(err)
	}

	// Migrating must reproduce the same compose file.
	orig, _ := os.ReadFile(filepath.Join(app.Path, ComposeFileName))
	regen, _ := compose.Marshal(c.ComposeSpec(app.Path))
	if string(orig) != string(regen) {
		t.Fatalf("compose differs after migration:\n%s\n---\n%s", orig, regen)
	}

	img := state.App{Image: "nginx:alpine", Mode: "local", Path: filepath.Join(base, "nginx")}
	compose.Generate(base, "nginx", "nginx:alpine", "local", "", "", "80", []string{"8080:80"}, nil)
	c2, err := FromLegacy("nginx", img)
	if err != nil || c2.Source.Type != SourceImage || c2.Deploy.Ports[0] != "8080:80" {
		t.Fatalf("image migration: %+v %v", c2, err)
	}
}
