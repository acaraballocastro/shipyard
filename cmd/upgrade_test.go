package cmd

import (
	"context"
	"errors"
	"testing"

	"shipyard/internal/api"
	"shipyard/internal/core"
	"shipyard/internal/events"
)

// Upgrade jobs live in the gateway; other ids go to the server.
func TestGatewayRemoteRoutesJobs(t *testing.T) {
	jobs := api.NewJobs(&core.Service{})
	jobs.Prefix = gatewayJobPrefix
	g := gatewayRemote{Remote: api.Remote{URL: "http://127.0.0.1:1"}, name: "pi", jobs: jobs}

	job, err := jobs.Launch("shipyard@pi", "upgrade", func(context.Context, events.Progress) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got, err := g.Job(context.Background(), job.ID); err != nil || got.Action != "upgrade" {
		t.Fatalf("local job: %+v %v", got, err)
	}
	if _, err := g.Job(context.Background(), "123-1-abc"); err == nil || errors.Is(err, api.ErrJobNotFound) {
		t.Fatalf("server job should be relayed (and fail: unreachable), got %v", err)
	}
}
