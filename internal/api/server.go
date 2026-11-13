// Package api is shipyard's HTTP API (/api/v1) and the host of the web UI.
//
// The same handler runs in two places:
//   - `shipyard serve` on a server: one backend, "local".
//   - `shipyard ui` on a workstation: "local" plus every configured server,
//     reached over HTTP; the browser only ever talks to the workstation and
//     server tokens never reach it.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"shipyard/internal/core"
)

// LocalServer is the name of the machine running the handler.
const LocalServer = "local"

const cookieName = "shipyard_session"

// csrfHeader must accompany state-changing requests. Browsers cannot set a
// custom header cross-origin without a CORS preflight, which this server
// never approves, so a third-party page cannot trigger actions.
const csrfHeader = "X-Shipyard"

// ServerInfo is an entry of GET /api/v1/servers.
type ServerInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // local | remote
	URL     string `json:"url,omitempty"`
	Default bool   `json:"default"`
	// Upgradable: this handler can update shipyard on the server (over SSH).
	Upgradable bool `json:"upgradable,omitempty"`
	// Routes lists the ways to reach the server and which one is in use.
	Routes []RouteStatus `json:"routes,omitempty"`
}

// Upgrader is implemented by backends that can update the shipyard
// running on their server (the workstation gateway, over SSH).
type Upgrader interface {
	Upgrade(ctx context.Context) (*Job, error)
}

// Registry resolves server names to backends.
type Registry interface {
	Servers() []ServerInfo
	Backend(name string) (Backend, bool)
}

// Options configures Handler.
type Options struct {
	Registry Registry
	// Token authenticates requests (Bearer header or session cookie).
	Token string
	// UI is the built frontend (index.html at its root); nil disables it.
	UI fs.FS
	// LoopbackOnly rejects requests whose Host is not localhost, which
	// blocks DNS-rebinding attacks against a UI bound to 127.0.0.1.
	LoopbackOnly bool

	// Access, when set, requires a valid Cloudflare Access token on every
	// request that arrives through Cloudflare. LAN and SSH-tunnel requests
	// are unaffected (they still need the shipyard token).
	Access *AccessVerifier
	// AccessLogin lets a verified Access user in without the shipyard
	// token (browser sign-in through Cloudflare). Service tokens never
	// count as a login.
	AccessLogin bool
	// AllowedEmails restricts AccessLogin to these users (empty: anyone
	// your Access policy lets through).
	AllowedEmails []string
}

type ctxKey int

const identityKey ctxKey = 0

func identityFrom(r *http.Request) *Identity {
	id, _ := r.Context().Value(identityKey).(*Identity)
	return id
}

// Handler returns the API + UI handler.
func Handler(o Options) http.Handler {
	h := &handler{o: o}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/session", h.auth(func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{"authenticated": true}
		if id := identityFrom(r); id != nil {
			out["access"] = true // behind Cloudflare Access: sign out there
			if id.Email != "" {
				out["email"] = id.Email
			}
		}
		writeJSON(w, http.StatusOK, out)
	}))
	mux.HandleFunc("POST /api/v1/session", h.login)
	mux.HandleFunc("DELETE /api/v1/session", h.logout)

	mux.HandleFunc("GET /api/v1/servers", h.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, o.Registry.Servers())
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/info", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		info, err := b.Info(r.Context())
		respond(w, info, err)
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/apps", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		apps, err := b.Apps(r.Context())
		if apps == nil && err == nil {
			apps = []core.App{}
		}
		respond(w, apps, err)
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/apps/{app}", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		st, err := b.Status(r.Context(), r.PathValue("app"))
		respond(w, st, err)
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/apps/{app}/logs", h.backend(h.logs))

	mux.HandleFunc("POST /api/v1/servers/{server}/apps/{app}/actions/{action}", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		job, err := b.StartAction(r.Context(), r.PathValue("app"), r.PathValue("action"))
		if err != nil {
			respond(w, nil, err)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/stats", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		stats, err := b.Stats(r.Context())
		if stats == nil && err == nil {
			stats = map[string]core.Stats{}
		}
		respond(w, stats, err)
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/stats/history", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		hist, err := b.StatsHistory(r.Context())
		if hist == nil && err == nil {
			hist = map[string][]Sample{}
		}
		respond(w, hist, err)
	}))
	mux.HandleFunc("POST /api/v1/servers/{server}/deploy", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		var req core.DeployRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid deploy request: "+err.Error())
			return
		}
		job, err := b.StartDeploy(r.Context(), req)
		if err != nil {
			respond(w, nil, err)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	}))
	mux.HandleFunc("POST /api/v1/servers/{server}/upgrade", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		u, ok := b.(Upgrader)
		if !ok {
			writeError(w, http.StatusBadRequest, ErrInvalidRequest.Error()+
				": this server cannot be updated from here; run shipyard server upgrade from your workstation")
			return
		}
		job, err := u.Upgrade(r.Context())
		if err != nil {
			respond(w, nil, err)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/jobs", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		jobs, err := b.ListJobs(r.Context(), r.URL.Query().Get("app"))
		if jobs == nil && err == nil {
			jobs = []Job{}
		}
		respond(w, jobs, err)
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/jobs/{id}", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		job, err := b.Job(r.Context(), r.PathValue("id"))
		respond(w, job, err)
	}))
	mux.HandleFunc("POST /api/v1/servers/{server}/jobs/{id}/cancel", h.backend(func(w http.ResponseWriter, r *http.Request, b Backend) {
		if err := b.CancelJob(r.Context(), r.PathValue("id")); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("GET /api/v1/servers/{server}/jobs/{id}/events", h.backend(h.jobEvents))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	if o.UI != nil {
		mux.Handle("/", spa(o.UI))
	}

	return h.guard(mux)
}

type handler struct{ o Options }

// =========================
// MIDDLEWARE
// =========================

func (h *handler) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.o.LoopbackOnly && !loopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Header.Get(cfConnectingIP) != "" && h.o.Access != nil {
			id, err := h.o.Access.FromRequest(r)
			if err != nil {
				writeError(w, http.StatusForbidden, "cloudflare access: "+err.Error())
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), identityKey, id))
		}
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *handler) authorized(r *http.Request) bool {
	if id := identityFrom(r); id != nil && h.o.AccessLogin && id.Email != "" && h.emailAllowed(id.Email) {
		return true
	}
	tok := ""
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		tok = strings.TrimPrefix(a, "Bearer ")
	} else if c, err := r.Cookie(cookieName); err == nil {
		tok = c.Value
	}
	return tok != "" && h.o.Token != "" &&
		subtle.ConstantTimeCompare([]byte(tok), []byte(h.o.Token)) == 1
}

