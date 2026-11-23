package usage

/*
#include <libproc.h>
#include <sys/proc_info.h>
#include <mach/mach_time.h>

static int task_info_of(int pid, uint64_t *cpu, uint64_t *rss) {
	struct proc_taskinfo ti;
	int n = proc_pidinfo(pid, PROC_PIDTASKINFO, 0, &ti, sizeof ti);
	if (n != (int)sizeof ti) return -1;
	*cpu = ti.pti_total_user + ti.pti_total_system;
	*rss = ti.pti_resident_size;
	return 0;
}

static void timebase(uint32_t *numer, uint32_t *denom) {
	mach_timebase_info_data_t tb;
	mach_timebase_info(&tb);
	*numer = tb.numer;
	*denom = tb.denom;
}
*/
import "C"

import (
	"context"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

// Local measures this computer's processes: kern.proc.all for every
// process's parent, then proc_pidinfo for the processes in the asked trees.
// A process macOS won't describe (another user's) is left out, so a tree
// whose root is refused is unknown. CPU time counts live processes only:
// macOS keeps no total for reaped children.
func Local() Sampler {
	var numer, denom C.uint32_t
	C.timebase(&numer, &denom) // CPU times are Mach ticks on Apple Silicon
	return SamplerFunc(func(_ context.Context, pids []int) (map[int]Totals, error) {
		procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
		if err != nil {
			return nil, err
		}
		all := make(Table, len(procs))
		for _, p := range procs {
			all[int(p.Proc.P_pid)] = Proc{PID: int(p.Proc.P_pid), PPID: int(p.Eproc.Ppid)}
		}
		t := Table{}
		for _, root := range pids {
			for _, pid := range all.members(root) {
				if _, done := t[pid]; done {
					continue
				}
				var cpu, rss C.uint64_t
				if C.task_info_of(C.int(pid), &cpu, &rss) != 0 {
					continue
				}
				p := all[pid]
				p.CPU = time.Duration(uint64(cpu) * uint64(numer) / uint64(denom))
				p.RSS = uint64(rss)
				t[pid] = p
			}
		}
		return t.Trees(pids), nil
	})
}

// LocalMachine is this computer's core count and total memory.
func LocalMachine() (Machine, error) {
	mem, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return Machine{}, err
	}
	return Machine{Cores: runtime.NumCPU(), MemBytes: mem}, nil
}
