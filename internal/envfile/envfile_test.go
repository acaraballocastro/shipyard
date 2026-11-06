package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAssignment(t *testing.T) {
	k, v, err := ParseAssignment("APP_ENV=production=x")
	if err != nil || k != "APP_ENV" || v != "production=x" {
		t.Fatalf("got %q %q %v", k, v, err)
	}
	if _, _, err := ParseAssignment("NOVALUE"); err == nil {
		t.Fatal("expected error for missing '='")
	}
	if _, _, err := ParseAssignment("1BAD=x"); err == nil {
		t.Fatal("expected error for invalid name")
	}
	_, _, err = ParseAssignment("TOKEN=line1\nsupersecret")
	if err == nil || strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("expected error without value, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.env")
	os.WriteFile(good, []byte("# comment\n\nA=1\nB='two words'\nPASSTHROUGH\n"), 0600)
	if err := Validate(good); err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}

	bad := filepath.Join(dir, "bad.env")
	os.WriteFile(bad, []byte("A=1\nnot valid hunter2=x\n"), 0600)
	err := Validate(bad)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected line 2 error, got %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatal("error leaks line content")
	}

	if err := Validate(filepath.Join(dir, "missing.env")); err == nil {
		t.Fatal("expected error for missing file")
	}
	if err := Validate(dir); err == nil {
		t.Fatal("expected error for directory")
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	in := map[string]string{"A": "plain", "B": "has $dollar", "C": "it's quoted", "D": ""}
	if err := Write(p, in); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("perm = %v, want 0600", fi.Mode().Perm())
	}
	out, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range in {
		if out[k] != v {
			t.Errorf("%s: got %q want %q", k, out[k], v)
		}
	}
	if err := Validate(p); err != nil {
		t.Fatalf("written file invalid: %v", err)
	}
}
