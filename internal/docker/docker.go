package docker

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"shipyard/internal/system"
)

// DefaultPlatform is the platform used when none is configured.
const DefaultPlatform = "linux/arm64"

// BuildOptions configures docker buildx build.
type BuildOptions struct {
	Platform string
	// Args are passed as --build-arg KEY=VALUE.
	Args map[string]string
	// ArgsFromEnv are passed as --build-arg KEY so that Docker reads the
	// value from shipyard's environment; the value never appears in argv.
	ArgsFromEnv []string
}

// BuildArgs returns the argv for docker buildx build (without "docker").
// Arguments are passed directly to exec, never through a shell.
func BuildArgs(image, path string, opts BuildOptions) []string {
	platform := opts.Platform
	if platform == "" {
		platform = DefaultPlatform
	}
	args := []string{"buildx", "build", "--platform", platform}

	keys := make([]string, 0, len(opts.Args))
	for k := range opts.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--build-arg", k+"="+opts.Args[k])
	}
	for _, k := range opts.ArgsFromEnv {
		args = append(args, "--build-arg", k)
	}

	return append(args, "-t", fmt.Sprintf("%s:latest", image), "--push", path)
}

// BuildAndPush builds for the default platform (linux/arm64) and pushes.
func BuildAndPush(image string, path string) error {
	return BuildAndPushWith(image, path, BuildOptions{})
}

// BuildAndPushWith builds with the given options and pushes the image.
func BuildAndPushWith(image string, path string, opts BuildOptions) error {
	for _, k := range opts.ArgsFromEnv {
		if _, ok := os.LookupEnv(k); !ok {
			return fmt.Errorf("build arg %s: not set in the environment", k)
		}
	}
	return system.RunCommand("docker", BuildArgs(image, path, opts)...)
}

func Push(image string, tag string) error {
	full := fmt.Sprintf("%s:%s", image, tag)
	return system.RunCommand("docker", "push", full)
}

// Login authenticates against ghcr.io. The token is passed on stdin and
// never through a shell or argv.
func Login(user string, token string) error {
	cmd := exec.Command("docker", "login", "ghcr.io", "-u", user, "--password-stdin")
	cmd.Stdin = strings.NewReader(token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
