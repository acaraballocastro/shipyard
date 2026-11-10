package docker

import (
	"context"
	"io"

	"shipyard/internal/system"
)

// ComposeUp pulls the latest images and recreates the containers.
func ComposeUp(ctx context.Context, path string, out io.Writer) error {
	if err := system.RunIn(ctx, path, out, "docker", "compose", "pull"); err != nil {
		return err
	}
	return system.RunIn(ctx, path, out, "docker", "compose", "up", "-d", "--remove-orphans")
}

func ComposeDown(ctx context.Context, path string, out io.Writer) error {
	return system.RunIn(ctx, path, out, "docker", "compose", "down")
}
