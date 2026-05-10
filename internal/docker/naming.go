package docker

import "strings"

func NormalizeName(name string) string {
	return strings.ToLower(name)
}

func AdvancedNormalizeName(name string) string {

	// quitar registry path
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		name = parts[len(parts)-1]
	}

	// quitar tags tipo :latest
	if strings.Contains(name, ":") {
		name = strings.Split(name, ":")[0]
	}

	// docker-safe lowercase
	return strings.ToLower(name)
}
