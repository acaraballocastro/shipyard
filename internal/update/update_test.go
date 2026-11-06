package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shipyard/internal/compose"
	"shipyard/internal/deployment"
	"shipyard/internal/docker"
	"shipyard/internal/envfile"
	"shipyard/internal/state"
	"shipyard/internal/ui"
)

type fake struct {
	clones, builds, ups int
	lastBuild           docker.BuildOptions
	buildErr, upErr     error
	upErrOnce           bool
	ready               docker.ContainerState
	readyErr            error
}

func (f *fake) runner() Runner {
	return Runner{
		Clone: func(repo string) (string, error) { f.clones++; return "/src/" + repo, nil },
		Build: func(image, dir string, o docker.BuildOptions) error {
			f.builds++
			f.lastBuild = o
			return f.buildErr
		},
		ComposeUp: func(dir string) error {
			f.ups++
			if f.upErr != nil {
				err := f.upErr
				if f.upErrOnce {
					f.upErr = nil
				}
				return err
			}
			return nil
		},
		WaitReady: func(string, time.Duration) (docker.ContainerState, error) { return f.ready, f.readyErr },
	}
}

func init() { ui.SetOutput(new(strings.Builder)) }

func gitCfg() *deployment.Config {
	return &deployment.Config{
		Name:   "inariweb",
		Image:  "ghcr.io/u/inariweb",
		Source: deployment.Source{Type: deployment.SourceGit, Repository: "u/inariWeb"},
		Build:  deployment.Build{Platform: "linux/amd64", Args: map[string]string{"NEXT_PUBLIC_API_URL": "https://api"}},
		Deploy: deployment.Deploy{Mode: "traefik", Domain: "inari.example.com", InternalPort: "3000", Entrypoint: "websecure", TLS: true},
	}
}

func TestApplyGitBuildsWithPlatformAndArgs(t *testing.T) {
	dir := t.TempDir()
	f := &fake{ready: docker.ContainerState{Running: true}}
	if _, err := f.runner().Apply(dir, gitCfg(), ApplyOptions{Build: true, SetEnv: map[string]string{"SECRET": "s3cr3t"}}); err != nil {
		t.Fatal(err)
	}
	if f.clones != 1 || f.builds != 1 || f.ups != 1 {
		t.Fatalf("calls: %+v", f)
	}
	if f.lastBuild.Platform != "linux/amd64" || f.lastBuild.Args["NEXT_PUBLIC_API_URL"] != "https://api" {
		t.Fatalf("build opts %+v", f.lastBuild)
	}

	// .env written 0600, referenced by compose, values never in deployment.yaml
	fi, err := os.Stat(filepath.Join(dir, ".env"))
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf(".env: %v %v", fi, err)
	}
	dy, _ := os.ReadFile(filepath.Join(dir, "deployment.yaml"))
	if strings.Contains(string(dy), "s3cr3t") || !strings.Contains(string(dy), "SECRET") {
		t.Fatalf("deployment.yaml:\n%s", dy)
	}
	c, _ := compose.Parse(filepath.Join(dir, "docker-compose.yml"))
	if c.Services["inariweb"].EnvFile[0] != filepath.Join(dir, ".env") {
		t.Fatalf("env_file %v", c.Services["inariweb"].EnvFile)
	}
}

func TestApplyImageSkipsBuild(t *testing.T) {
	f := &fake{ready: docker.ContainerState{Running: true}}
	cfg := &deployment.Config{Name: "nginx", Image: "nginx:alpine", Source: deployment.Source{Type: "image"},
		Deploy: deployment.Deploy{Mode: "traefik", Domain: "n.example.com", InternalPort: "80"}}
	if _, err := f.runner().Apply(t.TempDir(), cfg, ApplyOptions{Build: true}); err != nil {
		t.Fatal(err)
	}
	if f.clones != 0 || f.builds != 0 || f.ups != 1 {
		t.Fatalf("calls: %+v", f)
	}
}

func TestApplyBuildFailureLeavesFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("original"), 0644)
	f := &fake{buildErr: errors.New("boom")}
	if _, err := f.runner().Apply(dir, gitCfg(), ApplyOptions{Build: true}); err == nil {
		t.Fatal("expected error")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if string(data) != "original" || f.ups != 0 {
		t.Fatal("compose modified or started after failed build")
	}
	if _, err := os.Stat(filepath.Join(dir, "deployment.yaml")); err == nil {
		t.Fatal("deployment.yaml written after failed build")
	}
}

