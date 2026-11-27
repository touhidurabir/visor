package app

import (
	"context"
	"fmt"
	"time"

	"github.com/touhidurabir/visor/internal/usage"
)

// usageEvery is how often each tab measures its processes' CPU and memory
// (a var so tests sample faster). usagePoints keeps an hour of them, and
// sparkPoints is the dashboard's last 10 minutes.
var usageEvery = 10 * time.Second

const (
	usagePoints = 360
	sparkPoints = 60
)

// UsageView is one process's hour of CPU and memory for the config page.
type UsageView struct {
	Process string
	Points  []usage.Point
	Summary usage.Summary
	EveryMs int64
}

// usageSampler is how this tab measures: the option (tests), the server's
// bridge, or this computer (nil where it can't).
func (r *Runtime) usageSampler() usage.Sampler {
	if r.opts.Usage != nil {
		return r.opts.Usage
	}
	if r.opts.Remote != nil {
		return usage.SamplerFunc(func(ctx context.Context, pids []int) (map[int]usage.Totals, error) {
			fs, _ := r.serverFS()
			return fs.Usage(ctx, pids)
		})
	}
	return usage.Local()
}

// sampleUsage measures after the connection's first poll and then every
// usageEvery, until ctx ends.
func (r *Runtime) sampleUsage(ctx context.Context, first <-chan struct{}, s usage.Sampler) {
	defer r.wg.Done()
	select {
	case <-first:
	case <-ctx.Done():
		return
	}
	tick := time.NewTicker(usageEvery)
	defer tick.Stop()
	for {
		r.sampleOnce(ctx, s)
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
	}
}

// sampleOnce records a sample of every process supervisord reports. A
// failed sample (a server that dropped) records nothing, so the last
// figures stay until the next one.
func (r *Runtime) sampleOnce(ctx context.Context, s usage.Sampler) {
	r.mu.Lock()
	snap := r.last.Snapshot
	r.mu.Unlock()
	if !snap.Reachable {
		return
	}
	var pids []int
	for _, p := range snap.Processes {
		if p.PID > 0 {
			pids = append(pids, p.PID)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, max(usageEvery, 5*time.Second))
	defer cancel()
	got, err := s.Sample(ctx, pids)
	if err != nil {
		return
	}
	now := time.Now()
	names := make([]string, 0, len(snap.Processes))
	for _, p := range snap.Processes {
		name := p.FullName()
		names = append(names, name)
		t, ok := got[p.PID]
		r.usage.Add(now, name, p.PID, t, ok && p.PID > 0)
	}
	r.usage.Keep(names)
	r.emitOverview()
}

// Usage is the hour of CPU and memory of each process the config file at
// path runs (NoConfigFile's processes for "").
func (r *Runtime) Usage(path string) ([]UsageView, error) {
	for _, c := range r.Overview().Configs {
		if c.Path != path {
			continue
		}
		out := make([]UsageView, 0, len(c.Processes))
		for _, p := range c.Processes {
			out = append(out, UsageView{Process: p.FullName, Points: r.usage.Points(p.FullName), Summary: r.usage.Summary(p.FullName), EveryMs: usageEvery.Milliseconds()})
		}
		return out, nil
	}
	return nil, fmt.Errorf("no config file %s on this tab", path)
}

// machineSize is this connection's cores and total memory, read once and
// kept (read again while unknown, and after the server is edited); false
// when it can't be read. The read happens outside the lock, and a server
// gets 3 s, so a stalled link doesn't hold get_status for long.
func (r *Runtime) machineSize() (usage.Machine, bool) {
	r.sizeMu.Lock()
	m := r.size
	r.sizeMu.Unlock()
	if m.Cores > 0 {
		return m, true
	}
	var err error
	if r.opts.Remote != nil {
		fs, _ := r.serverFS()
		fs.Timeout = machineSizeWait
		m, err = fs.Machine()
	} else {
		m, err = usage.LocalMachine()
	}
	if err != nil || m.Cores < 1 {
		return usage.Machine{}, false
	}
	r.sizeMu.Lock()
	r.size = m
	r.sizeMu.Unlock()
	return m, true
}

// machineSizeWait is how long get_status waits for a server's size.
const machineSizeWait = 3 * time.Second
