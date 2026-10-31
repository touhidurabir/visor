package app

import (
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/supervisor"
)

func TestExpectationMatchers(t *testing.T) {
	cases := []struct {
		name string
		e    expectation
		yes  []string
		no   []string
	}{
		{"process", expectProcess("g:w"), []string{"g:w"}, []string{"g:v", "h:w", "w:w", "g:w2"}},
		{"bare name", expectProcess("web"), []string{"web:web"}, []string{"web:other", "other:web", "web"}},
		{"group wildcard", expectProcess("g:*"), []string{"g:a", "g:b"}, []string{"h:a", "gg:a"}},
		{"groups", expectGroups("a", "b"), []string{"a:x", "b:y"}, []string{"c:x", "ab:x"}},
		{"no groups", expectGroups(), nil, []string{"a:x"}},
		{"all", expectAll(), []string{"a:x", "b:y"}, nil},
	}
	for _, c := range cases {
		for _, p := range c.yes {
			if !c.e.match(p) {
				t.Errorf("%s: does not match %q", c.name, p)
			}
		}
		for _, p := range c.no {
			if c.e.match(p) {
				t.Errorf("%s: matches %q", c.name, p)
			}
		}
		if c.e.daemon {
			t.Errorf("%s: covers the daemon", c.name)
		}
	}
}

