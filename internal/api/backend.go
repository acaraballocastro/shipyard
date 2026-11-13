package api

import (
	"bufio"
	"bytes"
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

// APILevel grows whenever the API gains endpoints the UI depends on. The
// UI is embedded in the same binary, so it compares a server's level with
// its own to tell when a remote shipyard is too old. Servers that predate
// the field report 0.
//
//	1: jobs (restart, update, delete)
//	2: deploy, stats + history, job history
//	3: every Docker container (list, start/stop/restart, logs)
const APILevel = 3

// Info describes a shipyard host.
type Info struct {
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	API      int    `json:"api"`
}

// Backend is one server as seen by the API: the local machine (core) or
// another shipyard reached over HTTP.
type Backend interface {
	Info(ctx context.Context) (*Info, error)
	Apps(ctx context.Context) ([]core.App, error)
	Status(ctx context.Context, app string) (*core.Status, error)
	// Logs writes log lines to w until done or ctx is cancelled.
	Logs(ctx context.Context, app string, opts core.LogsOptions, w io.Writer) error

	// Stats samples resource usage of every running service.
	Stats(ctx context.Context) (map[string]core.Stats, error)
	// Containers lists every Docker container on the server.
	Containers(ctx context.Context) ([]core.Container, error)
	// ContainerAction starts, stops or restarts any container.
	ContainerAction(ctx context.Context, name, action string) error
	// ContainerLogs writes a container's log lines to w.
	ContainerLogs(ctx context.Context, name string, opts core.LogsOptions, w io.Writer) error
	// StatsHistory returns the recent samples per service.
	StatsHistory(ctx context.Context) (map[string][]Sample, error)

	StartAction(ctx context.Context, app, action string) (*Job, error)
	StartDeploy(ctx context.Context, req core.DeployRequest) (*Job, error)
	// ListJobs lists recent jobs, newest first; app filters when not empty.
	ListJobs(ctx context.Context, app string) ([]Job, error)
	Job(ctx context.Context, id string) (*Job, error)
	CancelJob(ctx context.Context, id string) error
	// JobEvents replays and follows a job: emit(kind, data) per entry.
	JobEvents(ctx context.Context, id string, emit func(kind, data string) error) error
}

// =========================
// LOCAL
// =========================

// Local serves this machine through core.
type Local struct {
	Svc     *core.Service
	Jobs    *Jobs
	History *StatsHistory // nil: no history kept
}

func (l Local) StartAction(_ context.Context, app, action string) (*Job, error) {
	return l.Jobs.Start(app, action)
}

func (l Local) Containers(ctx context.Context) ([]core.Container, error) {
	return l.Svc.Containers(ctx)
}

func (l Local) ContainerAction(ctx context.Context, name, action string) error {
	if !core.ContainerActions[action] {
		return fmt.Errorf("%w: %s", ErrUnknownAction, action)
	}
	return l.Svc.ContainerAction(ctx, name, action, io.Discard)
}

func (l Local) ContainerLogs(ctx context.Context, name string, opts core.LogsOptions, w io.Writer) error {
	return l.Svc.ContainerLogs(ctx, name, opts, w)
}

func (l Local) StartDeploy(_ context.Context, req core.DeployRequest) (*Job, error) {
	return l.Jobs.StartDeploy(req)
}

func (l Local) ListJobs(_ context.Context, app string) ([]Job, error) { return l.Jobs.List(app), nil }

func (l Local) Stats(ctx context.Context) (map[string]core.Stats, error) {
	if l.History == nil {
		return l.Svc.Stats(ctx)
	}
	return l.History.Sample(ctx, l.Svc.Stats)
}

func (l Local) StatsHistory(context.Context) (map[string][]Sample, error) {
	if l.History == nil {
		return map[string][]Sample{}, nil
	}
	return l.History.History(), nil
}

func (l Local) Job(_ context.Context, id string) (*Job, error) { return l.Jobs.Get(id) }

func (l Local) CancelJob(_ context.Context, id string) error { return l.Jobs.Cancel(id) }

func (l Local) JobEvents(ctx context.Context, id string, emit func(kind, data string) error) error {
	return l.Jobs.Stream(ctx, id, emit)
}

func (l Local) Info(context.Context) (*Info, error) {
	h, _ := os.Hostname()
	return &Info{Hostname: h, Version: version.Version, OS: runtime.GOOS, Arch: runtime.GOARCH, API: APILevel}, nil
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
	URL   string // e.g. http://pi.local:7878; ignored when Routes is set
	Token string
	HTTP  *http.Client
	// Routes, when set, are tried in order with automatic failover.
	Routes *Routes
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
	return &http.Client{Timeout: 30 * time.Second, Transport: transport}
}

// streamClient has no overall timeout: streams live as long as ctx.
func (r Remote) streamClient() *http.Client {
	if r.HTTP != nil {
		return &http.Client{Transport: r.HTTP.Transport}
	}
	return &http.Client{Transport: transport}
}

func (r Remote) routes() *Routes {
	if r.Routes != nil {
		return r.Routes
	}
	return NewRoutes(Route{Name: "direct", URL: r.URL})
}

func (r Remote) do(ctx context.Context, client *http.Client, path string, q url.Values) (*http.Response, error) {
	return r.send(ctx, client, http.MethodGet, path, q)
}

func (r Remote) send(ctx context.Context, client *http.Client, method, path string, q url.Values) (*http.Response, error) {
	return r.sendBody(ctx, client, method, path, q, nil)
}

func (r Remote) sendBody(ctx context.Context, client *http.Client, method, path string, q url.Values, body []byte) (*http.Response, error) {
	rs := r.routes()
	var errs []error
	var resp *http.Response
	for _, i := range rs.order() {
		rt := rs.list[i]
		base, err := rt.base(ctx)
		if err != nil {
			rs.failed(i, err)
			errs = append(errs, fmt.Errorf("%s: %w", rt.Name, err))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		u := strings.TrimRight(base, "/") + "/api/v1/servers/" + LocalServer + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return nil, err
		}
		for k, v := range rt.Header {
			req.Header[k] = v
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+r.Token)
		if method != http.MethodGet {
			req.Header.Set(csrfHeader, "1")
		}
		resp, err = client.Do(req)
		if err == nil {
			rs.ok(i)
			break
		}
		rs.failed(i, err)
		errs = append(errs, fmt.Errorf("%s: %w", rt.Name, err))
		if ctx.Err() != nil || !retryable(method, err, true) {
			break
		}
	}
	if resp == nil {
		return nil, fmt.Errorf("server unreachable: %w", errors.Join(errs...))
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		var e struct{ Error string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		switch {
		case resp.StatusCode == http.StatusNotFound && strings.Contains(e.Error, core.ErrNotFound.Error()):
			return nil, fmt.Errorf("%w: %s", core.ErrNotFound, path)
		case resp.StatusCode == http.StatusNotFound && strings.Contains(e.Error, core.ErrContainerNotFound.Error()):
			return nil, fmt.Errorf("%w: %s", core.ErrContainerNotFound, path)
		case resp.StatusCode == http.StatusNotFound && strings.Contains(e.Error, ErrJobNotFound.Error()):
			return nil, fmt.Errorf("%w: %s", ErrJobNotFound, path)
		case resp.StatusCode == http.StatusConflict:
			return nil, fmt.Errorf("%w: %s", ErrBusy, e.Error)
		case resp.StatusCode == http.StatusBadRequest && strings.Contains(e.Error, ErrUnknownAction.Error()):
			return nil, fmt.Errorf("%w: %s", ErrUnknownAction, e.Error)
		case resp.StatusCode == http.StatusBadRequest && strings.Contains(e.Error, ErrInvalidRequest.Error()):
			return nil, fmt.Errorf("%w: %s", ErrInvalidRequest, strings.TrimPrefix(e.Error, ErrInvalidRequest.Error()+": "))
		case resp.StatusCode == http.StatusNotFound && (method != http.MethodGet || path == "/stats" || path == "/stats/history" || path == "/jobs" || path == "/containers"):
			// An older shipyard serve without this endpoint.
			return nil, &RemoteError{Status: resp.StatusCode, Message: "the server's shipyard is too old for this action; update it"}
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
	return r.relayLogs(ctx, "/apps/"+url.PathEscape(app)+"/logs", opts, w)
}

func (r Remote) ContainerLogs(ctx context.Context, name string, opts core.LogsOptions, w io.Writer) error {
	return r.relayLogs(ctx, "/containers/"+url.PathEscape(name)+"/logs", opts, w)
}

func (r Remote) Containers(ctx context.Context) ([]core.Container, error) {
	var out []core.Container
	return out, r.getJSON(ctx, "/containers", &out)
}

func (r Remote) ContainerAction(ctx context.Context, name, action string) error {
	resp, err := r.send(ctx, r.client(), http.MethodPost, "/containers/"+url.PathEscape(name)+"/actions/"+url.PathEscape(action), nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (r Remote) relayLogs(ctx context.Context, path string, opts core.LogsOptions, w io.Writer) error {
	q := url.Values{"tail": {strconv.Itoa(opts.Tail)}}
	if opts.Follow {
		q.Set("follow", "1")
	}
	resp, err := r.do(ctx, r.streamClient(), path, q)
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

func (r Remote) StartAction(ctx context.Context, app, action string) (*Job, error) {
	resp, err := r.send(ctx, r.client(), http.MethodPost, "/apps/"+url.PathEscape(app)+"/actions/"+url.PathEscape(action), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var j Job
	return &j, json.NewDecoder(resp.Body).Decode(&j)
}

func (r Remote) StartDeploy(ctx context.Context, req core.DeployRequest) (*Job, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	// Deploys answer once the job is registered, so the normal timeout holds.
	resp, err := r.sendBody(ctx, r.client(), http.MethodPost, "/deploy", nil, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var j Job
	return &j, json.NewDecoder(resp.Body).Decode(&j)
}

func (r Remote) ListJobs(ctx context.Context, app string) ([]Job, error) {
	var q url.Values
	if app != "" {
		q = url.Values{"app": {app}}
	}
	resp, err := r.do(ctx, r.client(), "/jobs", q)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out []Job
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (r Remote) Stats(ctx context.Context) (map[string]core.Stats, error) {
	out := map[string]core.Stats{}
	return out, r.getJSON(ctx, "/stats", &out)
}

func (r Remote) StatsHistory(ctx context.Context) (map[string][]Sample, error) {
	out := map[string][]Sample{}
	return out, r.getJSON(ctx, "/stats/history", &out)
}

func (r Remote) Job(ctx context.Context, id string) (*Job, error) {
	var j Job
	return &j, r.getJSON(ctx, "/jobs/"+url.PathEscape(id), &j)
}

func (r Remote) CancelJob(ctx context.Context, id string) error {
	resp, err := r.send(ctx, r.client(), http.MethodPost, "/jobs/"+url.PathEscape(id)+"/cancel", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// JobEvents relays the remote job stream.
func (r Remote) JobEvents(ctx context.Context, id string, emit func(kind, data string) error) error {
	resp, err := r.do(ctx, r.streamClient(), "/jobs/"+url.PathEscape(id)+"/events", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
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
		case strings.HasPrefix(line, "data: ") && event != "":
			d := strings.TrimPrefix(line, "data: ")
			if event == "error" {
				return errors.New(d)
			}
			if err := emit(event, d); err != nil {
				return err
			}
			if event == KindDone {
				return nil
			}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("job stream ended unexpectedly")
}
