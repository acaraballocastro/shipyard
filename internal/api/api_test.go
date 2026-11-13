package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"shipyard/internal/core"
)

type fakeBackend struct{}

func (fakeBackend) Info(context.Context) (*Info, error) { return &Info{Hostname: "pi"}, nil }
func (fakeBackend) Apps(context.Context) ([]core.App, error) {
	return []core.App{{Name: "web", Image: "nginx", Mode: "local"}}, nil
}
func (fakeBackend) Status(_ context.Context, app string) (*core.Status, error) {
	if app != "web" {
		return nil, fmt.Errorf("%w: %s", core.ErrNotFound, app)
	}
	return &core.Status{App: core.App{Name: "web"}, Running: true, Container: true}, nil
}
func (fakeBackend) Logs(_ context.Context, app string, o core.LogsOptions, w io.Writer) error {
	if app != "web" {
		return errors.New("boom")
	}
	fmt.Fprintf(w, "line1\nline2 tail=%d\npartial", o.Tail)
	return nil
}

func (fakeBackend) StartAction(context.Context, string, string) (*Job, error) {
	return nil, ErrUnknownAction
}
func (fakeBackend) StartDeploy(_ context.Context, req core.DeployRequest) (*Job, error) {
	if req.Image == "" && req.Repo == "" {
		return nil, fmt.Errorf("%w: specify a repository or an image", ErrInvalidRequest)
	}
	return &Job{ID: "d1", App: core.DeployName(req), Action: "deploy", Status: JobRunning}, nil
}
func (fakeBackend) ListJobs(_ context.Context, app string) ([]Job, error) {
	return []Job{{ID: "j1", App: "web", Action: "restart", Status: JobSucceeded}}, nil
}
func (fakeBackend) Stats(context.Context) (map[string]core.Stats, error) {
	return map[string]core.Stats{"web": {CPU: 1.5, MemPerc: 2}}, nil
}
func (fakeBackend) StatsHistory(context.Context) (map[string][]Sample, error) {
	return map[string][]Sample{"web": {{T: 1, Stats: core.Stats{CPU: 1}}}}, nil
}
func (fakeBackend) Host(context.Context) (*core.Host, error) {
	return &core.Host{Cores: 4, MemTotal: 8 << 30, Temp: 51.5}, nil
}
func (fakeBackend) Prune(_ context.Context, all bool) (*Job, error) {
	return &Job{ID: "p1", App: "docker", Action: "prune", Status: JobRunning}, nil
}
func (fakeBackend) Webhook(_ context.Context, app string) (*WebhookInfo, error) {
	return &WebhookInfo{Enabled: true, Branch: "main", Path: "/hooks/github/" + app}, nil
}
func (fakeBackend) SetWebhook(_ context.Context, app, branch string, rotate bool) (*WebhookInfo, error) {
	return &WebhookInfo{Enabled: true, Branch: branch, Secret: "s3cret"}, nil
}
func (fakeBackend) DeleteWebhook(context.Context, string) error { return nil }
func (fakeBackend) AdoptPlan(_ context.Context, name string) (*core.AdoptPlan, error) {
	return &core.AdoptPlan{Container: name, Request: core.DeployRequest{Image: "x", Name: name}}, nil
}
func (fakeBackend) StartAdopt(_ context.Context, name string, req core.DeployRequest) (*Job, error) {
	return &Job{ID: "a1", App: req.Name, Action: "adopt", Status: JobRunning}, nil
}
func (fakeBackend) Env(_ context.Context, app string) (map[string]string, error) {
	if app != "web" {
		return nil, fmt.Errorf("%w: %s", core.ErrNotFound, app)
	}
	return map[string]string{"DB_URL": "postgres://x"}, nil
}
func (fakeBackend) Containers(context.Context) ([]core.Container, error) {
	return []core.Container{{Name: "cloudflared", State: "running"}, {Name: "web", Managed: true, App: "web"}}, nil
}
func (fakeBackend) ContainerAction(_ context.Context, name, action string) error {
	if name != "cloudflared" {
		return fmt.Errorf("%w: %s", core.ErrContainerNotFound, name)
	}
	if action != "restart" {
		return fmt.Errorf("%w: %s", ErrUnknownAction, action)
	}
	return nil
}
func (fakeBackend) ContainerLogs(_ context.Context, name string, o core.LogsOptions, w io.Writer) error {
	fmt.Fprintf(w, "cf-line tail=%d\n", o.Tail)
	return nil
}
func (fakeBackend) Job(context.Context, string) (*Job, error) { return nil, ErrJobNotFound }
func (fakeBackend) CancelJob(context.Context, string) error   { return ErrJobNotFound }
func (fakeBackend) JobEvents(context.Context, string, func(string, string) error) error {
	return ErrJobNotFound
}

type reg map[string]Backend

func (r reg) Servers() []ServerInfo {
	var out []ServerInfo
	for n := range r {
		out = append(out, ServerInfo{Name: n})
	}
	return out
}
func (r reg) Backend(n string) (Backend, bool) { b, ok := r[n]; return b, ok }

const tok = "secret-token"

func newServer(t *testing.T, r Registry, opts ...func(*Options)) *httptest.Server {
	o := Options{Registry: r, Token: tok, UI: fstest.MapFS{"index.html": {Data: []byte("<html>ui</html>")}}}
	for _, f := range opts {
		f(&o)
	}
	s := httptest.NewServer(Handler(o))
	t.Cleanup(s.Close)
	return s
}

