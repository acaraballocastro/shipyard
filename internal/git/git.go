package git

import (
	"fmt"
	"os"

	"shipyard/internal/system"
)

func Clone(repo string) (string, error) {
	url := fmt.Sprintf("https://github.com/%s.git", repo)

	dir := "./tmp/" + repo

	// limpiar si existe
	os.RemoveAll(dir)

	err := system.RunCommand("git", "clone", url, dir)
	if err != nil {
		return "", err
	}

	return dir, nil
}
