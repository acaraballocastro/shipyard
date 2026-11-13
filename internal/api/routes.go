package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Route is one way to reach a server's API: a LAN address, a public URL
// behind Cloudflare, a VPN address, an SSH tunnel…
type Route struct {
	Name string
	// URL is the base URL ("https://shipyard.example.com").
	URL string
	// Open, when set instead of URL, provides the base URL on demand
	// (e.g. starts an SSH tunnel). It is called before every request and
	// must be cheap once the route is up.
	Open func(ctx context.Context) (string, error)
	// Header is added to every request (e.g. Cloudflare Access service
	// token: CF-Access-Client-Id / CF-Access-Client-Secret).
	Header http.Header
}

func (r Route) base(ctx context.Context) (string, error) {
	if r.Open != nil {
		return r.Open(ctx)
	}
	return r.URL, nil
}

// failbackAfter is how long a server stays on a fallback route before the
// preferred ones are tried again.
const failbackAfter = time.Minute

// Routes is an ordered list of routes to one server (most preferred
// first) that remembers which one works. When a route cannot be reached,
// requests move on to the next; after failbackAfter the preferred routes
// get another chance. This keeps the UI working when one path is down,
// e.g. Cloudflare IPs blocked by an ISP.
type Routes struct {
	list []Route

	mu       sync.Mutex
	active   int
	switched time.Time
	lastErr  map[int]string
}

// NewRoutes returns a failover set; it needs at least one route.
func NewRoutes(rs ...Route) *Routes {
	return &Routes{list: rs, active: -1, lastErr: map[int]string{}}
}

// RouteStatus describes one route for display.
type RouteStatus struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Error  string `json:"error,omitempty"` // last failure, if any
}

// Status reports every route and which one is in use.
func (r *Routes) Status() []RouteStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RouteStatus, len(r.list))
	for i, rt := range r.list {
		out[i] = RouteStatus{Name: rt.Name, Active: i == r.active, Error: r.lastErr[i]}
	}
	return out
}

// Active is the name of the route in use ("" before the first request).
func (r *Routes) Active() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active < 0 {
		return ""
	}
	return r.list[r.active].Name
}

// order is the sequence of routes to try for the next request.
func (r *Routes) order() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int, 0, len(r.list))
	if r.active > 0 && time.Since(r.switched) < failbackAfter {
		out = append(out, r.active)
	}
	for i := range r.list {
		if len(out) == 0 || out[0] != i {
			out = append(out, i)
		}
	}
	return out
}

func (r *Routes) ok(i int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.lastErr, i)
	if r.active != i {
		r.active = i
		r.switched = time.Now()
	} else if i > 0 && time.Since(r.switched) >= failbackAfter {
		r.switched = time.Now() // preferred routes just failed again
	}
}

func (r *Routes) failed(i int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr[i] = err.Error()
}

// =========================
// TRANSPORT
// =========================

// transport fails fast on unreachable routes: a blocked IP usually shows
// up as a dial or TLS handshake that never completes.
var transport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&dialer{d: net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}}).DialContext,
	TLSHandshakeTimeout:   7 * time.Second,
	ResponseHeaderTimeout: 0, // streams and slow handlers are bounded by the client/ctx
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConnsPerHost:   4,
	ForceAttemptHTTP2:     true,
}

// dialError marks failures before anything was sent: always safe to retry
// on another route, even for state-changing requests.
type dialError struct{ err error }

func (e dialError) Error() string { return e.err.Error() }
func (e dialError) Unwrap() error { return e.err }

type dialer struct{ d net.Dialer }

func (d *dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	c, err := d.d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, dialError{err}
	}
	return c, nil
}

// retryable tells whether a failed attempt may move on to the next route.
// GETs always may; other methods only when the request never left
// (connection or tunnel setup failed), so an action is never sent twice.
func retryable(method string, err error, sent bool) bool {
	if !sent {
		return true
	}
	if method == http.MethodGet {
		return true
	}
	var de dialError
	return errors.As(err, &de) || isTLSHandshake(err)
}

func isTLSHandshake(err error) bool {
	return strings.Contains(err.Error(), "TLS handshake")
}
