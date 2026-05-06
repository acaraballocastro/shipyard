package docker

import (
	"fmt"

	"shipyard/internal/system"
)

func Build(image string, tag string, path string) error {
	full := fmt.Sprintf("%s:%s", image, tag)

	return system.RunCommand("docker", "build", "-t", full, path)
}

func Push(image string, tag string) error {
	full := fmt.Sprintf("%s:%s", image, tag)

	return system.RunCommand("docker", "push", full)
}

func Login(user string, token string) error {
	cmd := fmt.Sprintf("echo %s | docker login ghcr.io -u %s --password-stdin", token, user)

	return system.RunCommand("sh", "-c", cmd)
}
