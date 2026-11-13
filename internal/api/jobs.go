package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"shipyard/internal/core"
	"shipyard/internal/events"
)

// JobStatus is the lifecycle state of a job.
type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
	JobCanceled  JobStatus = "canceled"
)

// Job is a long-running action (deploy, restart, update, delete…) on a service.
// It runs independently of the request that started it.
type Job struct {
	ID         string     `json:"id"`
	App        string     `json:"app"`
	Action     string     `json:"action"`
	Status     JobStatus  `json:"status"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Job stream entry kinds (SSE event names).
const (
	KindProgress = "progress" // data: events.Event JSON
	KindOutput   = "output"   // data: one line of docker/git output
	KindDone     = "done"     // data: Job JSON (final)
)

// ErrBusy means the service already has a running job.
var ErrBusy = errors.New("another action is already running for this service")

// ErrUnknownAction is returned for actions the server does not support.
var ErrUnknownAction = errors.New("unknown action")

// ErrInvalidRequest is a malformed or incomplete request.
var ErrInvalidRequest = errors.New("invalid request")

// ErrJobNotFound is returned for unknown job ids.
var ErrJobNotFound = errors.New("job not found")

// Actions lists what can be started on a service.
var Actions = map[string]func(s *core.Service, ctx context.Context, app string, p events.Progress) error{
	"restart": func(s *core.Service, ctx context.Context, app string, p events.Progress) error {
		return s.Restart(ctx, app, p)
	},
	"update": func(s *core.Service, ctx context.Context, app string, p events.Progress) error {
		return s.Update(ctx, app, p)
	},
	"delete": func(s *core.Service, ctx context.Context, app string, p events.Progress) error {
		return s.Delete(ctx, app, p)
	},
}

const (
	maxEntries  = 20000 // per job; older output is dropped
	maxFinished = 50    // finished jobs kept for inspection
)

type entry struct{ kind, data string }

type jobState struct {
	mu      sync.Mutex
	job     Job
	entries []entry
	dropped int
	notify  chan struct{} // closed and replaced on every append
	cancel  context.CancelFunc
}

// Jobs runs and tracks actions for the local machine.
type Jobs struct {
	Svc *core.Service
	// Dir, when set, keeps finished jobs (with their output) across restarts.
	Dir string
	// Prefix starts every job id, so ids from different managers never clash.
	Prefix string

	mu      sync.Mutex
	jobs    map[string]*jobState
	running map[string]string // app -> job id
	seq     int
}

// NewJobs returns an in-memory job manager for svc.
func NewJobs(svc *core.Service) *Jobs {
	return &Jobs{Svc: svc, jobs: map[string]*jobState{}, running: map[string]string{}}
}

// OpenJobs returns a job manager that persists jobs under dir and loads
// the ones saved by a previous run. A job that was running when the
// process stopped is reported as failed ("interrupted").
func OpenJobs(svc *core.Service, dir, prefix string) (*Jobs, error) {
	j := NewJobs(svc)
	j.Dir, j.Prefix = dir, prefix
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	loaded, err := loadJobs(dir)
	for _, st := range loaded {
		if st.job.Status == JobRunning {
			now := time.Now()
			st.job.Status, st.job.Error, st.job.FinishedAt = JobFailed, ErrInterrupted.Error(), &now
			b, _ := json.Marshal(st.job)
			st.entries = append(st.entries, entry{KindDone, string(b)})
			j.persist(st)
		}
		j.jobs[st.job.ID] = st
	}
	j.gcLocked()
	return j, err
}

// ErrInterrupted marks jobs cut short by a restart of the server.
var ErrInterrupted = errors.New("interrupted: shipyard restarted while this was running")

// Start launches action on an existing app and returns immediately.
func (j *Jobs) Start(app, action string) (*Job, error) {
	run, ok := Actions[action]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAction, action)
	}
	apps, err := j.Svc.List()
	if err != nil {
		return nil, err
	}
	if !containsApp(apps, app) {
		return nil, fmt.Errorf("%w: %s", core.ErrNotFound, app)
	}
	return j.Launch(app, action, func(ctx context.Context, p events.Progress) error {
		return run(j.Svc, ctx, app, p)
	})
}

// StartDeploy creates or redeploys a service in the background.
func (j *Jobs) StartDeploy(req core.DeployRequest) (*Job, error) {
	if req.Repo == "" && req.Image == "" {
		return nil, fmt.Errorf("%w: specify a repository or an image", ErrInvalidRequest)
	}
	name := core.DeployName(req)
	if name == "" {
		return nil, fmt.Errorf("%w: cannot derive a service name, set one", ErrInvalidRequest)
	}
	return j.Launch(name, "deploy", func(ctx context.Context, p events.Progress) error {
		_, err := j.Svc.Deploy(ctx, req, p)
		return err
	})
}

// StartPrune cleans Docker disk space in the background.
func (j *Jobs) StartPrune(all bool) (*Job, error) {
	return j.Launch("docker", "prune", func(ctx context.Context, p events.Progress) error {
		return j.Svc.Prune(ctx, all, p)
	})
}

// StartAdopt turns an existing container into a shipyard service.
func (j *Jobs) StartAdopt(container string, req core.DeployRequest) (*Job, error) {
	if req.Image == "" {
		return nil, fmt.Errorf("%w: adoption needs the container image", ErrInvalidRequest)
	}
	name := core.DeployName(req)
	return j.Launch(name, "adopt", func(ctx context.Context, p events.Progress) error {
		return j.Svc.Adopt(ctx, container, req, p)
	})
}

// Launch registers a job under key (one running job per key) and runs fn
// in the background. Start and StartDeploy use it with the service name.
func (j *Jobs) Launch(app, action string, fn func(context.Context, events.Progress) error) (*Job, error) {
	j.mu.Lock()
	if id, busy := j.running[app]; busy {
		j.mu.Unlock()
		return nil, fmt.Errorf("%w (job %s)", ErrBusy, id)
	}
	j.seq++
	ctx, cancel := context.WithCancel(context.Background())
	st := &jobState{
		job: Job{
			ID:        fmt.Sprintf("%s%d-%d-%s", j.Prefix, time.Now().Unix(), j.seq, randSuffix()),
			App:       app,
			Action:    action,
			Status:    JobRunning,
			StartedAt: time.Now(),
		},
		notify: make(chan struct{}),
		cancel: cancel,
	}
	j.jobs[st.job.ID] = st
	j.running[app] = st.job.ID
	j.gcLocked()
	j.mu.Unlock()
	j.persist(st) // so a restart mid-job shows it as interrupted

	go func() {
		defer cancel()
		out := &lineWriter{emit: func(line string) { st.append(KindOutput, line) }}
		p := events.Progress{
			Emit: func(e events.Event) {
				b, _ := json.Marshal(e)
				st.append(KindProgress, string(b))
			},
			Out: out,
		}
		err := fn(ctx, p)
		out.Flush()

		j.mu.Lock()
		delete(j.running, app)
		j.mu.Unlock()
		st.finish(ctx, err)
		j.persist(st)
	}()

	job := st.snapshot()
	return &job, nil
}

// List returns the known jobs, newest first; app filters when not empty.
func (j *Jobs) List(app string) []Job {
	j.mu.Lock()
	states := make([]*jobState, 0, len(j.jobs))
	for _, st := range j.jobs {
		states = append(states, st)
	}
	j.mu.Unlock()
	out := []Job{}
	for _, st := range states {
		if job := st.snapshot(); app == "" || job.App == app {
			out = append(out, job)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt.After(out[b].StartedAt) })
	return out
}

// Get returns a job by id.
func (j *Jobs) Get(id string) (*Job, error) {
	st, err := j.state(id)
	if err != nil {
		return nil, err
	}
	job := st.snapshot()
	return &job, nil
}

// Cancel stops a running job (best effort: running commands are killed).
func (j *Jobs) Cancel(id string) error {
	st, err := j.state(id)
	if err != nil {
		return err
	}
	st.cancel()
	return nil
}

// Stream calls emit for every entry of the job, from the beginning, and
// for new ones as they arrive, until the job is done or ctx ends.
func (j *Jobs) Stream(ctx context.Context, id string, emit func(kind, data string) error) error {
	st, err := j.state(id)
	if err != nil {
		return err
	}
	next := 0
	for {
		st.mu.Lock()
		if next < st.dropped {
			next = st.dropped
		}
		pending := append([]entry(nil), st.entries[next-st.dropped:]...)
		next += len(pending)
		notify := st.notify
		st.mu.Unlock()

		for _, e := range pending {
			if err := emit(e.kind, e.data); err != nil {
				return err
			}
			if e.kind == KindDone {
				return nil
			}
		}
		select {
		case <-notify:
		case <-ctx.Done():
			return nil
		}
	}
}

func (j *Jobs) state(id string) (*jobState, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	st, ok := j.jobs[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return st, nil
}

// gcLocked forgets the oldest finished jobs beyond maxFinished.
func (j *Jobs) gcLocked() {
	var finished []*jobState
	for _, st := range j.jobs {
		st.mu.Lock()
		done := st.job.Status != JobRunning
		st.mu.Unlock()
		if done {
			finished = append(finished, st)
		}
	}
	if len(finished) <= maxFinished {
		return
	}
	sort.Slice(finished, func(a, b int) bool { return finished[a].job.StartedAt.Before(finished[b].job.StartedAt) })
	for _, st := range finished[:len(finished)-maxFinished] {
		delete(j.jobs, st.job.ID)
		if j.Dir != "" {
			_ = os.Remove(filepath.Join(j.Dir, st.job.ID+".json"))
		}
	}
}

// =========================
// PERSISTENCE
// =========================

const maxSavedEntries = 5000 // output lines kept on disk per job

type savedJob struct {
	Job     Job         `json:"job"`
	Entries [][2]string `json:"entries"` // [kind, data]
}

// persist writes a job atomically; failures only cost history.
func (j *Jobs) persist(st *jobState) {
	if j.Dir == "" {
		return
	}
	st.mu.Lock()
	sj := savedJob{Job: st.job}
	es := st.entries
	if len(es) > maxSavedEntries {
		es = es[len(es)-maxSavedEntries:]
	}
	for _, e := range es {
		sj.Entries = append(sj.Entries, [2]string{e.kind, e.data})
	}
	st.mu.Unlock()
	b, err := json.Marshal(sj)
	if err != nil {
		return
	}
	path := filepath.Join(j.Dir, sj.Job.ID+".json")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func loadJobs(dir string) ([]*jobState, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []*jobState
	var errs []error
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var sj savedJob
		if err := json.Unmarshal(b, &sj); err != nil || sj.Job.ID == "" {
			errs = append(errs, fmt.Errorf("%s: unreadable, skipped", filepath.Base(f)))
			continue
		}
		st := &jobState{job: sj.Job, notify: make(chan struct{}), cancel: func() {}}
		for _, e := range sj.Entries {
			st.entries = append(st.entries, entry{e[0], e[1]})
		}
		out = append(out, st)
	}
	return out, errors.Join(errs...)
}

func randSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (st *jobState) append(kind, data string) {
	st.mu.Lock()
	st.entries = append(st.entries, entry{kind, data})
	if over := len(st.entries) - maxEntries; over > 0 {
		st.entries = st.entries[over:]
		st.dropped += over
	}
	close(st.notify)
	st.notify = make(chan struct{})
	st.mu.Unlock()
}

func (st *jobState) finish(ctx context.Context, err error) {
	st.mu.Lock()
	now := time.Now()
	st.job.FinishedAt = &now
	switch {
	case err == nil:
		st.job.Status = JobSucceeded
	case ctx.Err() != nil:
		st.job.Status = JobCanceled
		st.job.Error = "canceled"
	default:
		st.job.Status = JobFailed
		st.job.Error = err.Error()
	}
	b, _ := json.Marshal(st.job)
	st.mu.Unlock()
	st.append(KindDone, string(b))
}

func (st *jobState) snapshot() Job {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.job
}

func containsApp(apps []core.App, name string) bool {
	for _, a := range apps {
		if a.Name == name {
			return true
		}
	}
	return false
}

// lineWriter turns a byte stream into lines.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		w.emit(strings.TrimRight(string(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(strings.TrimRight(string(w.buf), "\r"))
		w.buf = nil
	}
}
