package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"shipyard/internal/core"
)

// deadURL is an address nothing listens on: dialing fails immediately.
func deadURL(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr
}

func TestRoutesFailoverAndFailback(t *testing.T) {
	pi := newServer(t, reg{LocalServer: fakeBackend{}})
	dead := deadURL(t)
	opened := 0
	rs := NewRoutes(
		Route{Name: "cloudflare", URL: dead, Header: http.Header{"Cf-Access-Client-Id": {"x"}}},
		Route{Name: "lan", Open: func(context.Context) (string, error) { opened++; return pi.URL, nil }},
	)
	b := Remote{Token: tok, Routes: rs}

	if _, err := b.Apps(context.Background()); err != nil {
		t.Fatalf("should fail over: %v", err)
	}
	if rs.Active() != "lan" || opened != 1 {
		t.Fatalf("active %q opened %d", rs.Active(), opened)
	}
	st := rs.Status()
	if st[0].Error == "" || st[1].Error != "" || !st[1].Active {
		t.Fatalf("status: %+v", st)
	}

	// While on the fallback, it is tried first (no repeated dial to the dead route).
	_, _ = b.Apps(context.Background())
	if o := rs.order(); o[0] != 1 {
		t.Fatalf("fallback not preferred during window: %v", o)
	}

	// After the window the preferred route is tried again.
	rs.mu.Lock()
	rs.switched = time.Now().Add(-2 * failbackAfter)
	rs.mu.Unlock()
	if o := rs.order(); o[0] != 0 {
		t.Fatalf("failback not attempted: %v", o)
	}
	// It still fails, so the window restarts.
	_, _ = b.Apps(context.Background())
	if o := rs.order(); o[0] != 1 {
		t.Fatalf("window not restarted after failed failback: %v", o)
	}

	// State-changing requests also fail over when the connection never opened.
	rs.mu.Lock()
	rs.active = -1
	rs.mu.Unlock()
	if _, err := b.StartDeploy(context.Background(), deployReq("nginx")); err != nil {
		t.Fatalf("POST failover: %v", err)
	}
}

func deployReq(image string) core.DeployRequest { return core.DeployRequest{Image: image} }

func TestRoutesAllDown(t *testing.T) {
	b := Remote{Token: tok, Routes: NewRoutes(
		Route{Name: "a", URL: deadURL(t)},
		Route{Name: "b", Open: func(context.Context) (string, error) { return "", errors.New("tunnel failed") }},
	)}
	_, err := b.Info(context.Background())
	if err == nil || !strings.Contains(err.Error(), "a: ") || !strings.Contains(err.Error(), "tunnel failed") {
		t.Fatalf("want every route's error, got %v", err)
	}
}

func TestRouteHeadersSent(t *testing.T) {
	got := make(chan string, 1)
	srv := newServer(t, reg{LocalServer: fakeBackend{}})
	proxy := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("CF-Access-Client-Id")
		http.Redirect(w, r, srv.URL+r.URL.Path, http.StatusTemporaryRedirect)
	})}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	go proxy.Serve(l)
	defer proxy.Close()
	b := Remote{Token: tok, Routes: NewRoutes(Route{Name: "cf", URL: "http://" + l.Addr().String(),
		Header: http.Header{"Cf-Access-Client-Id": {"abc"}}})}
	_, _ = b.Info(context.Background())
	if v := <-got; v != "abc" {
		t.Fatalf("header not sent: %q", v)
	}
}

func TestRetryable(t *testing.T) {
	sent := errors.New("connection reset after request")
	if retryable(http.MethodPost, sent, true) {
		t.Fatal("a POST that may have reached the server must not be repeated")
	}
	if !retryable(http.MethodGet, sent, true) {
		t.Fatal("GETs are always retryable")
	}
	if !retryable(http.MethodPost, dialError{errors.New("refused")}, true) {
		t.Fatal("dial failures are retryable")
	}
}
