package core

import "testing"

func TestHostParsers(t *testing.T) {
	if c, ok := parseProcStat("cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 1 2 3 4\n"); !ok || c[0] != 1000 || c[1] != 850 {
		t.Fatalf("proc stat: %v %v", c, ok)
	}
	if l := parseLoadavg("0.52 0.41 0.30 1/234 5678\n"); l[0] != 0.52 || l[2] != 0.30 {
		t.Fatalf("loadavg: %v", l)
	}
	total, used, swap := parseMeminfo("MemTotal:  4000 kB\nMemFree: 100 kB\nMemAvailable: 1000 kB\nSwapTotal: 500 kB\nSwapFree: 400 kB\n")
	if total != 4000*1024 || used != 3000*1024 || swap != 100*1024 {
		t.Fatalf("meminfo: %d %d %d", total, used, swap)
	}
	d := parseSystemDF([]byte(`{"Type":"Images","Size":"1.5GB","Reclaimable":"512MB (34%)"}
{"Type":"Build Cache","Size":"200MB","Reclaimable":"200MB"}
{"Type":"Local Volumes","Size":"3.4kB","Reclaimable":"0B (0%)"}`))
	if d.Images != 1.5e9 || d.ImagesReclaim != 512e6 || d.BuildCache != 200e6 || d.Volumes != 3400 {
		t.Fatalf("system df: %+v", d)
	}
}
