package usage

import (
	"testing"
	"time"
)

var t0 = time.Unix(1_800_000_000, 0)

func TestHistoryCPU(t *testing.T) {
	h := NewHistory(10)
	h.Add(t0, "w", 5, Totals{CPU: 10 * time.Second, RSS: 1 << 20, Procs: 2}, true)
	if p := h.Current("w"); p.CPU != -1 || p.RSS != 1<<20 || p.AtMs != t0.UnixMilli() {
		t.Fatalf("first sample %+v", p)
	}
	h.Add(t0.Add(10*time.Second), "w", 5, Totals{CPU: 15 * time.Second, RSS: 2 << 20, Procs: 2}, true)
	if p := h.Current("w"); p.CPU != 50 || p.RSS != 2<<20 {
		t.Fatalf("second sample %+v", p)
	}
	h.Add(t0.Add(20*time.Second), "w", 6, Totals{CPU: time.Second, RSS: 3 << 20, Procs: 1}, true) // restarted
	if p := h.Current("w"); p.CPU != -1 || p.RSS != 3<<20 {
		t.Fatalf("after a restart %+v", p)
	}
	h.Add(t0.Add(30*time.Second), "w", 6, Totals{CPU: 0, RSS: 3 << 20, Procs: 1}, true) // went down
	if p := h.Current("w"); p.CPU != 0 {
		t.Fatalf("a decrease %+v", p)
	}
	h.Add(t0.Add(40*time.Second), "w", 6, Totals{}, false)
	if p := h.Current("w"); p.CPU != -1 || p.RSS != -1 {
		t.Fatalf("unknown %+v", p)
	}
	if s := h.Summary("w"); s.Procs != 1 {
		t.Fatalf("procs of the last known sample: %+v", s)
	}
	if p := h.Current("nobody"); p.CPU != -1 || p.RSS != -1 {
		t.Fatalf("no history %+v", p)
	}
}

func TestHistoryRing(t *testing.T) {
	h := NewHistory(3)
	cpu := time.Duration(0)
	for i := range 5 {
		cpu += time.Duration(i) * time.Second
		h.Add(t0.Add(time.Duration(i)*10*time.Second), "w", 1, Totals{CPU: cpu, RSS: uint64(i+1) * 100, Procs: 1}, true)
	}
	pts := h.Points("w")
	if len(pts) != 3 || pts[0].RSS != 300 || pts[2].RSS != 500 {
		t.Fatalf("points %+v", pts)
	}
	if pts[0].CPU != 20 || pts[2].CPU != 40 {
		t.Fatalf("cpu %+v", pts)
	}
	s := h.Summary("w")
	if s.PeakCPU != 40 || s.AvgCPU != 30 || s.PeakRSS != 500 || s.AvgRSS != 400 {
		t.Fatalf("summary %+v", s)
	}
	if sp := h.Spark("w", 5); len(sp) != 3 || sp[0] != 20 {
		t.Fatalf("spark %v", sp)
	}
	h.Add(t0, "v", 2, Totals{RSS: 1}, true)
	if sp := h.Spark("v", 5); len(sp) != 1 || sp[0] != 0 {
		t.Fatalf("unknown cpu in a spark is 0: %v", sp)
	}
	h.Keep([]string{"v"})
	if len(h.Points("w")) != 0 || len(h.Points("v")) != 1 {
		t.Fatal("Keep didn't drop w")
	}
}
