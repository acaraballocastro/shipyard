package update

import (
	"fmt"

	"shipyard/internal/docker"
	"shipyard/internal/git"
	"shipyard/internal/state"
)

func Run(appName string) error {

	// =========================
	// LOAD STATE
	// =========================

	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("error cargando state: %w", err)
	}

	app, exists := st.Apps[appName]
	if !exists {
		return fmt.Errorf("app no encontrada: %s", appName)
	}

	// =========================
	// CLONE REPO
	// =========================

	fmt.Println("📥 Clonando repo...")

	dir, err := git.Clone(app.Repo)
	if err != nil {
		return fmt.Errorf("error clonando repo: %w", err)
	}

	// =========================
	// BUILD + PUSH
	// =========================

	fmt.Println("🐳 Build y push imagen...")

	err = docker.BuildAndPush(app.Image, dir)
	if err != nil {
		return fmt.Errorf("error build/push: %w", err)
	}

	// =========================
	// REDEPLOY
	// =========================

	fmt.Println("🚀 Redeploy compose...")

	err = docker.ComposeUp(app.Path)
	if err != nil {
		return fmt.Errorf("error compose up: %w", err)
	}

	return nil
}
