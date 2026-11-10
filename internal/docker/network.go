package docker

import (
	"context"
	"io"
	"os/exec"

	"shipyard/internal/system"
)

func NetworkExists(name string) bool {
	return exec.Command("docker", "network", "inspect", "--", name).Run() == nil
}

func CreateNetwork(ctx context.Context, name string, out io.Writer) error {
	return system.Run(ctx, out, "docker", "network", "create", name)
}
