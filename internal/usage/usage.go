// Package usage measures the CPU and memory of supervised processes, each
// counted with its descendants, and keeps an hour of samples in memory.
// It knows nothing about Wails or supervisord.
package usage

import (
	"context"
	"time"
)

// Proc is one process in a table. CPU is its CPU time so far; on Linux it
// includes the children it has reaped. RSS is resident memory in bytes.
type Proc struct {
	PID, PPID int
	CPU       time.Duration
	RSS       uint64
}

// Table is every process on a machine, by PID.
type Table map[int]Proc

// Totals is a process tree: the root and all its descendants.
type Totals struct {
	CPU   time.Duration
	RSS   uint64
	Procs int
}

// Machine is a computer's size: its CPU cores and total memory in bytes
// (0 when unknown).
type Machine struct {
	Cores    int
	MemBytes uint64
}

// Sampler measures the trees rooted at pids. A pid it can't measure is
// left out of the map.
type Sampler interface {
	Sample(ctx context.Context, pids []int) (map[int]Totals, error)
}

// SamplerFunc is a Sampler.
type SamplerFunc func(ctx context.Context, pids []int) (map[int]Totals, error)

// Sample calls f.
func (f SamplerFunc) Sample(ctx context.Context, pids []int) (map[int]Totals, error) {
	return f(ctx, pids)
}

// Tree sums the process pid and its descendants; false when pid isn't in t.
func (t Table) Tree(pid int) (Totals, bool) {
	if _, ok := t[pid]; !ok {
		return Totals{}, false
	}
	var sum Totals
	for _, n := range t.members(pid) {
		sum.CPU += t[n].CPU
		sum.RSS += t[n].RSS
		sum.Procs++
	}
	return sum, true
}

// members is pid and its descendants in t (pid first), each once.
func (t Table) members(pid int) []int {
	if _, ok := t[pid]; !ok {
		return nil
	}
	kids := map[int][]int{}
	for _, p := range t {
		kids[p.PPID] = append(kids[p.PPID], p.PID)
	}
	var out []int
	seen := map[int]bool{}
	stack := []int{pid}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
		stack = append(stack, kids[n]...)
	}
	return out
}

// Trees is Tree for each pid in t.
func (t Table) Trees(pids []int) map[int]Totals {
	out := make(map[int]Totals, len(pids))
	for _, pid := range pids {
		if s, ok := t.Tree(pid); ok {
			out[pid] = s
		}
	}
	return out
}
