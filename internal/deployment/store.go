package deployment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"shipyard/internal/compose"
	"shipyard/internal/state"
)

// ErrNotFound is returned by Load when deployment.yaml does not exist.
var ErrNotFound = errors.New("deployment.yaml not found")

// Load reads and normalizes <dir>/deployment.yaml. Unknown fields are
// rejected to catch typos in hand-edited files.
func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return Unmarshal(data)
}

// Unmarshal parses deployment.yaml content.
func Unmarshal(data []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", FileName, err)
	}
	c.Normalize()
	return &c, nil
}

// Marshal renders the configuration as YAML.
func (c *Config) Marshal() ([]byte, error) {
	header := "# Managed by shipyard. Safe to edit; validated on next deploy/update.\n" +
		"# Never put secrets here: use env files (--env-file) or --env.\n"
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	return append([]byte(header), data...), nil
}

// Save validates and atomically writes <dir>/deployment.yaml.
func Save(dir string, c *Config) error {
	if err := c.Validate(false); err != nil {
		return err
	}
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	return WriteFileAtomic(filepath.Join(dir, FileName), data, 0644)
}

// WriteFileAtomic writes data to a temp file in the same directory and
// renames it over path, so readers never observe a partial file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// FromLegacy reconstructs a Config for a service deployed by an older
// shipyard that only recorded the state entry. It reads the existing
// compose file to recover ports, volumes and Traefik settings that were
// never stored in state.
func FromLegacy(name string, app state.App) (*Config, error) {
	c := &Config{
		Version: CurrentVersion,
		Name:    name,
		Image:   app.Image,
		Deploy: Deploy{
			Mode:    app.Mode,
			Domain:  app.Domain,
			Network: app.Network,
		},
	}
	if app.Repo != "" {
		c.Source = Source{Type: SourceGit, Repository: app.Repo}
		c.Build.Platform = DefaultPlatform // the only platform older versions built
	} else {
		c.Source = Source{Type: SourceImage}
	}

	file := filepath.Join(app.Path, ComposeFileName)
	cf, err := compose.Parse(file)
	if err != nil {
		return nil, fmt.Errorf("migrating %s: reading %s: %w", name, file, err)
	}
	svc, ok := cf.Services[name]
	if !ok {
		return nil, fmt.Errorf("migrating %s: service not found in %s", name, file)
	}
	if c.Image == "" {
		c.Image = svc.Image
	}
	c.Deploy.Ports = svc.Ports
	c.Deploy.Volumes = svc.Volumes
	c.Env.Files = svc.EnvFile
	if len(svc.Networks) > 0 && c.Deploy.Network == "" {
		c.Deploy.Network = svc.Networks[0]
	}

	for _, l := range svc.Labels {
		key, val, _ := strings.Cut(l, "=")
		switch {
		case strings.HasSuffix(key, ".loadbalancer.server.port"):
			c.Deploy.InternalPort = val
		case strings.HasSuffix(key, ".entrypoints") && strings.HasPrefix(key, "traefik.http.routers."):
			c.Deploy.Entrypoint = val
		case strings.HasSuffix(key, ".rule") && c.Deploy.Domain == "":
			if s, ok := strings.CutPrefix(val, "Host(`"); ok {
				c.Deploy.Domain = strings.TrimSuffix(s, "`)")
			}
		}
	}

	c.Normalize()
	return c, nil
}