func TestApplyUpFailureRestoresPrevious(t *testing.T) {
	dir := t.TempDir()
	ok := &fake{ready: docker.ContainerState{Running: true}}
	if _, err := ok.runner().Apply(dir, gitCfg(), ApplyOptions{Build: true}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	beforeCfg, _ := os.ReadFile(filepath.Join(dir, "deployment.yaml"))

	changed := gitCfg()
	changed.Deploy.InternalPort = "4000"
	bad := &fake{upErr: errors.New("port in use"), upErrOnce: true}
	_, err := bad.runner().Apply(dir, changed, ApplyOptions{Build: true, SetEnv: map[string]string{"A": "1"}})
	if err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("err = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	afterCfg, _ := os.ReadFile(filepath.Join(dir, "deployment.yaml"))
	if string(before) != string(after) || string(beforeCfg) != string(afterCfg) {
		t.Fatal("previous files not restored")
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		t.Fatal(".env should be removed (did not exist before)")
	}
	if bad.ups != 2 {
		t.Fatalf("expected re-up of previous config, ups=%d", bad.ups)
	}
}

func TestApplyPreservesExistingEnv(t *testing.T) {
	dir := t.TempDir()
	f := &fake{ready: docker.ContainerState{Running: true}}
	r := f.runner()
	r.Apply(dir, gitCfg(), ApplyOptions{SetEnv: map[string]string{"A": "1", "B": "2"}})
	if _, err := r.Apply(dir, gitCfg(), ApplyOptions{SetEnv: map[string]string{"B": "3"}}); err != nil {
		t.Fatal(err)
	}
	vars, _ := envfile.Read(filepath.Join(dir, ".env"))
	if vars["A"] != "1" || vars["B"] != "3" {
		t.Fatalf("vars %v", vars)
	}
}

func TestUpdatePreservesConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "apps", "inariweb")

	cfg := gitCfg()
	envPath := filepath.Join(home, "inari.env")
	os.WriteFile(envPath, []byte("DB_URL=postgres://x\n"), 0600)
	cfg.Env.Files = []string{envPath}
	cfg.Healthcheck = deployment.Healthcheck{Enabled: true, Command: "true", Interval: "10s"}

	f := &fake{ready: docker.ContainerState{Running: true, Health: "healthy"}}
	r := f.runner()
	if _, err := r.Apply(dir, cfg, ApplyOptions{Build: true, SetEnv: map[string]string{"K": "v"}}); err != nil {
		t.Fatal(err)
	}
	state.Save(&state.State{Apps: map[string]state.App{"inariweb": {Repo: "u/inariWeb", Image: cfg.Image, Mode: "traefik", Path: dir, LastCommit: "abc"}}})
	before, _ := os.ReadFile(filepath.Join(dir, "deployment.yaml"))
	beforeCompose, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))

	if err := r.Update("inariweb"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "deployment.yaml"))
	afterCompose, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if string(before) != string(after) || string(beforeCompose) != string(afterCompose) {
		t.Fatalf("update changed config:\n%s\n---\n%s", before, after)
	}
	if f.lastBuild.Platform != "linux/amd64" || f.lastBuild.Args["NEXT_PUBLIC_API_URL"] != "https://api" {
		t.Fatalf("update did not reuse build settings: %+v", f.lastBuild)
	}
	vars, _ := envfile.Read(filepath.Join(dir, ".env"))
	if vars["K"] != "v" {
		t.Fatal(".env lost on update")
	}
	st, _ := state.Load()
	if st.Apps["inariweb"].LastCommit != "abc" {
		t.Fatal("state LastCommit lost")
	}
}

func TestUpdateMigratesLegacyImageDeployment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, "apps")
	compose.Generate(base, "whoami", "traefik/whoami", "traefik", "w.example.com", "edge", "80", nil, nil)
	state.Save(&state.State{Apps: map[string]state.App{"whoami": {Image: "traefik/whoami", Mode: "traefik", Domain: "w.example.com", Network: "edge", Path: filepath.Join(base, "whoami")}}})

	f := &fake{ready: docker.ContainerState{Running: true}}
	if err := f.runner().Update("whoami"); err != nil {
		t.Fatal(err)
	}
	if f.clones != 0 || f.builds != 0 || f.ups != 1 {
		t.Fatalf("image update should only pull/recreate: %+v", f)
	}
	cfg, err := deployment.Load(filepath.Join(base, "whoami"))
	if err != nil || cfg.Source.Type != "image" || cfg.Deploy.Domain != "w.example.com" {
		t.Fatalf("migration: %+v %v", cfg, err)
	}
}

func TestUpdateUnknownApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := (&fake{}).runner().Update("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestReadyTimeout(t *testing.T) {
	if readyTimeout(deployment.Healthcheck{}) != time.Minute {
		t.Fatal("default")
	}
	got := readyTimeout(deployment.Healthcheck{Enabled: true, Interval: "30s", Timeout: "5s", Retries: 3, StartPeriod: "20s"})
	if got != 20*time.Second+4*35*time.Second+15*time.Second {
		t.Fatalf("got %s", got)
	}
}
