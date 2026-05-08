package docker

import "shipyard/internal/system"

func NetworkExists(name string) bool {
	err := system.RunCommand("docker", "network", "inspect", name)
	return err == nil
}

func CreateNetwork(name string) error {
	return system.RunCommand("docker", "network", "create", name)
}
