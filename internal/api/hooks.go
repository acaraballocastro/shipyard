package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"shipyard/internal/core"
)

// Webhooks let GitHub redeploy a repository service on push: POST
// /hooks/github/{app}, signed with a per-service secret (HMAC-SHA256, the
// X-Hub-Signature-256 header). The route needs no shipyard token (GitHub
// cannot send one); the signature is the authentication.

// WebhookInfo is a service's webhook as shown to the UI. Secret is only
// filled when it was just created or rotated.
type WebhookInfo struct {
	Enabled bool      `json:"enabled"`
	Branch  string    `json:"branch,omitempty"`
	Path    string    `json:"path,omitempty"` // /hooks/github/<app>
	URL     string    `json:"url,omitempty"`  // public URL, when known
	Secret  string    `json:"secret,omitempty"`
	Last    *Delivery `json:"last,omitempty"`
}

// Delivery records the latest webhook call.
type Delivery struct {
	At     time.Time `json:"at" yaml:"at"`
	Event  string    `json:"event" yaml:"event"`
	Ref    string    `json:"ref,omitempty" yaml:"ref,omitempty"`
	Commit string    `json:"commit,omitempty" yaml:"commit,omitempty"`
	Result string    `json:"result" yaml:"result"` // deployed | ignored | busy | rejected
	Detail string    `json:"detail,omitempty" yaml:"detail,omitempty"`
	JobID  string    `json:"jobId,omitempty" yaml:"job_id,omitempty"`
}

type webhookEntry struct {
	Secret string    `yaml:"secret"`
	Branch string    `yaml:"branch"`
	Last   *Delivery `yaml:"last,omitempty"`
}

// Hooks stores webhook settings (0600 YAML) and serves deliveries.
type Hooks struct {
	Path string // ~/.shipyard/webhooks.yaml
	Jobs *Jobs
	// PublicURL is the base URL GitHub can reach ("https://shipyard.example.com").
	PublicURL string

	mu sync.Mutex
}

func (h *Hooks) load() (map[string]webhookEntry, error) {
	b, err := os.ReadFile(h.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]webhookEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]webhookEntry{}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", h.Path, err)
	}
	return m, nil
}

func (h *Hooks) save(m map[string]webhookEntry) error {
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.Path), 0700); err != nil {
		return err
	}
	tmp := h.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, h.Path)
}

func (h *Hooks) info(app string, e webhookEntry, withSecret bool) *WebhookInfo {
	path := "/hooks/github/" + app
	in := &WebhookInfo{Enabled: true, Branch: e.Branch, Path: path, Last: e.Last}
	if h.PublicURL != "" {
		in.URL = strings.TrimRight(h.PublicURL, "/") + path
	}
	if withSecret {
		in.Secret = e.Secret
	}
	return in
}

// repoApp returns the service, which must be a repository deployment.
func (h *Hooks) repoApp(app string) (core.App, error) {
	apps, err := h.Jobs.Svc.List()
	if err != nil {
		return core.App{}, err
	}
	for _, a := range apps {
		if a.Name == app {
			if a.Repo == "" {
				return a, fmt.Errorf("%w: %s is deployed from an image; webhooks redeploy repository services", ErrInvalidRequest, app)
			}
			return a, nil
		}
	}
	return core.App{}, fmt.Errorf("%w: %s", core.ErrNotFound, app)
}

// Get returns the webhook of app (Enabled false when none).
func (h *Hooks) Get(app string) (*WebhookInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, err := h.load()
	if err != nil {
		return nil, err
	}
	e, ok := m[app]
	if !ok {
		return &WebhookInfo{}, nil
	}
	return h.info(app, e, false), nil
}

// Set enables (or updates) the webhook of app. A secret is generated on
// first use or when rotate is true, and returned only then.
func (h *Hooks) Set(app, branch string, rotate bool) (*WebhookInfo, error) {
	if _, err := h.repoApp(app); err != nil {
		return nil, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	if strings.ContainsAny(branch, " \n\t~^:?*[\\") {
		return nil, fmt.Errorf("%w: invalid branch %q", ErrInvalidRequest, branch)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	m, err := h.load()
	if err != nil {
		return nil, err
	}
	e, existed := m[app]
	fresh := !existed || rotate || e.Secret == ""
	if fresh {
		e.Secret = NewToken()
	}
	e.Branch = branch
	m[app] = e
	if err := h.save(m); err != nil {
		return nil, err
	}
	return h.info(app, e, fresh), nil
}

// Delete disables the webhook of app.
func (h *Hooks) Delete(app string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, err := h.load()
	if err != nil {
		return err
	}
	delete(m, app)
	return h.save(m)
}

func (h *Hooks) record(app string, d Delivery) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, err := h.load()
	if err != nil {
		return
	}
	if e, ok := m[app]; ok {
		d.At = time.Now()
		e.Last = &d
		m[app] = e
		_ = h.save(m)
	}
}

// ServeHTTP handles POST /hooks/github/{app}.
func (h *Hooks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable body")
		return
	}

	h.mu.Lock()
	m, err := h.load()
	h.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "webhook settings unreadable")
		return
	}
	e, ok := m[app]
	if !ok || !validSignature(e.Secret, body, r.Header.Get("X-Hub-Signature-256")) {
		// Same answer for unknown services and bad signatures.
		time.Sleep(200 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	switch event {
	case "ping":
		h.record(app, Delivery{Event: "ping", Result: "ignored", Detail: "GitHub ping: webhook works"})
		writeJSON(w, http.StatusOK, map[string]string{"ok": "pong"})
		return
	case "push":
	default:
		h.record(app, Delivery{Event: event, Result: "ignored", Detail: "only push events deploy"})
		writeJSON(w, http.StatusOK, map[string]string{"ignored": "event " + event})
		return
	}

	var push struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &push); err != nil {
		writeError(w, http.StatusBadRequest, "invalid push payload")
		return
	}
	d := Delivery{Event: "push", Ref: push.Ref, Commit: short7(push.After)}
	a, err := h.repoApp(app)
	if err != nil {
		d.Result, d.Detail = "rejected", err.Error()
		h.record(app, d)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !strings.EqualFold(a.Repo, push.Repository.FullName) {
		d.Result, d.Detail = "rejected", fmt.Sprintf("push from %s, service deploys %s", push.Repository.FullName, a.Repo)
		h.record(app, d)
		writeError(w, http.StatusUnprocessableEntity, d.Detail)
		return
	}
	if push.Deleted || push.Ref != "refs/heads/"+e.Branch {
		d.Result, d.Detail = "ignored", "not a push to "+e.Branch
		h.record(app, d)
		writeJSON(w, http.StatusOK, map[string]string{"ignored": d.Detail})
		return
	}

	job, err := h.Jobs.Start(app, "update")
	if errors.Is(err, ErrBusy) {
		d.Result, d.Detail = "busy", err.Error()
		h.record(app, d)
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		d.Result, d.Detail = "rejected", err.Error()
		h.record(app, d)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.Result, d.JobID = "deployed", job.ID
	h.record(app, d)
	writeJSON(w, http.StatusAccepted, job)
}

func validSignature(secret string, body []byte, header string) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok || secret == "" {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

func short7(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
