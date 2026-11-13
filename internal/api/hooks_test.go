package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shipyard/internal/core"
)

// withRepoApp registers a repository service "api" (repo me/api).
func withRepoApp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	st := "apps:\n  api:\n    repo: me/api\n    image: ghcr.io/me/api\n    mode: local\n    path: /tmp/api\n  web:\n    image: nginx\n    mode: local\n    path: /tmp/web\n"
	if err := os.WriteFile(filepath.Join(home, ".shipyard-state.yaml"), []byte(st), 0600); err != nil {
		t.Fatal(err)
	}
}

func signed(t *testing.T, url, secret, event, body string) (*http.Response, string) {
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set(cfConnectingIP, "140.82.112.1") // GitHub through the tunnel
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	resp.Body.Close()
	return resp, b.String()
}

func TestGitHubWebhook(t *testing.T) {
	withRepoApp(t)
	release := make(chan struct{})
	close(release)
	orig := Actions["update"]
	Actions["update"] = testAction(release)
	defer func() { Actions["update"] = orig }()

	svc := &core.Service{}
	jobs := NewJobs(svc)
	hooks := &Hooks{Path: filepath.Join(t.TempDir(), "webhooks.yaml"), Jobs: jobs, PublicURL: "https://shipyard.example.com"}
	fa := newFakeAccess(t)
	s := newServer(t, reg{LocalServer: Local{Svc: svc, Jobs: jobs, Hooks: hooks}}, func(o *Options) {
		o.Hooks, o.Access = hooks, fa.verifier()
	})

	// Image services cannot have a webhook.
	if _, err := hooks.Set("web", "main", false); err == nil {
		t.Fatal("webhook on an image service accepted")
	}
	info, err := hooks.Set("api", "main", false)
	if err != nil || info.Secret == "" || info.URL != "https://shipyard.example.com/hooks/github/api" {
		t.Fatalf("set: %+v %v", info, err)
	}
	if again, _ := hooks.Set("api", "release", false); again.Secret != "" || again.Branch != "release" {
		t.Fatalf("update must not reveal the secret again: %+v", again)
	}
	hooks.Set("api", "main", false)
	secret := info.Secret
	u := s.URL + "/hooks/github/api"
	push := func(ref, repo string) string {
		return fmt.Sprintf(`{"ref":%q,"after":"0123456789abcdef","repository":{"full_name":%q}}`, ref, repo)
	}

	if r, _ := signed(t, u, "wrong", "push", push("refs/heads/main", "me/api")); r.StatusCode != 401 {
		t.Fatalf("bad signature: %d", r.StatusCode)
	}
	if r, _ := signed(t, s.URL+"/hooks/github/ghost", secret, "push", "{}"); r.StatusCode != 401 {
		t.Fatalf("unknown service must look like a bad signature: %d", r.StatusCode)
	}
	if r, b := signed(t, u, secret, "ping", `{}`); r.StatusCode != 200 || !strings.Contains(b, "pong") {
		t.Fatalf("ping: %d %s", r.StatusCode, b)
	}
	if r, b := signed(t, u, secret, "push", push("refs/heads/dev", "me/api")); r.StatusCode != 200 || !strings.Contains(b, "ignored") {
		t.Fatalf("other branch: %d %s", r.StatusCode, b)
	}
	if r, _ := signed(t, u, secret, "push", push("refs/heads/main", "evil/api")); r.StatusCode != 422 {
		t.Fatalf("other repo: %d", r.StatusCode)
	}
	r, b := signed(t, u, secret, "push", push("refs/heads/main", "Me/API"))
	if r.StatusCode != 202 || !strings.Contains(b, `"action":"update"`) {
		t.Fatalf("deploy: %d %s", r.StatusCode, b)
	}
	var job Job
	json.Unmarshal([]byte(b), &job)

	got, _ := hooks.Get("api")
	if got.Last == nil || got.Last.Result != "deployed" || got.Last.Commit != "0123456" || got.Last.JobID != job.ID || got.Secret != "" {
		t.Fatalf("last delivery: %+v", got)
	}
	if fi, _ := os.Stat(hooks.Path); fi.Mode().Perm() != 0600 {
		t.Fatalf("webhooks.yaml perms %v", fi.Mode().Perm())
	}
	// The rest of the API still requires Access through Cloudflare.
	if got := cfGet(t, s.URL+"/api/v1/servers", "", tok); got != 403 {
		t.Fatalf("access exemption leaked to the API: %d", got)
	}
	_ = jobs.Stream(t.Context(), job.ID, func(string, string) error { return nil })
}

func TestHostPruneWebhookAdoptThroughGateway(t *testing.T) {
	pi := newServer(t, reg{LocalServer: fakeBackend{}})
	gw := newServer(t, reg{"pi": Remote{URL: pi.URL, Token: tok}}, func(o *Options) { o.Token = "gw" })
	base := gw.URL + "/api/v1/servers/pi"

	if _, b := get(t, base+"/host", "gw"); !strings.Contains(b, `"temp":51.5`) {
		t.Fatalf("host: %s", b)
	}
	if r, b := postJSON(t, base+"/prune", "gw", `{"all":true}`); r.StatusCode != 202 || !strings.Contains(b, `"action":"prune"`) {
		t.Fatalf("prune: %d %s", r.StatusCode, b)
	}
	if _, b := get(t, base+"/apps/api/webhook", "gw"); !strings.Contains(b, `"/hooks/github/api"`) {
		t.Fatalf("webhook get: %s", b)
	}
	req, _ := http.NewRequest("PUT", base+"/apps/api/webhook", strings.NewReader(`{"branch":"dev"}`))
	req.Header.Set("Authorization", "Bearer gw")
	req.Header.Set(csrfHeader, "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("webhook put: %v %v", err, resp)
	}
	resp.Body.Close()
	del, _ := http.NewRequest("DELETE", base+"/apps/api/webhook", nil)
	del.Header.Set("Authorization", "Bearer gw")
	del.Header.Set(csrfHeader, "1")
	if resp, _ := http.DefaultClient.Do(del); resp.StatusCode != 204 {
		t.Fatalf("webhook delete: %d", resp.StatusCode)
	}
	if _, b := get(t, base+"/containers/cf/adopt", "gw"); !strings.Contains(b, `"container":"cf"`) {
		t.Fatalf("adopt plan: %s", b)
	}
	if r, b := postJSON(t, base+"/containers/cf/adopt", "gw", `{"image":"x","name":"cf","command":["tunnel","run"]}`); r.StatusCode != 202 || !strings.Contains(b, `"action":"adopt"`) {
		t.Fatalf("adopt: %d %s", r.StatusCode, b)
	}
}
