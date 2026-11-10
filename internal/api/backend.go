package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"shipyard/internal/core"
	"shipyard/internal/version"
)

// Info describes a shipyard host.
type Info struct {
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

// Backend is one server as seen by the API: the local machine (core) or
// another shipyard reached over HTTP.
type Backend interface {
	Info(ctx context.Context) (*Info, error)
	Apps(ctx context.Context) ([]core.App, error)
	Status(ctx context.Context, app string) (*core.Status, error)
	// Logs writes log lines to w until done or ctx is cancelled.
	Logs(ctx context.Context, app string, opts core.LogsOptions, w io.Writer) error
}

// =========================
// LOCAL
// =========================

// Local serves this machine through core.
type Local struct{ Svc *core.Service }

func (l Local) Info(context.Context) (*Info, error) {
	h, _ := os.Hostname()
	return &Info{Hostname: h, Version: version.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}, nil
}

func (l Local) Apps(context.Context) ([]core.App, error) { return l.Svc.List() }

func (l Local) Status(ctx context.Context, app string) (*core.Status, error) {
	return l.Svc.Status(ctx, app)
}

func (l Local) Logs(ctx context.Context, app string, opts core.LogsOptions, w io.Writer) error {
	return l.Svc.Logs(ctx, app, opts, w)
}

// =========================
// REMOTE
// =========================

// Remote talks to `shipyard serve` on another host.
type Remote struct {
	URL   string // e.g. http://pi.local:7878
	Token string
	HTTP  *http.Client
}

// RemoteError is a non-2xx answer from a remote server.
type RemoteError struct {
	Status  int
	Message string
}

func (e *RemoteError) Error() string { return e.Message }

func (r Remote) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (r Remote) do(ctx context.Context, client *http.Client, path string, q url.Values) (*http.Response, error) {
	u := strings.TrimRight(r.URL, "/") + "/api/v1/servers/" + LocalServer + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("server unreachable: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		var e struct{ Error string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		if resp.StatusCode == http.StatusNotFound && strings.Contains(e.Error, core.ErrNotFound.Error()) {
			return nil, fmt.Errorf("%w: %s", core.ErrNotFound, path)
		}
		return nil, &RemoteError{Status: resp.StatusCode, Message: e.Error}
	}
	return resp, nil
}

func (r Remote) getJSON(ctx context.Context, path string, out any) error {
	resp, err := r.do(ctx, r.client(), path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (r Remote) Info(ctx context.Context) (*Info, error) {
	var i Info
	return &i, r.getJSON(ctx, "/info", &i)
}

func (r Remote) Apps(ctx context.Context) ([]core.App, error) {
	var a []core.App
	return a, r.getJSON(ctx, "/apps", &a)
}

func (r Remote) Status(ctx context.Context, app string) (*core.Status, error) {
	var s core.Status
	return &s, r.getJSON(ctx, "/apps/"+url.PathEscape(app), &s)
}

// Logs relays the remote SSE stream as plain lines.
func (r Remote) Logs(ctx context.Context, app string, opts core.LogsOptions, w io.Writer) error {
	q := url.Values{"tail": {strconv.Itoa(opts.Tail)}}
	if opts.Follow {
		q.Set("follow", "1")
	}
	// No client timeout: the stream lives as long as ctx.
	resp, err := r.do(ctx, &http.Client{}, "/apps/"+url.PathEscape(app)+"/logs", q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Parse SSE: messages are separated by blank lines; only unnamed
	// messages are log lines, "error" carries a failure, "end" finishes.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			event = ""
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
			if event == "end" {
				return nil
			}
		case strings.HasPrefix(line, "data: "):
			d := strings.TrimPrefix(line, "data: ")
			if event == "error" {
				return errors.New(d)
			}
			if event == "" {
				if _, err := io.WriteString(w, d+"\n"); err != nil {
					return err
				}
			}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return sc.Err()
}
