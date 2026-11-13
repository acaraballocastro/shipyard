package core

import "testing"

func TestParseStats(t *testing.T) {
	raw := []byte(`{"Name":"web","CPUPerc":"12.50%","MemPerc":"3.10%","MemUsage":"60MiB / 1.9GiB","NetIO":"1kB / 2kB","BlockIO":"0B / 0B","PIDs":"7"}
{"Name":"other-container","CPUPerc":"1%"}
not json
`)
	got := parseStats(raw, map[string]bool{"web": true})
	if len(got) != 1 {
		t.Fatalf("want only known services, got %v", got)
	}
	w := got["web"]
	if w.CPU != 12.5 || w.MemPerc != 3.1 || w.PIDs != 7 || w.MemUsage != "60MiB / 1.9GiB" {
		t.Fatalf("bad parse: %+v", w)
	}
}
