package git

import (
	"fmt"
	"os"

	"shipyard/internal/system"
)

func Clone(repo string) (string, error) {
	url := fmt.Sprintf("git@github.com:%s.git", repo)
	dir := "/srv/repos/" + repo

	os.RemoveAll(dir)

	if err := system.RunCommand("git", "clone", url, dir); err != nil {
		return "", fmt.Errorf("cloning %s: %w", repo, err)
	}

	return dir, nil
}
