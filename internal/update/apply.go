package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"shipyard/internal/compose"
	"shipyard/internal/deployment"
	"shipyard/internal/docker"
	"shipyard/internal/envfile"
	"shipyard/internal/events"
	"shipyard/internal/git"
)

// Runner holds the side-effecting operations of a deployment so that the
// workflow can be tested without Docker or Git.
type Runner struct {
	Clone     func(ctx context.Context, repo string, out io.Writer) (string, error)
	Build     func(ctx context.Context, image, dir string, opts docker.BuildOptions, out io.Writer) error
	ComposeUp func(ctx context.Context, dir string, out io.Writer) error
	WaitReady func(name string, timeout time.Duration) (docker.ContainerState, error)
}

// DefaultRunner uses the real git and docker implementations.
var DefaultRunner = Runner{
	Clone:     git.Clone,
	Build:     docker.BuildAndPushWith,
	ComposeUp: docker.ComposeUp,
	WaitReady: docker.WaitReady,
}

// ApplyOptions controls a single Apply call.
type ApplyOptions struct {
	// Build clones and rebuilds git sources. Image sources are always
	// pulled by ComposeUp.
	Build bool
	// SetEnv merges variables into the deployment-local .env file. Existing
	// variables not listed here are preserved.
	SetEnv map[string]string
	// UnsetEnv removes variables from the .env file (applied before SetEnv).
	UnsetEnv []string
}

// Apply validates cfg, builds the image when needed, writes the
// deployment files, starts the service and verifies it.
//
// Files are only replaced after a successful build. If starting or
// verifying the service fails, the previous docker-compose.yml, .env and
// deployment.yaml are restored and the previous configuration is started
// again (best effort). Volumes are never removed.
func (r Runner) Apply(ctx context.Context, dir string, cfg *deployment.Config, opts ApplyOptions, p events.Progress) (docker.ContainerState, error) {
	var none docker.ContainerState

	// =========================
	// VALIDATE
	// =========================

	cfg.Normalize()
	if err := cfg.Validate(true); err != nil {
		return none, fmt.Errorf("invalid configuration: %w", err)
	}
	for k, v := range opts.SetEnv {
		if _, _, err := envfile.ParseAssignment(k + "=" + v); err != nil {
			return none, err
		}
	}

	// =========================
	// BUILD
	// =========================

	if cfg.Source.Type == deployment.SourceGit && opts.Build {
		p.Step("clone", "cloning %s", cfg.Source.Repository)
		src, err := r.Clone(ctx, cfg.Source.Repository, p.Writer())
		if err != nil {
			return none, fmt.Errorf("git clone: %w", err)
		}

		p.Step("build", "building %s image", cfg.Build.Platform)
		if names := buildArgNames(cfg.Build); len(names) > 0 {
			// names only — values are never logged
			p.Detail("build args", fmt.Sprint(names))
		}
		err = r.Build(ctx, cfg.Image, src, docker.BuildOptions{
			Platform:    cfg.Build.Platform,
			Args:        cfg.Build.Args,
			ArgsFromEnv: cfg.Build.ArgsFromEnv,
		}, p.Writer())
		if err != nil {
			return none, fmt.Errorf("build failed, existing deployment left untouched: %w", err)
		}
	}

	// =========================
	// WRITE FILES (with backup)
	// =========================

	if err := os.MkdirAll(dir, 0755); err != nil {
		return none, err
	}

	backup, err := snapshot(dir)
	if err != nil {
		return none, fmt.Errorf("backing up current deployment: %w", err)
	}

	if err := writeFiles(dir, cfg, opts); err != nil {
		_ = backup.restore()
		return none, err
	}

	// =========================
	// START + VERIFY
	// =========================

	fail := func(stage string, cause error) (docker.ContainerState, error) {
		if !backup.changed() {
			return none, fmt.Errorf("%s: %w", stage, cause)
		}
		p.Warn("%s failed, restoring previous configuration", stage)
		if rerr := backup.restore(); rerr != nil {
			return none, errors.Join(fmt.Errorf("%s: %w", stage, cause), fmt.Errorf("restore: %w", rerr))
		}
		if backup.hadCompose() {
			if uerr := r.ComposeUp(ctx, dir, p.Writer()); uerr != nil {
				return none, errors.Join(fmt.Errorf("%s: %w", stage, cause), fmt.Errorf("restarting previous configuration: %w", uerr))
			}
		}
		return none, fmt.Errorf("%s (previous configuration restored): %w", stage, cause)
	}

	p.Step("up", "starting %s", cfg.Name)
	if err := r.ComposeUp(ctx, dir, p.Writer()); err != nil {
		return fail("compose up", err)
	}

	p.Step("verify", "waiting for %s", cfg.Name)
	st, err := r.WaitReady(cfg.Name, readyTimeout(cfg.Healthcheck))
	if err != nil {
		return fail("verify", err)
	}
	return st, nil
}

