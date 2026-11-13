// Package remote runs shipyard on another host over the system ssh client,
// so ~/.ssh/config, keys and agents work as they do in a terminal.
package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Bin is the shipyard executable on the remote host.
const Bin = "shipyard"

// Options controls a remote invocation.
type Options struct {
	// TTY allocates a terminal (needed for prompts, logs -f and shell).
	TTY bool
	// Stdin is sent to the remote command. Incompatible with TTY.
	Stdin io.Reader
}

// Command builds the remote shell command line: every argument quoted.
func Command(args ...string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = Quote(a)
	}
	return strings.Join(q, " ")
}

func sshArgs(host string, tty bool, remoteCmd string) []string {
	args := []string{"-o", "ConnectTimeout=15"}
	if tty {
		args = append(args, "-t")
	} else {
		args = append(args, "-T")
	}
	return append(args, host, "--", remoteCmd)
}

// Run executes args on host, streaming output to the local terminal.
func Run(host string, opts Options, args ...string) error {
	c := exec.Command("ssh", sshArgs(host, opts.TTY, Command(args...))...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if opts.Stdin != nil {
		c.Stdin = opts.Stdin
	} else {
		c.Stdin = os.Stdin
	}
	return c.Run()
}

// Output executes args on host and returns stdout. Never prompts.
func Output(host string, args ...string) ([]byte, error) {
	return OutputWithInput(host, nil, args...)
}

// OutputWithInput is Output with data sent on stdin.
func OutputWithInput(host string, stdin io.Reader, args ...string) ([]byte, error) {
	sa := append([]string{"-o", "BatchMode=yes"}, sshArgs(host, false, Command(args...))...)
	c := exec.Command("ssh", sa...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	c.Stdin = stdin
	out, err := c.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return out, fmt.Errorf("%s: %w", msg, err)
		}
		return out, err
	}
	return out, nil
}

// ExitCode returns the remote exit status of an error from Run, or 1.
func ExitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() > 0 {
		return ee.ExitCode()
	}
	return 1
}

// Quote quotes s for a POSIX shell.
func Quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'$`\\|&;<>()*?![]{}#~=%,") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Stream executes args on host without a terminal (so it never prompts),
// writing stdout and stderr to w, until done or ctx is cancelled.
func Stream(ctx context.Context, host string, w io.Writer, args ...string) error {
	sa := append([]string{"-o", "BatchMode=yes"}, sshArgs(host, false, Command(args...))...)
	c := exec.CommandContext(ctx, "ssh", sa...)
	c.Stdout, c.Stderr = w, w
	return c.Run()
}