func get(t *testing.T, url, token string) (*http.Response, string) {
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func TestAuthRequired(t *testing.T) {
	s := newServer(t, reg{"local": fakeBackend{}})
	if r, _ := get(t, s.URL+"/api/v1/servers", ""); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	if r, _ := get(t, s.URL+"/api/v1/servers", "wrong"); r.StatusCode != 401 {
		t.Fatalf("bad token: %d", r.StatusCode)
	}
	if r, _ := get(t, s.URL+"/api/v1/servers", tok); r.StatusCode != 200 {
		t.Fatalf("good token: %d", r.StatusCode)
	}
	// The UI itself is public (it shows the login screen); client routes fall back to index.
	if r, body := get(t, s.URL+"/servers/local/apps/web", ""); r.StatusCode != 200 || !strings.Contains(body, "ui") {
		t.Fatalf("spa fallback: %d %s", r.StatusCode, body)
	}
}

func TestSessionCookie(t *testing.T) {
	s := newServer(t, reg{"local": fakeBackend{}})
	req0, _ := http.NewRequest("POST", s.URL+"/api/v1/session", strings.NewReader(`{"token":"`+tok+`"}`))
	req0.Header.Set(csrfHeader, "1")
	resp, err := http.DefaultClient.Do(req0)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("login: %v %d", err, resp.StatusCode)
	}
	c := resp.Cookies()[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags: %+v", c)
	}
	req, _ := http.NewRequest("GET", s.URL+"/api/v1/session", nil)
	req.AddCookie(c)
	r2, _ := http.DefaultClient.Do(req)
	if r2.StatusCode != 200 {
		t.Fatalf("cookie auth: %d", r2.StatusCode)
	}
}

func TestAppsStatusAndNotFound(t *testing.T) {
	s := newServer(t, reg{"local": fakeBackend{}})
	_, body := get(t, s.URL+"/api/v1/servers/local/apps", tok)
	var apps []core.App
	if json.Unmarshal([]byte(body), &apps) != nil || len(apps) != 1 || apps[0].Name != "web" {
		t.Fatalf("apps: %s", body)
	}
	if r, _ := get(t, s.URL+"/api/v1/servers/local/apps/nope", tok); r.StatusCode != 404 {
		t.Fatalf("missing app: %d", r.StatusCode)
	}
	if r, _ := get(t, s.URL+"/api/v1/servers/ghost/apps", tok); r.StatusCode != 404 {
		t.Fatalf("missing server: %d", r.StatusCode)
	}
}

func TestLogsSSE(t *testing.T) {
	s := newServer(t, reg{"local": fakeBackend{}})
	r, body := get(t, s.URL+"/api/v1/servers/local/apps/web/logs?tail=50", tok)
	if r.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %s", r.Header.Get("Content-Type"))
	}
	for _, want := range []string{"data: line1\n\n", "data: line2 tail=50\n\n", "data: partial\n\n", "event: end"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	_, body = get(t, s.URL+"/api/v1/servers/local/apps/other/logs", tok)
	if !strings.Contains(body, "event: error\ndata: boom") {
		t.Errorf("error event missing:\n%s", body)
	}
}

// The workstation gateway talks to a remote `shipyard serve` over HTTP.
func TestGatewayToRemote(t *testing.T) {
	pi := newServer(t, reg{LocalServer: fakeBackend{}})
	gw := newServer(t, reg{"pi": Remote{URL: pi.URL, Token: tok}}, func(o *Options) { o.Token = "gw" })

	_, body := get(t, gw.URL+"/api/v1/servers/pi/apps/web", "gw")
	var st core.Status
	if json.Unmarshal([]byte(body), &st) != nil || !st.Running {
		t.Fatalf("remote status: %s", body)
	}
	if r, _ := get(t, gw.URL+"/api/v1/servers/pi/apps/nope", "gw"); r.StatusCode != 404 {
		t.Fatalf("remote 404 not propagated: %d", r.StatusCode)
	}
	_, body = get(t, gw.URL+"/api/v1/servers/pi/apps/web/logs?tail=7", "gw")
	if !strings.Contains(body, "data: line2 tail=7") {
		t.Fatalf("remote logs:\n%s", body)
	}
	if strings.Contains(body, "data: \n\ndata") || strings.Count(body, "data: \n") > 1 {
		t.Fatalf("relay added empty lines:\n%s", body)
	}

	bad := newServer(t, reg{"pi": Remote{URL: pi.URL, Token: "wrong"}}, func(o *Options) { o.Token = "gw" })
	if r, _ := get(t, bad.URL+"/api/v1/servers/pi/apps", "gw"); r.StatusCode != 502 {
		t.Fatalf("bad remote token should be 502, got %d", r.StatusCode)
	}
}

func TestLoopbackOnly(t *testing.T) {
	s := newServer(t, reg{}, func(o *Options) { o.LoopbackOnly = true })
	req, _ := http.NewRequest("GET", s.URL+"/", nil)
	req.Host = "evil.example.com"
	r, _ := http.DefaultClient.Do(req)
	if r.StatusCode != 403 {
		t.Fatalf("rebinding host allowed: %d", r.StatusCode)
	}
}