func TestExitReasonFrom(t *testing.T) {
	stop := time.Date(2026, 10, 31, 14, 36, 34, 0, time.Local) // whole seconds, like supervisord
	worker := supervisor.ProcessInfo{Name: "worker", Group: "jobs", Stop: stop}
	line := func(name string, after time.Duration, status int, expected bool) supervisor.ExitLine {
		return supervisor.ExitLine{At: stop.Add(after), Name: name, Desc: "exit status x", Status: status, Expected: expected}
	}
	stopped := func(name string, after time.Duration) supervisor.ExitLine {
		return supervisor.ExitLine{At: stop.Add(after), Name: name, Desc: "terminated by SIGTERM", Status: -1, Stopped: true}
	}
	exits := func(l ...supervisor.ExitLine) supervisor.ProcessLog { return supervisor.ProcessLog{Exits: l} }
	onRequest := issues.ExitReason{Desc: "terminated by SIGTERM", Status: -1, Deliberate: true}
	procs := []supervisor.ProcessInfo{worker, {Name: "web", Group: "web"}}
	cases := []struct {
		name string
		log  supervisor.ProcessLog
		info supervisor.ProcessInfo
		want issues.ExitReason
		ok   bool
	}{
		{"match", exits(line("worker", 698*time.Millisecond, 1, false)), worker,
			issues.ExitReason{Desc: "exit status x", Status: 1}, true},
		{"latest of several", exits(
			line("worker", -1500*time.Millisecond, 0, true),
			line("web", 300*time.Millisecond, 9, false),
			line("worker", 900*time.Millisecond, 2, false),
		), worker, issues.ExitReason{Desc: "exit status x", Status: 2}, true},
		{"earlier within 2s", exits(line("worker", -2*time.Second, 0, true)), worker,
			issues.ExitReason{Desc: "exit status x", Status: 0, Expected: true}, true},
		{"too old", exits(line("worker", -3*time.Second, 1, false)), worker, issues.ExitReason{}, false},
		{"too new", exits(line("worker", 2500*time.Millisecond, 1, false)), worker, issues.ExitReason{}, false},
		{"other name", exits(line("web", 0, 1, false)), worker, issues.ExitReason{}, false},
		{"no stop time, no spawn line", exits(line("worker", 0, 1, false)), supervisor.ProcessInfo{Name: "worker", Group: "jobs"},
			issues.ExitReason{}, false},
		{"no lines", supervisor.ProcessLog{}, worker, issues.ExitReason{}, false},
		// R26: a stop someone asked for.
		{"stopped on request", exits(stopped("worker", 142*time.Millisecond)), worker, onRequest, true},
		{"crash after a requested stop", exits(stopped("worker", -1*time.Second), line("worker", 900*time.Millisecond, 1, false)), worker,
			issues.ExitReason{Desc: "exit status x", Status: 1}, true},
		{"stop of another process", exits(stopped("web", 0)), worker, issues.ExitReason{}, false},
	}
	for _, c := range cases {
		got, ok := exitReasonFrom(c.log, c.info, procs)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
	twoGroups := append(procs, supervisor.ProcessInfo{Name: "worker", Group: "mail"})
	if got, ok := exitReasonFrom(exits(line("worker", 0, 1, false)), worker, twoGroups); ok {
		t.Errorf("name in two groups: got %+v", got)
	}
}

// After a daemon restart (or an update that re-adds the group) supervisord
// has never stopped the new process, so its stop time is zero. The line
// that ended its previous run is the last exit or stop line for the name
// before the "spawned:" line of its pid, with no other spawn in between.
func TestExitReasonFromAfterDaemonRestart(t *testing.T) {
	boot := time.Date(2026, 10, 31, 17, 1, 32, 0, time.Local)
	ms := func(d int) time.Time { return boot.Add(time.Duration(d) * time.Millisecond) }
	info := supervisor.ProcessInfo{Name: "web", Group: "web", PID: 200, Start: boot}
	procs := []supervisor.ProcessInfo{info}
	spawn := func(at time.Time, pid int) supervisor.SpawnLine {
		return supervisor.SpawnLine{At: at, Name: "web", PID: pid}
	}
	stoppedAt := func(at time.Time) supervisor.ExitLine {
		return supervisor.ExitLine{At: at, Name: "web", Desc: "terminated by SIGTERM", Status: -1, Stopped: true}
	}
	cases := []struct {
		name string
		log  supervisor.ProcessLog
		info supervisor.ProcessInfo
		want issues.ExitReason
		ok   bool
	}{
		{"reload stopped it", supervisor.ProcessLog{
			Exits:  []supervisor.ExitLine{stoppedAt(ms(-500))},
			Spawns: []supervisor.SpawnLine{spawn(ms(-60_000), 100), spawn(ms(158), 200)},
		}, info, issues.ExitReason{Desc: "terminated by SIGTERM", Status: -1, Deliberate: true}, true},
		{"slow stop long before the reload", supervisor.ProcessLog{
			Exits:  []supervisor.ExitLine{stoppedAt(ms(-600_000))},
			Spawns: []supervisor.SpawnLine{spawn(ms(-3_600_000), 100), spawn(ms(158), 200)},
		}, info, issues.ExitReason{Desc: "terminated by SIGTERM", Status: -1, Deliberate: true}, true},
		{"crashed during the reload", supervisor.ProcessLog{
			Exits:  []supervisor.ExitLine{{At: ms(-400), Name: "web", Desc: "exit status 2", Status: 2}},
			Spawns: []supervisor.SpawnLine{spawn(ms(158), 200)},
		}, info, issues.ExitReason{Desc: "exit status 2", Status: 2}, true},
		{"daemon killed: an old stop, then a spawn", supervisor.ProcessLog{
			Exits:  []supervisor.ExitLine{stoppedAt(ms(-60_000))},
			Spawns: []supervisor.SpawnLine{spawn(ms(-59_990), 100), spawn(ms(158), 200)},
		}, info, issues.ExitReason{}, false},
		{"no spawn line for its pid", supervisor.ProcessLog{
			Exits:  []supervisor.ExitLine{stoppedAt(ms(-500))},
			Spawns: []supervisor.SpawnLine{spawn(ms(158), 999)},
		}, info, issues.ExitReason{}, false},
		{"no pid", supervisor.ProcessLog{
			Exits:  []supervisor.ExitLine{stoppedAt(ms(-500))},
			Spawns: []supervisor.SpawnLine{spawn(ms(158), 200)},
		}, supervisor.ProcessInfo{Name: "web", Group: "web", Start: boot}, issues.ExitReason{}, false},
		{"nothing before the spawn", supervisor.ProcessLog{
			Exits:  []supervisor.ExitLine{stoppedAt(ms(5_000))},
			Spawns: []supervisor.SpawnLine{spawn(ms(158), 200)},
		}, info, issues.ExitReason{}, false},
	}
	for _, c := range cases {
		got, ok := exitReasonFrom(c.log, c.info, procs)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}
