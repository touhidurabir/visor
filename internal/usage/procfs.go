package usage

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ParseStat reads one /proc/<pid>/stat line. tick is one clock tick
// (1/USER_HZ) and page the page size in bytes. The command name may hold
// spaces and parentheses, so fields are counted from its last ")".
func ParseStat(line string, tick time.Duration, page uint64) (Proc, error) {
	open, end := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
	if open < 1 || end < open {
		return Proc{}, errors.New("not a stat line")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line[:open]))
	if err != nil {
		return Proc{}, err
	}
	// rest[0] is field 3 (state), so field n is rest[n-3].
	rest := strings.Fields(line[end+1:])
	if len(rest) < 24-2 {
		return Proc{}, errors.New("stat line too short")
	}
	num := func(n int) (int64, error) { return strconv.ParseInt(rest[n-3], 10, 64) }
	ppid, err := num(4)
	if err != nil {
		return Proc{}, err
	}
	var ticks int64
	for n := 14; n <= 17; n++ {
		v, err := num(n)
		if err != nil {
			return Proc{}, err
		}
		ticks += v
	}
	rss, err := num(24)
	if err != nil {
		return Proc{}, err
	}
	return Proc{PID: pid, PPID: int(ppid), CPU: time.Duration(ticks) * tick, RSS: uint64(max(rss, 0)) * page}, nil
}

// ReadProc reads every process under root (normally /proc). A process that
// exits while being read is skipped.
func ReadProc(root string, tick time.Duration, page uint64) (Table, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	t := make(Table, len(ents))
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "stat"))
		if err != nil {
			continue
		}
		if p, err := ParseStat(string(b), tick, page); err == nil {
			t[p.PID] = p
		}
	}
	return t, nil
}
