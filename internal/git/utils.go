package git

import "strings"

func ExtractRepoName(full string) string {
	parts := strings.Split(full, "/")
	return parts[len(parts)-1]
}
