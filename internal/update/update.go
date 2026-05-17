package update

import (
	"fmt"

	"shipyard/internal/docker"
	"shipyard/internal/git"
	"shipyard/internal/state"
	"shipyard/internal/ui"
)

func Run(appName string) error {

	// =========================
	// LOAD STATE
	// =========================

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	app, exists := st.Apps[appName]
	if !exists {
		return fmt.Errorf("app not found: %s", appName)
	}

	// =========================
	// CLONE REPO
	// =========================

	ui.Step("clone", "cloning %s", app.Repo)

	dir, err := git.Clone(app.Repo)
	if err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	// =========================
	// BUILD + PUSH
	// =========================

	ui.Step("build", "building and pushing image")

	if err := docker.BuildAndPush(app.Image, dir); err != nil {
		return fmt.Errorf("build: %w", err)
	}

	// =========================
	// REDEPLOY
	// =========================

	ui.Step("up", "redeploying %s", appName)

	if err := docker.ComposeUp(app.Path); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}

	return nil
}
