// Package envfile validates, parses and writes Docker Compose env files.
//
// Values are never included in error messages so that secrets cannot leak
// into logs.
package envfile

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// keyPattern matches variable names accepted by Shipyard. It is slightly
// stricter than what Compose tolerates, to catch typos early.
var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// ValidKey reports whether name is an acceptable environment variable name.
func ValidKey(name string) bool {
	return keyPattern.MatchString(name)
}

// ParseAssignment parses a KEY=VALUE string (as given to --env).
// The returned error never contains the value.
func ParseAssignment(s string) (string, string, error) {
	key, value, ok := strings.Cut(s, "=")
	if !ok {
		return "", "", fmt.Errorf("invalid variable %q: expected KEY=VALUE", redactKey(s))
	}
	key = strings.TrimSpace(key)
	if !ValidKey(key) {
		return "", "", fmt.Errorf("invalid variable name %q", key)
	}
	if strings.ContainsAny(value, "\n\r\x00") {
		return "", "", fmt.Errorf("variable %s: value must not contain newlines or NUL bytes", key)
	}
	return key, value, nil
}

// redactKey returns at most the part before '=' so values never leak.
func redactKey(s string) string {
	k, _, _ := strings.Cut(s, "=")
	if len(k) > 64 {
		k = k[:64] + "…"
	}
	return k
}

// Validate checks that path exists, is a regular file and has valid
// env-file syntax: blank lines, '#' comments, KEY=VALUE or bare KEY.
func Validate(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("env file %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("env file %s: not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("env file %s: %w", path, err)
	}
	defer f.Close()
	if _, err := parse(f); err != nil {
		return fmt.Errorf("env file %s: %w", path, err)
	}
	return nil
}

// Read parses an env file into a map. Single- and double-quoted values are
// unquoted the way Shipyard writes them.
func Read(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parse(f)
}

func parse(r io.Reader) (map[string]string, error) {
	vars := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, hasValue := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ValidKey(key) {
			// Only report the line number: the line may hold a secret.
			return nil, fmt.Errorf("line %d: invalid variable name", lineNo)
		}
		if !hasValue {
			vars[key] = ""
			continue
		}
		vars[key] = unquote(strings.TrimSpace(value))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return vars, nil
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '"' && v[len(v)-1] == '"') {
			inner := v[1 : len(v)-1]
			if v[0] == '"' {
				inner = strings.ReplaceAll(inner, `\"`, `"`)
				inner = strings.ReplaceAll(inner, `\\`, `\`)
			}
			return inner
		}
	}
	return v
}

// quote renders a value for an env file. Single quotes are used when
// possible because Compose treats them literally (no $ interpolation).
func quote(v string) string {
	if !strings.Contains(v, "'") {
		return "'" + v + "'"
	}
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	// Note: Compose interpolates ${VAR} inside double quotes. This only
	// applies to values that themselves contain a single quote.
	return `"` + v + `"`
}

// Write atomically writes vars to path with 0600 permissions, keys sorted.
func Write(path string, vars map[string]string) error {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		if !ValidKey(k) {
			return fmt.Errorf("invalid variable name %q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("# Managed by shipyard. Values set with --env.\n")
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(quote(vars[k]))
		b.WriteString("\n")
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".env-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
