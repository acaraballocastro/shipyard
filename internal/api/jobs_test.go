package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shipyard/internal/core"
	"shipyard/internal/events"
)

// withApp registers a fake deployed app in an isolated HOME.
func withApp(t *testing.T, name string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := fmt.Sprintf("apps:\n  %s:\n    image: nginx\n    mode: local\n    path: /tmp/x\n", name)
	if err := os.WriteFile(filepath.Join(home, ".shipyard-state.yaml"), []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
}

// testAction emits progress and output, then waits for release or cancel.
func testAction(release chan struct{}) func(*core.Service, context.Context, string, events.Progress) error {
	return func(_ *core.Service, ctx context.Context, app string, p events.Progress) error {
		p.Step("test", "working on %s", app)
		fmt.Fprint(p.Writer(), "out1\nout2")
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func post(t *testing.T, url, token string, csrf bool) (*http.Response, string) {
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if csrf {
		req.Header.Set(csrfHeader, "1")
	}
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

func TestJobLifecycleLocalAndThroughGateway(t *testing.T) {
	withApp(t, "web")
	release := make(chan struct{})
	Actions["test"] = testAction(release)
	defer delete(Actions, "test")

	svc := &core.Service{}
	pi := newServer(t, reg{LocalServer: Local{Svc: svc, Jobs: NewJobs(svc)}})
	gw := newServer(t, reg{"pi": Remote{URL: pi.URL, Token: tok}}, func(o *Options) { o.Token = "gw" })
	base := gw.URL + "/api/v1/servers/pi"

	// CSRF header is required for actions.
	if r, _ := post(t, base+"/apps/web/actions/test", "gw", false); r.StatusCode != 403 {
		t.Fatalf("missing csrf header: %d", r.StatusCode)
	}
	if r, _ := post(t, base+"/apps/web/actions/nope", "gw", true); r.StatusCode != 400 {
		t.Fatalf("unknown action: %d", r.StatusCode)
	}
	if r, _ := post(t, base+"/apps/ghost/actions/test", "gw", true); r.StatusCode != 404 {
		t.Fatalf("unknown app: %d", r.StatusCode)
	}

	r, body := post(t, base+"/apps/web/actions/test", "gw", true)
	if r.StatusCode != 202 {
		t.Fatalf("start: %d %s", r.StatusCode, body)
	}
	var job Job
	if err := json.Unmarshal([]byte(body), &job); err != nil || job.Status != JobRunning {
		t.Fatalf("job: %s %v", body, err)
	}

	// A second action on the same app is rejected while the first runs.
	if r, _ := post(t, base+"/apps/web/actions/test", "gw", true); r.StatusCode != 409 {
		t.Fatalf("concurrent action: %d", r.StatusCode)
	}

	// Release after the stream is open: events must replay from the start
	// and end with done.
	time.AfterFunc(200*time.Millisecond, func() { close(release) })
	_, stream := get(t, base+"/jobs/"+job.ID+"/events", "gw")
	for _, want := range []string{
		"event: progress\ndata: {", `"stage":"test"`, "working on web",
		"event: output\ndata: out1\n", "event: output\ndata: out2\n",
		"event: done\ndata: {", `"status":"succeeded"`,
	} {
		if !strings.Contains(stream, want) {
			t.Errorf("stream missing %q:\n%s", want, stream)
		}
	}

	// Finished: the job is queryable and a reconnect replays everything.
	_, body = get(t, base+"/jobs/"+job.ID, "gw")
	if !strings.Contains(body, `"status":"succeeded"`) {
		t.Fatalf("job after finish: %s", body)
	}
	if _, again := get(t, base+"/jobs/"+job.ID+"/events", "gw"); !strings.Contains(again, "data: out1") {
		t.Fatalf("replay missing output:\n%s", again)
	}
	if r, _ := get(t, base+"/jobs/missing", "gw"); r.StatusCode != 404 {
		t.Fatalf("missing job: %d", r.StatusCode)
	}
}

func TestJobCancel(t *testing.T) {
	withApp(t, "web")
	Actions["test"] = testAction(make(chan struct{}))
	defer delete(Actions, "test")

	jobs := NewJobs(&core.Service{})
	job, err := jobs.Start("web", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	var final Job
	err = jobs.Stream(context.Background(), job.ID, func(kind, data string) error {
		if kind == KindDone {
			return json.Unmarshal([]byte(data), &final)
		}
		return nil
	})
	if err != nil || final.Status != JobCanceled {
		t.Fatalf("cancel: %+v %v", final, err)
	}
	// The app is free again.
	if _, err := jobs.Start("web", "test"); err != nil {
		t.Fatalf("restart after cancel: %v", err)
	}
}

func postJSON(t *testing.T, url, token, body string) (*http.Response, string) {
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(csrfHeader, "1")
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

func TestDeployStatsAndHistoryThroughGateway(t *testing.T) {
	pi := newServer(t, reg{LocalServer: fakeBackend{}})
	gw := newServer(t, reg{"pi": Remote{URL: pi.URL, Token: tok}}, func(o *Options) { o.Token = "gw" })
	base := gw.URL + "/api/v1/servers/pi"

	r, body := postJSON(t, base+"/deploy", "gw", `{"image":"nginx:1.27","name":"edge","ports":["8080:80"]}`)
	if r.StatusCode != 202 || !strings.Contains(body, `"app":"edge"`) || !strings.Contains(body, `"action":"deploy"`) {
		t.Fatalf("deploy: %d %s", r.StatusCode, body)
	}
	if r, body := postJSON(t, base+"/deploy", "gw", `{}`); r.StatusCode != 400 || !strings.Contains(body, "repository or an image") {
		t.Fatalf("empty deploy: %d %s", r.StatusCode, body)
	}
	if r, _ := postJSON(t, base+"/deploy", "gw", `{"image":"x","bogus":1}`); r.StatusCode != 400 {
		t.Fatalf("unknown field: %d", r.StatusCode)
	}

	_, body = get(t, base+"/stats", "gw")
	var stats map[string]core.Stats
	if json.Unmarshal([]byte(body), &stats) != nil || stats["web"].CPU != 1.5 {
		t.Fatalf("stats: %s", body)
	}
	_, body = get(t, base+"/jobs?app=web", "gw")
	var jobs []Job
	if json.Unmarshal([]byte(body), &jobs) != nil || len(jobs) != 1 || jobs[0].ID != "j1" {
		t.Fatalf("jobs: %s", body)
	}
}

func TestJobsListAndDeployLock(t *testing.T) {
	withApp(t, "web")
	release := make(chan struct{})
	Actions["test"] = testAction(release)
	defer delete(Actions, "test")

	jobs := NewJobs(&core.Service{})
	first, err := jobs.Start("web", "test")
	if err != nil {
		t.Fatal(err)
	}
	// A deploy to a service with a running job is rejected.
	if _, err := jobs.StartDeploy(core.DeployRequest{Image: "nginx", Name: "web"}); err == nil || !strings.Contains(err.Error(), ErrBusy.Error()) {
		t.Fatalf("deploy while busy: %v", err)
	}
	if _, err := jobs.StartDeploy(core.DeployRequest{}); err == nil {
		t.Fatal("empty deploy accepted")
	}
	close(release)
	_ = jobs.Stream(context.Background(), first.ID, func(string, string) error { return nil })

	if _, err := jobs.Start("web", "test"); err != nil {
		t.Fatal(err)
	}
	list := jobs.List("web")
	if len(list) != 2 || list[0].StartedAt.Before(list[1].StartedAt) {
		t.Fatalf("list order: %+v", list)
	}
	if len(jobs.List("other")) != 0 {
		t.Fatal("filter by app failed")
	}
}

func TestContainersThroughGateway(t *testing.T) {
	pi := newServer(t, reg{LocalServer: fakeBackend{}})
	gw := newServer(t, reg{"pi": Remote{URL: pi.URL, Token: tok}}, func(o *Options) { o.Token = "gw" })
	base := gw.URL + "/api/v1/servers/pi"

	_, body := get(t, base+"/containers", "gw")
	var cs []core.Container
	if json.Unmarshal([]byte(body), &cs) != nil || len(cs) != 2 || cs[0].Name != "cloudflared" {
		t.Fatalf("containers: %s", body)
	}
	if r, b := post(t, base+"/containers/cloudflared/actions/restart", "gw", true); r.StatusCode != 204 {
		t.Fatalf("restart: %d %s", r.StatusCode, b)
	}
	if r, _ := post(t, base+"/containers/ghost/actions/restart", "gw", true); r.StatusCode != 404 {
		t.Fatalf("missing container: %d", r.StatusCode)
	}
	if r, _ := post(t, base+"/containers/cloudflared/actions/rm", "gw", true); r.StatusCode != 400 {
		t.Fatalf("unknown action: %d", r.StatusCode)
	}
	if r, _ := post(t, base+"/containers/cloudflared/actions/restart", "gw", false); r.StatusCode != 403 {
		t.Fatalf("csrf: %d", r.StatusCode)
	}
	_, logs := get(t, base+"/containers/cloudflared/logs?tail=9", "gw")
	if !strings.Contains(logs, "data: cf-line tail=9") {
		t.Fatalf("logs:\n%s", logs)
	}
}

func TestEnvThroughGateway(t *testing.T) {
	pi := newServer(t, reg{LocalServer: fakeBackend{}})
	gw := newServer(t, reg{"pi": Remote{URL: pi.URL, Token: tok}}, func(o *Options) { o.Token = "gw" })
	r, body := get(t, gw.URL+"/api/v1/servers/pi/apps/web/env", "gw")
	if r.StatusCode != 200 || !strings.Contains(body, `"DB_URL":"postgres://x"`) || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("env: %d %v %s", r.StatusCode, r.Header, body)
	}
	if r, _ := get(t, gw.URL+"/api/v1/servers/pi/apps/ghost/env", "gw"); r.StatusCode != 404 {
		t.Fatalf("missing app: %d", r.StatusCode)
	}
	if r, _ := get(t, gw.URL+"/api/v1/servers/pi/apps/web/env", ""); r.StatusCode != 401 {
		t.Fatalf("env without auth: %d", r.StatusCode)
	}
}
