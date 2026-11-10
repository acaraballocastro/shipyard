package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// TokenPath is where `shipyard serve` keeps its API token (0600).
func TokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".shipyard", "serve-token")
}

// NewToken returns a random 256-bit token.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// LoadOrCreateToken returns the server token, creating it on first use.
func LoadOrCreateToken() (string, error) {
	b, err := os.ReadFile(TokenPath())
	if err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b)), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return RotateToken()
}

// RotateToken replaces the server token, invalidating the old one.
func RotateToken() (string, error) {
	t := NewToken()
	if err := os.MkdirAll(filepath.Dir(TokenPath()), 0700); err != nil {
		return "", err
	}
	return t, os.WriteFile(TokenPath(), []byte(t+"\n"), 0600)
}
