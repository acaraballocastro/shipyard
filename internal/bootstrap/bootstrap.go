// Package bootstrap checks and prepares a machine to run shipyard:
// system packages, Docker access, working directories, GHCR login and
// SSH access to GitHub. Every step is idempotent.
package bootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Dirs are the paths shipyard writes to (compose files and clones).
var Dirs = []string{"/srv/apps", "/srv/repos"}

// ErrRelogin means a fix (docker group) only applies to new login sessions.
var ErrRelogin = errors.New("log out and back in (or reconnect) for the docker group to apply")

// Step is one requirement.
type Step struct {
	Name string
	// Check returns a short detail and nil when the requirement is met.
	Check func() (string, error)
	// Fix tries to satisfy the requirement; nil when it cannot be automated.
	Fix func() error
	// Hint explains how to fix it by hand.
	Hint string
	// Interactive steps need user input in Fix (asked inline).
	Interactive bool
}

// Inputs are callbacks for values asked during fixes.
type Inputs struct {
	// Token asks for a GitHub token (for GHCR login and SSH key upload).
	Token func() (string, error)
	// WaitKeyAdded shows a public key and waits until the user added it.
	WaitKeyAdded func(pubKey string) error
}

// Steps returns the requirements in dependency order. githubUser is read
// lazily so a step can rely on a value asked earlier.
func Steps(githubUser func() string, in Inputs) []Step {
	linux := runtime.GOOS == "linux"
	onlyLinux := func(f func() error) func() error {
		if linux {
			return f
		}
		return nil
	}
	var token string
	getToken := func() (string, error) {
		if token != "" || in.Token == nil {
			return token, nil
		}
		t, err := in.Token()
		token = strings.TrimSpace(t)
		return token, err
	}

	return []Step{
		{
			Name:  "git",
			Check: lookPath("git"),
			Fix:   onlyLinux(func() error { return installPackages("git") }),
			Hint:  "install git with your package manager",
		},
		{
			Name:  "docker",
			Check: lookPath("docker"),
			Fix: onlyLinux(func() error {
				return run("sh", "-c", "curl -fsSL https://get.docker.com | sudo sh")
			}),
			Hint: "install Docker: https://docs.docker.com/engine/install/",
		},
		{
			Name: "daemon",
			Check: func() (string, error) {
				out, err := output("docker", "version", "--format", "{{.Server.Version}}")
				if err != nil {
					return "", errors.New("cannot talk to the Docker daemon as this user")
				}
				return "Docker " + out, nil
			},
			Fix: onlyLinux(func() error {
				u, err := user.Current()
				if err != nil {
					return err
				}
				_ = run("sudo", "systemctl", "enable", "--now", "docker")
				if _, err := output("docker", "info"); err == nil {
					return nil
				}
				if err := run("sudo", "usermod", "-aG", "docker", u.Username); err != nil {
					return err
				}
				return ErrRelogin
			}),
			Hint: "start Docker and add your user to the docker group",
		},
		{
			Name:  "compose",
			Check: versionOf("docker", "compose", "version", "--short"),
			Fix:   onlyLinux(func() error { return installPackages("docker-compose-plugin") }),
			Hint:  "install the Docker Compose v2 plugin",
		},
		{
			Name:  "buildx",
			Check: versionOf("docker", "buildx", "version"),
			Fix:   onlyLinux(func() error { return installPackages("docker-buildx-plugin") }),
			Hint:  "install the Docker Buildx plugin",
		},
		{
			Name: "dirs",
			Check: func() (string, error) {
				for _, d := range Dirs {
					if err := writable(d); err != nil {
						return "", fmt.Errorf("%s is not writable", d)
					}
				}
				return strings.Join(Dirs, ", "), nil
			},
			Fix: func() error {
				u, err := user.Current()
				if err != nil {
					return err
				}
				args := append([]string{"mkdir", "-p"}, Dirs...)
				if err := run("sudo", args...); err != nil {
					return err
				}
				return run("sudo", append([]string{"chown", u.Username}, Dirs...)...)
			},
			Hint: "sudo mkdir -p /srv/apps /srv/repos && sudo chown $USER /srv/apps /srv/repos",
		},
		{
			Name:        "ghcr",
			Interactive: true,
			Check: func() (string, error) {
				if hasRegistryAuth("ghcr.io") {
					return "logged in", nil
				}
				return "", errors.New("not logged in to ghcr.io")
			},
			Fix: func() error {
				u := githubUser()
				if u == "" {
					return errors.New("GitHub user is not set")
				}
				t, err := getToken()
				if err != nil {
					return err
				}
				if t == "" {
					return errors.New("a token is required")
				}
				c := exec.Command("docker", "login", "ghcr.io", "-u", u, "--password-stdin")
				c.Stdin = strings.NewReader(t)
				c.Stdout, c.Stderr = os.Stdout, os.Stderr
				return c.Run()
			},
			Hint: "docker login ghcr.io -u <github user> (token with write:packages)",
		},
		{
			Name:  "hostkey",
			Check: githubKnownHost,
			Fix:   addGitHubKnownHost,
			Hint:  "ssh-keyscan github.com >> ~/.ssh/known_hosts",
		},
		{
			Name:        "github",
			Interactive: true,
			Check:       githubSSH,
			Fix: func() error {
				pub, err := ensureSSHKey()
				if err != nil {
					return err
				}
				if t, _ := getToken(); t != "" {
					if err := uploadKey(t, pub); err == nil {
						return nil
					}
				}
				if in.WaitKeyAdded == nil {
					return errors.New("add ~/.ssh/id_ed25519.pub to https://github.com/settings/ssh/new")
				}
				return in.WaitKeyAdded(pub)
			},
			Hint: "add an SSH key to https://github.com/settings/keys (test: ssh -T git@github.com)",
		},
	}
}

