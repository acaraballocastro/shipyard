package docker

import (
	"shipyard/internal/system"
)

func ComposeUp(path string) error {
	return system.RunCommand(
		"docker",
		"compose",
		"-f",
		path+"/docker-compose.yml",
		"up",
		"-d",
		"--force-recreate",
	)
}
