package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"shipyard/internal/remote"
	"shipyard/internal/ui"

	"github.com/spf13/cobra"
)

// envStdin is a hidden flag used when deploy is forwarded to a server:
// --env values arrive on stdin so they never show up in argv or ps.
var envStdin bool

// deployRemote forwards deploy to a server. The server clones, builds,
// pushes and runs the service exactly like a local deploy would. Local
// env files are uploaded first; --env values are sent on stdin.
func deployRemote(t target, cmd *cobra.Command) {
	host := t.Server.Host
	ui.Section("deploy")
	ui.Info("target %s (%s), building on the server", ui.Colorize("bold", t.Name), host)

	if _, err := ParseEnvVars(envVars); err != nil {
		ui.Fatal("%s", err)
	}

	flags := forwardedFlags(cmd.Flags(), "env", "env-file")

	if cmd.Flags().Changed("env-file") {
		paths, err := uploadEnvFiles(host, envFiles)
		if err != nil {
			ui.Fatal("%s", err)
		}
		for _, p := range paths {
			flags = append(flags, "--env-file="+p)
		}
	}

	opts := remote.Options{TTY: true}
	if len(envVars) > 0 {
		flags = append(flags, "--env-stdin")
		opts = remote.Options{Stdin: strings.NewReader(strings.Join(envVars, "\n") + "\n")}
	}

	if err := remote.Run(host, opts, remoteArgs(cmd, nil, flags)...); err != nil {
		os.Exit(remote.ExitCode(err))
	}
	os.Exit(0)
}

// uploadEnvFiles copies local env files to ~/.shipyard/envfiles on host
// (0600) and returns their remote absolute paths.
func uploadEnvFiles(host string, files []string) ([]string, error) {
	home, err := remote.Output(host, "sh", "-c", `printf %s "$HOME"`)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", host, err)
	}
	dir := strings.TrimSpace(string(home)) + "/.shipyard/envfiles"

	var out []string
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return nil, err
		}
		fh, err := os.Open(abs)
		if err != nil {
			return nil, fmt.Errorf("env file: %w", err)
		}
		sum := sha256.Sum256([]byte(abs))
		dest := dir + "/" + hex.EncodeToString(sum[:4]) + "-" + filepath.Base(abs)

		ui.Step("upload", "%s -> %s", f, dest)
		_, err = remote.OutputWithInput(host, fh, "sh", "-c",
			`umask 077 && mkdir -p "$1" && cat > "$2.tmp" && mv "$2.tmp" "$2"`, "sh", dir, dest)
		fh.Close()
		if err != nil {
			return nil, fmt.Errorf("uploading %s: %w", f, err)
		}
		out = append(out, dest)
	}
	return out, nil
}

// readEnvStdin appends KEY=VALUE lines from r to envVars.
func readEnvStdin(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if l := strings.TrimRight(sc.Text(), "\r"); strings.TrimSpace(l) != "" {
			envVars = append(envVars, l)
		}
	}
	return sc.Err()
}
