package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"shipyard/internal/api"
	"shipyard/internal/config"
	"shipyard/internal/remote"
)

// buildRoutes turns a server's configured routes into a failover set. The
// returned close stops any SSH tunnel it started.
func buildRoutes(srv config.Server) (*api.Routes, func()) {
	var list []api.Route
	var closers []func()
	for _, r := range srv.EffectiveRoutes() {
		rt, c := buildRoute(srv, r)
		list = append(list, rt)
		closers = append(closers, c)
	}
	return api.NewRoutes(list...), func() {
		for _, c := range closers {
			c()
		}
	}
}

// buildRoute makes one route usable: headers, and an SSH tunnel for the
// automatic ssh route (started lazily; close stops it).
func buildRoute(srv config.Server, r config.Route) (api.Route, func()) {
	rt := api.Route{Name: r.Name, URL: r.URL}
	if len(r.Headers) > 0 {
		rt.Header = http.Header{}
		for k, v := range r.Headers {
			rt.Header.Set(k, v)
		}
	}
	if r.Name != config.SSHRoute || r.URL != "" {
		return rt, func() {}
	}
	t := &remote.Tunnel{Host: srv.Host, Target: "127.0.0.1:" + servePort(srv.URL)}
	rt.Open = func(ctx context.Context) (string, error) {
		addr, err := t.Addr(ctx)
		if err != nil {
			return "", err
		}
		return "http://" + addr, nil
	}
	return rt, t.Close
}

// servePort is the API port on the server: from its URL, else the default.
func servePort(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Port() != "" {
		return u.Port()
	}
	return DefaultServePort
}

// remoteBackend is the API client for a configured server.
func remoteBackend(srv config.Server) (api.Remote, func()) {
	rs, closeFn := buildRoutes(srv)
	return api.Remote{Token: srv.Token, Routes: rs}, closeFn
}

// routeCache keeps one failover set per server for the life of
// `shipyard ui`, so the working route and its tunnel are reused across
// requests. A config change for a server rebuilds its entry.
type routeCache struct {
	mu sync.Mutex
	m  map[string]cachedRoutes
}

type cachedRoutes struct {
	key   string
	rs    *api.Routes
	close func()
}

func newRouteCache() *routeCache { return &routeCache{m: map[string]cachedRoutes{}} }

func (c *routeCache) get(name string, srv config.Server) *api.Routes {
	key := fmt.Sprintf("%s|%v|%v|%s", srv.Host, srv.EffectiveRoutes(), srv.NoSSHFallback, srv.URL)
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[name]; ok {
		if e.key == key {
			return e.rs
		}
		e.close()
	}
	rs, closeFn := buildRoutes(srv)
	c.m[name] = cachedRoutes{key: key, rs: rs, close: closeFn}
	return rs
}

// peek returns the cached set without creating one.
func (c *routeCache) peek(name string) *api.Routes {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[name].rs
}

func (c *routeCache) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.m {
		e.close()
	}
	c.m = map[string]cachedRoutes{}
}