func buildArgNames(b deployment.Build) []string {
	names := make([]string, 0, len(b.Args)+len(b.ArgsFromEnv))
	for k := range b.Args {
		names = append(names, k)
	}
	names = append(names, b.ArgsFromEnv...)
	sort.Strings(names)
	return names
}

// writeFiles writes .env (if needed), docker-compose.yml and deployment.yaml.
func writeFiles(dir string, cfg *deployment.Config, opts ApplyOptions) error {
	localEnv := filepath.Join(dir, deployment.LocalEnvFileName)

	vars := map[string]string{}
	if _, err := os.Stat(localEnv); err == nil {
		existing, err := envfile.Read(localEnv)
		if err != nil {
			return fmt.Errorf("reading %s: %w", localEnv, err)
		}
		vars = existing
	}
	for _, k := range opts.UnsetEnv {
		delete(vars, k)
	}
	for k, v := range opts.SetEnv {
		vars[k] = v
	}
	if len(opts.SetEnv) > 0 || len(opts.UnsetEnv) > 0 {
		if err := envfile.Write(localEnv, vars); err != nil {
			return fmt.Errorf("writing %s: %w", localEnv, err)
		}
	} else if len(vars) > 0 {
		// Enforce restrictive permissions even on a hand-edited file.
		if err := os.Chmod(localEnv, 0600); err != nil {
			return err
		}
	}

	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cfg.Env.Keys = keys

	data, err := compose.Marshal(cfg.ComposeSpec(dir))
	if err != nil {
		return fmt.Errorf("compose: %w", err)
	}
	if err := deployment.WriteFileAtomic(filepath.Join(dir, deployment.ComposeFileName), data, 0644); err != nil {
		return fmt.Errorf("writing compose file: %w", err)
	}
	if err := deployment.Save(dir, cfg); err != nil {
		return fmt.Errorf("writing %s: %w", deployment.FileName, err)
	}
	return nil
}

// readyTimeout estimates how long a container may take to become healthy.
func readyTimeout(h deployment.Healthcheck) time.Duration {
	const base = 60 * time.Second
	if !h.Enabled {
		return base
	}
	parse := func(s string, def time.Duration) time.Duration {
		if d, err := time.ParseDuration(s); err == nil && s != "" {
			return d
		}
		return def
	}
	retries := h.Retries
	if retries == 0 {
		retries = 3 // Docker default
	}
	interval := parse(h.Interval, 30*time.Second)
	timeout := parse(h.Timeout, 30*time.Second)
	t := parse(h.StartPeriod, 0) + time.Duration(retries+1)*(interval+timeout) + 15*time.Second
	if t < base {
		return base
	}
	if t > 15*time.Minute {
		return 15 * time.Minute
	}
	return t
}

// =========================
// BACKUP
// =========================

var managedFiles = []string{
	deployment.ComposeFileName,
	deployment.FileName,
	deployment.LocalEnvFileName,
}

type fileSnapshot struct {
	data   []byte
	mode   os.FileMode
	exists bool
}

type dirSnapshot struct {
	dir   string
	files map[string]fileSnapshot
}

func snapshot(dir string) (*dirSnapshot, error) {
	s := &dirSnapshot{dir: dir, files: map[string]fileSnapshot{}}
	for _, name := range managedFiles {
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p)
		if errors.Is(err, os.ErrNotExist) {
			s.files[name] = fileSnapshot{}
			continue
		}
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		s.files[name] = fileSnapshot{data: data, mode: fi.Mode().Perm(), exists: true}
	}
	return s, nil
}

func (s *dirSnapshot) hadCompose() bool {
	return s.files[deployment.ComposeFileName].exists
}

// changed reports whether any managed file differs from the snapshot.
func (s *dirSnapshot) changed() bool {
	for name, f := range s.files {
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		exists := err == nil
		if exists != f.exists || (exists && !bytes.Equal(data, f.data)) {
			return true
		}
	}
	return false
}

func (s *dirSnapshot) restore() error {
	var errs []error
	for name, f := range s.files {
		p := filepath.Join(s.dir, name)
		if !f.exists {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		if err := deployment.WriteFileAtomic(p, f.data, f.mode); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