// =========================
// HELPERS
// =========================

func lookPath(name string) func() (string, error) {
	return func() (string, error) {
		p, err := exec.LookPath(name)
		if err != nil {
			return "", errors.New("not installed")
		}
		return p, nil
	}
}

func versionOf(name string, args ...string) func() (string, error) {
	return func() (string, error) {
		out, err := output(name, args...)
		if err != nil {
			return "", errors.New("not installed")
		}
		return firstLine(out), nil
	}
}

func output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// installPackages uses the system package manager through sudo.
func installPackages(pkgs ...string) error {
	switch {
	case has("apt-get"):
		if err := run("sudo", "apt-get", "update", "-q"); err != nil {
			return err
		}
		return run("sudo", append([]string{"apt-get", "install", "-y", "-q"}, pkgs...)...)
	case has("dnf"):
		return run("sudo", append([]string{"dnf", "install", "-y"}, pkgs...)...)
	case has("apk"):
		return run("sudo", append([]string{"apk", "add"}, pkgs...)...)
	case has("pacman"):
		return run("sudo", append([]string{"pacman", "-S", "--noconfirm"}, pkgs...)...)
	}
	return errors.New("no supported package manager found")
}

func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".shipyard-check-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

// hasRegistryAuth reports whether ~/.docker/config.json has credentials
// for registry (inline or through a credential helper).
func hasRegistryAuth(registry string) bool {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return false
	}
	var cfg struct {
		Auths       map[string]json.RawMessage `json:"auths"`
		CredHelpers map[string]string          `json:"credHelpers"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}
	_, a := cfg.Auths[registry]
	_, h := cfg.CredHelpers[registry]
	return a || h
}

func githubKnownHost() (string, error) {
	if err := exec.Command("ssh-keygen", "-F", "github.com").Run(); err != nil {
		return "", errors.New("github.com host key not trusted yet")
	}
	return "github.com", nil
}

func addGitHubKnownHost() error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	keys, err := exec.Command("ssh-keyscan", "-t", "ed25519,rsa,ecdsa", "github.com").Output()
	if err != nil || len(bytes.TrimSpace(keys)) == 0 {
		return fmt.Errorf("ssh-keyscan github.com failed: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "known_hosts"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(keys)
	return err
}

// githubSSH runs `ssh -T git@github.com`, which exits 1 even on success.
func githubSSH() (string, error) {
	out, _ := exec.Command("ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
		"git@github.com").CombinedOutput()
	s := string(out)
	if i := strings.Index(s, "Hi "); i >= 0 && strings.Contains(s, "successfully authenticated") {
		name, _, _ := strings.Cut(s[i+3:], "!")
		return "authenticated as " + name, nil
	}
	return "", errors.New("no SSH access to GitHub")
}

// ensureSSHKey returns the public key, creating ~/.ssh/id_ed25519 if no
// key exists yet.
func ensureSSHKey() (string, error) {
	home, _ := os.UserHomeDir()
	for _, n := range []string{"id_ed25519.pub", "id_ecdsa.pub", "id_rsa.pub"} {
		if b, err := os.ReadFile(filepath.Join(home, ".ssh", n)); err == nil {
			return strings.TrimSpace(string(b)), nil
		}
	}
	host, _ := os.Hostname()
	key := filepath.Join(home, ".ssh", "id_ed25519")
	if err := os.MkdirAll(filepath.Dir(key), 0700); err != nil {
		return "", err
	}
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-f", key,
		"-C", "shipyard@"+host).CombinedOutput(); err != nil {
		return "", fmt.Errorf("ssh-keygen: %s", out)
	}
	b, err := os.ReadFile(key + ".pub")
	return strings.TrimSpace(string(b)), err
}

// uploadKey adds pub to the token owner's GitHub account. Requires the
// admin:public_key scope; 422 means the key is already registered.
func uploadKey(token, pub string) error {
	host, _ := os.Hostname()
	body, _ := json.Marshal(map[string]string{"title": "shipyard@" + host, "key": pub})
	req, err := http.NewRequest("POST", "https://api.github.com/user/keys", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == 201 || resp.StatusCode == 422 {
		return nil
	}
	return fmt.Errorf("GitHub API: %s", resp.Status)
}
