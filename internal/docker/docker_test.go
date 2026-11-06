package docker

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBuildArgsDefaultPlatform(t *testing.T) {
	got := BuildArgs("ghcr.io/u/app", "/src", BuildOptions{})
	want := []string{"buildx", "build", "--platform", "linux/arm64", "-t", "ghcr.io/u/app:latest", "--push", "/src"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestBuildArgsWithOptions(t *testing.T) {
	got := BuildArgs("img", "/src", BuildOptions{
		Platform:    "linux/amd64",
		Args:        map[string]string{"B": "2", "A": "https://x/api?a=1&b=2"},
		ArgsFromEnv: []string{"NPM_TOKEN"},
	})
	want := []string{"buildx", "build", "--platform", "linux/amd64",
		"--build-arg", "A=https://x/api?a=1&b=2",
		"--build-arg", "B=2",
		"--build-arg", "NPM_TOKEN",
		"-t", "img:latest", "--push", "/src"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestBuildFromEnvMissing(t *testing.T) {
	err := BuildAndPushWith("img", "/nonexistent", BuildOptions{ArgsFromEnv: []string{"SHIPYARD_DEFINITELY_UNSET_123"}})
	if err == nil {
		t.Fatal("expected error for unset env build arg")
	}
}

func TestWaitReady(t *testing.T) {
	seq := []ContainerState{{Status: "running", Running: true, Health: "starting"}, {Status: "running", Running: true, Health: "healthy"}}
	i := 0
	f := func(string) (ContainerState, error) {
		s := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return s, nil
	}
	st, err := waitReady(f, "x", time.Second, time.Millisecond)
	if err != nil || st.Health != "healthy" {
		t.Fatalf("got %+v %v", st, err)
	}

	unhealthy := func(string) (ContainerState, error) { return ContainerState{Running: true, Health: "unhealthy"}, nil }
	if _, err := waitReady(unhealthy, "x", time.Second, time.Millisecond); err == nil {
		t.Fatal("expected unhealthy error")
	}

	missing := func(string) (ContainerState, error) { return ContainerState{}, errors.New("nope") }
	if _, err := waitReady(missing, "x", 5*time.Millisecond, time.Millisecond); err == nil {
		t.Fatal("expected timeout")
	}
}
