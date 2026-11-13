package core

import "testing"

func TestParseContainers(t *testing.T) {
	raw := []byte(`{"Names":"web","Image":"nginx","State":"running","Status":"Up 2 hours","Labels":"com.docker.compose.project=web"}
{"Names":"cloudflared","Image":"cloudflare/cloudflared","State":"running","Status":"Up 5 days","Labels":"com.docker.compose.project=tunnel,x=y"}
{"Names":"db-1","Image":"postgres","State":"exited","Status":"Exited (0) 1 day ago","Labels":"com.docker.compose.project=api"}
garbage
`)
	got := parseContainers(raw, []App{{Name: "web"}, {Name: "api"}})
	if len(got) != 3 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	byName := map[string]Container{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if !byName["web"].Managed || byName["web"].App != "web" {
		t.Errorf("web: %+v", byName["web"])
	}
	if c := byName["db-1"]; !c.Managed || c.App != "api" {
		t.Errorf("container of a managed compose project: %+v", c)
	}
	if c := byName["cloudflared"]; c.Managed || c.Project != "tunnel" {
		t.Errorf("unmanaged: %+v", c)
	}
	if got[0].Name != "cloudflared" {
		t.Error("not sorted")
	}
}
