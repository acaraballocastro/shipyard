package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shipyard/internal/core"
)

func TestJobsSurviveRestart(t *testing.T) {
	withApp(t, "web")
	release := make(chan struct{})
	Actions["test"] = testAction(release)
	defer delete(Actions, "test")
	dir := t.TempDir()

	a, err := OpenJobs(&core.Service{}, dir, "p-")
	if err != nil {
		t.Fatal(err)
	}
	done, _ := a.Start("web", "test")
	if !strings.HasPrefix(done.ID, "p-") {
		t.Fatalf("prefix missing: %s", done.ID)
	}
	close(release)
	_ = a.Stream(context.Background(), done.ID, func(string, string) error { return nil })

	// A job still running when the process "dies".
	Actions["hang"] = testAction(make(chan struct{}))
	defer delete(Actions, "hang")
	hung, err := a.Start("web", "hang")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	b, err := OpenJobs(&core.Service{}, dir, "p-")
	if err != nil {
		t.Fatal(err)
	}
	list := b.List("web")
	if len(list) != 2 {
		t.Fatalf("reloaded %d jobs", len(list))
	}
	got, _ := b.Get(done.ID)
	if got.Status != JobSucceeded {
		t.Fatalf("finished job: %+v", got)
	}
	var out []string
	_ = b.Stream(context.Background(), done.ID, func(kind, data string) error {
		out = append(out, kind+":"+data)
		return nil
	})
	if !strings.Contains(strings.Join(out, "\n"), "output:out1") {
		t.Fatalf("output not replayed: %v", out)
	}
	h, _ := b.Get(hung.ID)
	if h.Status != JobFailed || !strings.Contains(h.Error, "interrupted") {
		t.Fatalf("interrupted job: %+v", h)
	}
	// Interrupted jobs must not block new ones on the same app.
	next, err := b.Start("web", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Let every goroutine finish writing before the temp dir is removed.
	wait := func(j *Jobs, id string) {
		_ = j.Stream(context.Background(), id, func(string, string) error { return nil })
	}
	wait(b, next.ID)
	_ = a.Cancel(hung.ID)
	wait(a, hung.ID)
}

func TestOpenJobsSkipsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0600)
	j, err := OpenJobs(&core.Service{}, dir, "")
	if j == nil || err == nil || len(j.List("")) != 0 {
		t.Fatalf("want manager + warning, got %v %v", j, err)
	}
}

func TestStatsHistoryThrottlesAndRecords(t *testing.T) {
	h := NewStatsHistory()
	calls := 0
	fetch := func(context.Context) (map[string]core.Stats, error) {
		calls++
		return map[string]core.Stats{"web": {CPU: float64(calls)}}, nil
	}
	for i := 0; i < 3; i++ {
		if _, err := h.Sample(context.Background(), fetch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("docker called %d times within the throttle window", calls)
	}
	h.last = time.Now().Add(-time.Minute)
	_, _ = h.Sample(context.Background(), fetch)
	if got := h.History()["web"]; len(got) != 2 || got[1].CPU != 2 {
		t.Fatalf("history: %+v", got)
	}
}

func TestUpgradeAndHistoryRoutes(t *testing.T) {
	pi := newServer(t, reg{LocalServer: fakeBackend{}})
	gw := newServer(t, reg{"pi": Remote{URL: pi.URL, Token: tok}}, func(o *Options) { o.Token = "gw" })
	r, body := postJSON(t, gw.URL+"/api/v1/servers/pi/upgrade", "gw", "")
	if r.StatusCode != 400 || !strings.Contains(body, "shipyard server upgrade") {
		t.Fatalf("upgrade without SSH: %d %s", r.StatusCode, body)
	}
	_, body = get(t, gw.URL+"/api/v1/servers/pi/stats/history", "gw")
	var hist map[string][]Sample
	if json.Unmarshal([]byte(body), &hist) != nil || len(hist["web"]) != 1 {
		t.Fatalf("history: %s", body)
	}
	info, _ := Local{}.Info(context.Background())
	if info.API != APILevel {
		t.Fatalf("local api level %d", info.API)
	}
}
