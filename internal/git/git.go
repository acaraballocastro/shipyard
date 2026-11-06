package git

import (
	"fmt"
	"os"
	"strings"

	"shipyard/internal/system"
)

func Clone(repo string) (string, error) {
	// Guard against option injection and path traversal: repo is used both
	// as a git argument and as part of a path passed to os.RemoveAll.
	if repo == "" || strings.HasPrefix(repo, "-") || strings.Contains(repo, "..") ||
		strings.Count(repo, "/") != 1 || strings.ContainsAny(repo, " \t\n\\:") {
		return "", fmt.Errorf("invalid repository %q: expected owner/name", repo)
	}

	url := fmt.Sprintf("git@github.com:%s.git", repo)
	dir := "/srv/repos/" + repo

	os.RemoveAll(dir)

	if err := system.RunCommand("git", "clone", "--depth", "1", url, dir); err != nil {
		return "", fmt.Errorf("cloning %s: %w", repo, err)
	}

	return dir, nil
}
