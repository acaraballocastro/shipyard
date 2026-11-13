package docker

import (
	"context"
	"io"
	"os/exec"

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

// ComposeDownProject stops a compose project by name, for when its
// directory (and compose file) no longer exists. Compose names projects
// after their directory, so this is filepath.Base of the service path.
func ComposeDownProject(ctx context.Context, project string, out io.Writer) error {
	return system.Run(ctx, out, "docker", "compose", "-p", project, "down", "--remove-orphans")
}

// RemoveContainer force-removes a container; a missing one is not an error.
func RemoveContainer(ctx context.Context, name string, out io.Writer) error {
	if exec.CommandContext(ctx, "docker", "inspect", "--type", "container", "--", name).Run() != nil {
		return nil
	}
	return system.Run(ctx, out, "docker", "rm", "-f", "--", name)
}
