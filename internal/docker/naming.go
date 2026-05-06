package docker

import "strings"

func NormalizeName(name string) string {
	return strings.ToLower(name)
}
