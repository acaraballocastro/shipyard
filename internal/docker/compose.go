package docker

import (
	"os"
	"os/exec"
)

func ComposeUp(path string) error {

	// =========================
	// PULL LATEST IMAGE
	// =========================

	pull := exec.Command("docker", "compose", "pull")
	pull.Dir = path
	pull.Stdout = os.Stdout
	pull.Stderr = os.Stderr

	if err := pull.Run(); err != nil {
		return err
	}

	// =========================
	// RECREATE CONTAINERS
	// =========================

	up := exec.Command("docker", "compose", "up", "-d", "--remove-orphans")
	up.Dir = path
	up.Stdout = os.Stdout
	up.Stderr = os.Stderr

	return up.Run()
}

func ComposeDown(path string) error {
	cmd := exec.Command("docker", "compose", "down")
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
