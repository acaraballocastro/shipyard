package state

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// State is the top-level structure persisted to ~/.shipyard/state.yaml.
type State struct {
	Apps map[string]App `yaml:"apps"`
}

// App holds the runtime record of a deployed service.
// Every field here must be sufficient to reproduce or manage the deployment
// without the user having to remember flags.
type App struct {
	// Source
	Repo  string `yaml:"repo,omitempty"` // git slug, e.g. "user/my-app"
	Image string `yaml:"image"`          // full image reference

	// Providers used at deploy time — needed for update/rebuild
	GitProvider string `yaml:"git_provider,omitempty"` // name from config, e.g. "github"
	Registry    string `yaml:"registry,omitempty"`     // name from config, e.g. "ghcr"

	// Compose / networking
	Mode    string `yaml:"mode"`              // "local" | "traefik"
	Domain  string `yaml:"domain,omitempty"`  // traefik domain
	Network string `yaml:"network,omitempty"` // docker network name
	Path    string `yaml:"path"`              // absolute path to compose dir

	// Watch support
	LastCommit string `yaml:"last_commit,omitempty"` // SHA of last deployed commit
}

// ---------------------------------------------------------------------------
// Load / Save
// ---------------------------------------------------------------------------

func statePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".shipyard", "state.yaml")
}

// Load reads the state file. Returns an empty State if the file does not exist.
func Load() (*State, error) {
	p := statePath()

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Apps: map[string]App{}}, nil
		}
		return nil, fmt.Errorf("reading state: %w", err)
	}

	var st State
	if err := yaml.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}

	// Guard against a nil map from an empty file.
	if st.Apps == nil {
		st.Apps = map[string]App{}
	}

	return &st, nil
}

// Save writes state to disk atomically (write to tmp, then rename).
func Save(st *State) error {
	p := statePath()

	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}

	data, err := yaml.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshalling state: %w", err)
	}

	// Write to a temp file first to avoid corruption on crash.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}

	return os.Rename(tmp, p)
}
