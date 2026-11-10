package system

import (
	"context"
	"io"
	"os"
	"os/exec"
)

func RunCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Run executes a command, sending stdout and stderr to out, and kills it
// when ctx is cancelled.
func Run(ctx context.Context, out io.Writer, name string, args ...string) error {
	return RunIn(ctx, "", out, name, args...)
}

// RunIn is Run with a working directory.
func RunIn(ctx context.Context, dir string, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}
