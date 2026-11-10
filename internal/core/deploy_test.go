package core

import (
	"shipyard/internal/deployment"

	"strings"
	"testing"
)

func TestParseBuildArgs(t *testing.T) {
	args, fromEnv, err := ParseBuildArgs([]string{"NEXT_PUBLIC_API_URL=https://x/api,v1", "NPM_TOKEN", "EMPTY="})
	if err != nil {
		t.Fatal(err)
	}
	if args["NEXT_PUBLIC_API_URL"] != "https://x/api,v1" || args["EMPTY"] != "" || len(fromEnv) != 1 || fromEnv[0] != "NPM_TOKEN" {
		t.Fatalf("got %v %v", args, fromEnv)
	}
	if _, _, err := ParseBuildArgs([]string{"BAD KEY=1"}); err == nil {
		t.Fatal("expected error")
	}
	if a, _, _ := ParseBuildArgs(nil); a != nil {
		t.Fatal("expected nil map")
	}
}

func TestParseEnvVars(t *testing.T) {
	m, err := ParseEnvVars([]string{"APP_ENV=production", "LOG_LEVEL=info"})
	if err != nil || m["APP_ENV"] != "production" || m["LOG_LEVEL"] != "info" {
		t.Fatalf("%v %v", m, err)
	}
	_, err = ParseEnvVars([]string{"PASSWORD"})
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = ParseEnvVars([]string{"9X=topsecret"})
	if err == nil || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("err %v", err)
	}
}

func TestRequestRedeployPreservesUnsetFields(t *testing.T) {
	existing := &deployment.Config{
		Version: 1, Name: "inariweb", Image: "ghcr.io/u/inariweb",
		Source: deployment.Source{Type: "git", Repository: "u/inariWeb"},
		Build:  deployment.Build{Platform: "linux/amd64", Args: map[string]string{"A": "1"}},
		Deploy: deployment.Deploy{Mode: "traefik", Domain: "old.example.com", Network: "edge",
			InternalPort: "3000", Entrypoint: "websecure", TLS: true},
		Healthcheck: deployment.Healthcheck{Enabled: true, Command: "true"},
	}
	domain := "new.example.com"
	req := DeployRequest{Repo: "u/inariWeb", Domain: &domain, Env: map[string]string{"X": "1"}}
	if err := req.apply(existing, true, "inariweb", "ghcr.io/u/inariweb", false); err != nil {
		t.Fatal(err)
	}
	d := existing.Deploy
	if d.Domain != "new.example.com" || d.InternalPort != "3000" || d.Entrypoint != "websecure" || !d.TLS || d.Mode != "traefik" {
		t.Fatalf("deploy settings not preserved: %+v", d)
	}
	if existing.Build.Platform != "linux/amd64" || existing.Build.Args["A"] != "1" || !existing.Healthcheck.Enabled {
		t.Fatalf("build/healthcheck not preserved: %+v %+v", existing.Build, existing.Healthcheck)
	}
}

func TestRequestNewDeploymentDefaults(t *testing.T) {
	cfg := &deployment.Config{Version: 1, Name: "app"}
	if err := (DeployRequest{Repo: "u/app"}).apply(cfg, false, "app", "ghcr.io/u/app", false); err != nil {
		t.Fatal(err)
	}
	if cfg.Deploy.Mode != "local" || cfg.Deploy.InternalPort != "80" || cfg.Deploy.Entrypoint != "web" || cfg.Build.Platform != deployment.DefaultPlatform {
		t.Fatalf("defaults not applied: %+v %+v", cfg.Deploy, cfg.Build)
	}
}

func TestRequestImageRejectsBuildArgs(t *testing.T) {
	cfg := &deployment.Config{Version: 1, Name: "whoami"}
	args := []string{"A=1"}
	err := (DeployRequest{Image: "traefik/whoami", BuildArgs: &args}).apply(cfg, false, "whoami", "traefik/whoami", true)
	if err == nil {
		t.Fatal("expected error")
	}
}
