// Package api is shipyard's HTTP API (/api/v1) and the host of the web UI.
//
// The same handler runs in two places:
//   - `shipyard serve` on a server: one backend, "local".
//   - `shipyard ui` on a workstation: "local" plus every configured server,
//     reached over HTTP; the browser only ever talks to the workstation and
//     server tokens never reach it.
package api

import (
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

// ServerInfo is an entry of GET /api/v1/servers.
type ServerInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // local | remote
	URL     string `json:"url,omitempty"`
	Default bool   `json:"default"`
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
}

// Handler returns the API + UI handler.
func Handler(o Options) http.Handler {
	h := &handler{o: o}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/session", h.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
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
	tok := ""
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		tok = strings.TrimPrefix(a, "Bearer ")
	} else if c, err := r.Cookie(cookieName); err == nil {
		tok = c.Value
	}
	return tok != "" && h.o.Token != "" &&
		subtle.ConstantTimeCompare([]byte(tok), []byte(h.o.Token)) == 1
}

func (h *handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
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

	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				sw.comment("ping")
			case <-done:
				return
			}
		}
	}()

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
		case errors.Is(err, core.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
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