func (h *handler) emailAllowed(email string) bool {
	if len(h.o.AllowedEmails) == 0 {
		return true
	}
	for _, e := range h.o.AllowedEmails {
		if strings.EqualFold(strings.TrimSpace(e), email) {
			return true
		}
	}
	return false
}

func (h *handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(csrfHeader) == "" {
			writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
			return
		}
		next(w, r)
	}
}

func (h *handler) backend(next func(http.ResponseWriter, *http.Request, Backend)) http.HandlerFunc {
	return h.auth(func(w http.ResponseWriter, r *http.Request) {
		b, ok := h.o.Registry.Backend(r.PathValue("server"))
		if !ok {
			writeError(w, http.StatusNotFound, "unknown server "+r.PathValue("server"))
			return
		}
		next(w, r, b)
	})
}

// =========================
// SESSION
// =========================

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(csrfHeader) == "" {
		writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if h.o.Token == "" || subtle.ConstantTimeCompare([]byte(body.Token), []byte(h.o.Token)) != 1 {
		time.Sleep(500 * time.Millisecond) // slow down guessing
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: body.Token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure(r),
		MaxAge: 30 * 24 * 3600,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure(r)})
	w.WriteHeader(http.StatusNoContent)
}

func secure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// =========================
// LOGS (Server-Sent Events)
// =========================

// logs streams lines as SSE "data:" messages. An "event: error" message
// carries a failure; a comment ping every 15s keeps proxies from closing
// idle streams.
func (h *handler) logs(w http.ResponseWriter, r *http.Request, b Backend) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 || tail > 10000 {
		tail = 200
	}
	opts := core.LogsOptions{Tail: tail, Follow: r.URL.Query().Get("follow") == "1"}

	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-cache")
	hd.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sw := &sseWriter{w: w, fl: fl}
	sw.comment("connected")
	stop := sw.keepAlive()
	defer stop()

	err := b.Logs(r.Context(), r.PathValue("app"), opts, sw)
	sw.flushPartial()
	if err != nil && r.Context().Err() == nil {
		sw.event("error", err.Error())
	} else {
		sw.event("end", "")
	}
}

type sseWriter struct {
	mu      sync.Mutex
	w       io.Writer
	fl      http.Flusher
	partial []byte
}

// Write splits the input into lines, one SSE message each.
func (s *sseWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partial = append(s.partial, p...)
	for {
		i := strings.IndexByte(string(s.partial), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(s.partial[:i]), "\r")
		s.partial = s.partial[i+1:]
		if _, err := io.WriteString(s.w, "data: "+line+"\n\n"); err != nil {
			return 0, err
		}
	}
	s.fl.Flush()
	return len(p), nil
}

func (s *sseWriter) flushPartial() {
	s.mu.Lock()
	rest := string(s.partial)
	s.partial = nil
	s.mu.Unlock()
	if rest != "" {
		_, _ = s.Write([]byte(rest + "\n"))
	}
}

// keepAlive sends a comment every 15s until the returned stop is called.
func (s *sseWriter) keepAlive() (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.comment("ping")
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

func (s *sseWriter) comment(c string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = io.WriteString(s.w, ": "+c+"\n\n")
	s.fl.Flush()
}

func (s *sseWriter) event(name, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = io.WriteString(s.w, "event: "+name+"\ndata: "+strings.ReplaceAll(data, "\n", " ")+"\n\n")
	s.fl.Flush()
}

// jobEvents streams a job as SSE: progress / output / done events, from
// the beginning, so a reconnecting browser sees the whole history.
func (h *handler) jobEvents(w http.ResponseWriter, r *http.Request, b Backend) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	id := r.PathValue("id")
	if _, err := b.Job(r.Context(), id); err != nil {
		respond(w, nil, err)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-cache")
	hd.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sw := &sseWriter{w: w, fl: fl}
	sw.comment("connected")
	stop := sw.keepAlive()
	defer stop()

	err := b.JobEvents(r.Context(), id, func(kind, data string) error {
		sw.event(kind, data)
		return nil
	})
	if err != nil && r.Context().Err() == nil {
		sw.event("error", err.Error())
	}
}

// =========================
// UI
// =========================

// spa serves static files and falls back to index.html for client routes.
func spa(ui fs.FS) http.Handler {
	files := http.FileServer(http.FS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(ui, p); err != nil {
			index, err := fs.ReadFile(ui, "index.html")
			if err != nil {
				http.Error(w, "web UI not built (run: make web)", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(index)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

// =========================
// JSON
// =========================

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		var re *RemoteError
		switch {
		case errors.Is(err, core.ErrNotFound), errors.Is(err, ErrJobNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrBusy):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrUnknownAction), errors.Is(err, ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.As(err, &re):
			writeError(w, http.StatusBadGateway, re.Message)
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
