package docker

import (
	"os"
	"os/exec"
)

func ComposeUp(path string) error {

	pull := exec.Command(
		"docker",
		"compose",
		"pull",
	)

	pull.Dir = path
	pull.Stdout = os.Stdout
	pull.Stderr = os.Stderr

	if err := pull.Run(); err != nil {
		return err
	}

	up := exec.Command(
		"docker",
		"compose",
		"up",
		"-d",
	)

	up.Dir = path
	up.Stdout = os.Stdout
	up.Stderr = os.Stderr

	return up.Run()
}
