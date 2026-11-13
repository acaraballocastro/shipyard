package api

import (
	"context"
	"sync"
	"time"

	"shipyard/internal/core"
)

// Sample is a timestamped core.Stats.
type Sample struct {
	T int64 `json:"t"` // unix milliseconds
	core.Stats
}

const (
	maxSamples     = 120             // per service: 10 minutes at the UI's 5s poll
	minSampleEvery = 4 * time.Second // closer requests reuse the last sample
)

// StatsHistory records the samples served by /stats so that a page
// reload, or a second browser, starts with recent history instead of an
// empty chart. Sampling is driven by requests: nobody watching, no docker
// calls.
type StatsHistory struct {
	mu      sync.Mutex
	last    time.Time
	current map[string]core.Stats
	samples map[string][]Sample
}

// NewStatsHistory returns an empty history.
func NewStatsHistory() *StatsHistory {
	return &StatsHistory{samples: map[string][]Sample{}}
}

// Sample returns current stats, calling fetch at most every few seconds
// and appending each fresh result to the history.
func (h *StatsHistory) Sample(ctx context.Context, fetch func(context.Context) (map[string]core.Stats, error)) (map[string]core.Stats, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != nil && time.Since(h.last) < minSampleEvery {
		return h.current, nil
	}
	cur, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	h.last, h.current = now, cur
	for app, s := range cur {
		list := append(h.samples[app], Sample{T: now.UnixMilli(), Stats: s})
		if len(list) > maxSamples {
			list = list[len(list)-maxSamples:]
		}
		h.samples[app] = list
	}
	// Forget services that disappeared long ago (deleted or stopped).
	cutoff := now.Add(-maxSamples * 10 * time.Second).UnixMilli()
	for app, list := range h.samples {
		if len(list) == 0 || list[len(list)-1].T < cutoff {
			delete(h.samples, app)
		}
	}
	return cur, nil
}

// History returns a copy of the recorded samples per service.
func (h *StatsHistory) History() map[string][]Sample {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string][]Sample, len(h.samples))
	for app, list := range h.samples {
		out[app] = append([]Sample(nil), list...)
	}
	return out
}
