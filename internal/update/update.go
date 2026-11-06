package update

import (
	"errors"
	"fmt"

	"shipyard/internal/deployment"
	"shipyard/internal/state"
	"shipyard/internal/ui"
)

// Run updates a deployed service using its saved configuration.
func Run(appName string) error {
	return DefaultRunner.Update(appName)
}

// LoadConfig returns the deployment configuration of a service, migrating
// legacy deployments (state entry + compose file only) in memory.
// migrated is true when the config was reconstructed.
func LoadConfig(appName string, app state.App) (cfg *deployment.Config, migrated bool, err error) {
	cfg, err = deployment.Load(app.Path)
	if errors.Is(err, deployment.ErrNotFound) {
		cfg, err = deployment.FromLegacy(appName, app)
		return cfg, true, err
	}
	if err != nil {
		return nil, false, err
	}
	if cfg.Name != appName {
		return nil, false, fmt.Errorf("%s/%s describes %q, expected %q",
			app.Path, deployment.FileName, cfg.Name, appName)
	}
	return cfg, false, nil
}

// Update rebuilds (git) or re-pulls (image) a service and recreates it,
// reusing every setting from deployment.yaml.
func (r Runner) Update(appName string) error {

	// =========================
	// LOAD STATE + CONFIG
	// =========================

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	app, exists := st.Apps[appName]
	if !exists {
		return fmt.Errorf("app not found: %s", appName)
	}

	cfg, migrated, err := LoadConfig(appName, app)
	if err != nil {
		return err
	}
	if migrated {
		ui.Info("no %s found, migrating settings from state and compose file", deployment.FileName)
	}

	ui.Detail("source", cfg.Source.Type)
	ui.Detail("image", cfg.Image)

	// =========================
	// APPLY
	// =========================

	cs, err := r.Apply(app.Path, cfg, ApplyOptions{Build: true})
	if err != nil {
		return err
	}
	if cs.Health != "" {
		ui.Detail("health", cs.Health)
	}

	// =========================
	// STATE
	// =========================

	// Reload: watch and other commands may have written in the meantime.
	if st2, err := state.Load(); err == nil {
		st = st2
	}
	st.Apps[appName] = MergeState(st.Apps[appName], cfg, app.Path)
	return state.Save(st)
}

// MergeState refreshes the state index entry from cfg while keeping
// fields state owns (such as LastCommit).
func MergeState(prev state.App, cfg *deployment.Config, path string) state.App {
	prev.Repo = cfg.Source.Repository
	prev.Image = cfg.Image
	prev.Mode = cfg.Deploy.Mode
	prev.Domain = cfg.Deploy.Domain
	prev.Network = cfg.Deploy.Network
	prev.Path = path
	return prev
}
