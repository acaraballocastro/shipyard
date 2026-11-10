package git

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"shipyard/internal/system"
)

// Clone shallow-clones repo into /srv/repos, writing git output to out.
func Clone(ctx context.Context, repo string, out io.Writer) (string, error) {
	// Guard against option injection and path traversal: repo is used both
	// as a git argument and as part of a path passed to os.RemoveAll.
	if repo == "" || strings.HasPrefix(repo, "-") || strings.Contains(repo, "..") ||
		strings.Count(repo, "/") != 1 || strings.ContainsAny(repo, " \t\n\\:") {
		return "", fmt.Errorf("invalid repository %q: expected owner/name", repo)
	}

	url := fmt.Sprintf("git@github.com:%s.git", repo)
	dir := "/srv/repos/" + repo

	os.RemoveAll(dir)

	if err := system.Run(ctx, out, "git", "clone", "--depth", "1", url, dir); err != nil {
		return "", fmt.Errorf("cloning %s: %w", repo, err)
	}

	return dir, nil
}
