package usage

import (
	"sync"
	"time"
)

// Point is one sample. CPU is a percentage of one core and RSS bytes; each
// is -1 when unknown (before a second sample, or not measurable).
type Point struct {
	AtMs int64
	CPU  float64
	RSS  int64
}

// Summary is a process's history: the peak and average of the known
// values, and how many processes its last known sample counted.
type Summary struct {
	PeakCPU, AvgCPU float64
	PeakRSS, AvgRSS int64
	Procs           int
}

// History keeps the last size samples of each process, by name. It is safe
// for concurrent use.
type History struct {
	mu   sync.Mutex
	size int
	of   map[string]*track
}

type track struct {
	pts   []Point // a ring once full; next is the oldest
	next  int
	pid   int
	at    time.Time
	cpu   time.Duration
	base  bool // pid/at/cpu hold the last known sample
	procs int
}

// NewHistory keeps size samples per process (360 is an hour at 10 s).
func NewHistory(size int) *History { return &History{size: max(size, 1), of: map[string]*track{}} }

// Add records a sample of name (process pid) taken at at. ok false records
// an unknown point. CPU is the change from the previous sample of the same
// pid; a new pid (a restart) starts over.
func (h *History) Add(at time.Time, name string, pid int, t Totals, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tr := h.of[name]
	if tr == nil {
		tr = &track{}
		h.of[name] = tr
	}
	p := Point{AtMs: at.UnixMilli(), CPU: -1, RSS: -1}
	if ok {
		p.RSS = int64(t.RSS)
		if tr.base && tr.pid == pid && at.After(tr.at) {
			p.CPU = max(0, float64(t.CPU-tr.cpu)/float64(at.Sub(tr.at))*100)
		}
		tr.pid, tr.at, tr.cpu, tr.base, tr.procs = pid, at, t.CPU, true, t.Procs
	} else {
		tr.base = false
	}
	if len(tr.pts) < h.size {
		tr.pts = append(tr.pts, p)
		return
	}
	tr.pts[tr.next] = p
	tr.next = (tr.next + 1) % h.size
}

func (tr *track) ordered() []Point {
	out := make([]Point, 0, len(tr.pts))
	out = append(out, tr.pts[tr.next:]...)
	return append(out, tr.pts[:tr.next]...)
}

// Points is name's samples, oldest first.
func (h *History) Points(name string) []Point {
	h.mu.Lock()
	defer h.mu.Unlock()
	if tr := h.of[name]; tr != nil {
		return tr.ordered()
	}
	return nil
}

// Current is name's latest sample (unknown when there is none).
func (h *History) Current(name string) Point {
	h.mu.Lock()
	defer h.mu.Unlock()
	tr := h.of[name]
	if tr == nil || len(tr.pts) == 0 {
		return Point{CPU: -1, RSS: -1}
	}
	return tr.pts[(tr.next+len(tr.pts)-1)%len(tr.pts)]
}

// Spark is name's last n CPU values, oldest first, unknown ones as 0.
func (h *History) Spark(name string, n int) []float64 {
	pts := h.Points(name)
	pts = pts[max(0, len(pts)-n):]
	out := make([]float64, len(pts))
	for i, p := range pts {
		out[i] = max(p.CPU, 0)
	}
	return out
}

// Summary is name's peak and average over its history.
func (h *History) Summary(name string) Summary {
	h.mu.Lock()
	var procs int
	if tr := h.of[name]; tr != nil {
		procs = tr.procs
	}
	h.mu.Unlock()
	s := Summary{Procs: procs}
	var cpuN, rssN int
	var cpuSum float64
	var rssSum int64
	for _, p := range h.Points(name) {
		if p.CPU >= 0 {
			s.PeakCPU = max(s.PeakCPU, p.CPU)
			cpuSum += p.CPU
			cpuN++
		}
		if p.RSS >= 0 {
			s.PeakRSS = max(s.PeakRSS, p.RSS)
			rssSum += p.RSS
			rssN++
		}
	}
	if cpuN > 0 {
		s.AvgCPU = cpuSum / float64(cpuN)
	}
	if rssN > 0 {
		s.AvgRSS = rssSum / int64(rssN)
	}
	return s
}

// Keep forgets every process not in names.
func (h *History) Keep(names []string) {
	keep := make(map[string]bool, len(names))
	for _, n := range names {
		keep[n] = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for n := range h.of {
		if !keep[n] {
			delete(h.of, n)
		}
	}
}
