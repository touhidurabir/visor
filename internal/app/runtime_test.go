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
	procs := []supervisor.ProcessInfo{worker, {Name: "web", Group: "web"}}
	cases := []struct {
		name  string
		lines []supervisor.ExitLine
		info  supervisor.ProcessInfo
		procs []supervisor.ProcessInfo
		want  issues.ExitReason
		ok    bool
	}{
		{"match", []supervisor.ExitLine{line("worker", 698*time.Millisecond, 1, false)}, worker, procs,
			issues.ExitReason{Desc: "exit status x", Status: 1}, true},
		{"latest of several", []supervisor.ExitLine{
			line("worker", -1500*time.Millisecond, 0, true),
			line("web", 300*time.Millisecond, 9, false),
			line("worker", 900*time.Millisecond, 2, false),
		}, worker, procs, issues.ExitReason{Desc: "exit status x", Status: 2}, true},
		{"earlier within 2s", []supervisor.ExitLine{line("worker", -2*time.Second, 0, true)}, worker, procs,
			issues.ExitReason{Desc: "exit status x", Status: 0, Expected: true}, true},
		{"too old", []supervisor.ExitLine{line("worker", -3*time.Second, 1, false)}, worker, procs, issues.ExitReason{}, false},
		{"too new", []supervisor.ExitLine{line("worker", 2500*time.Millisecond, 1, false)}, worker, procs, issues.ExitReason{}, false},
		{"other name", []supervisor.ExitLine{line("web", 0, 1, false)}, worker, procs, issues.ExitReason{}, false},
		{"no stop time", []supervisor.ExitLine{line("worker", 0, 1, false)}, supervisor.ProcessInfo{Name: "worker", Group: "jobs"}, procs,
			issues.ExitReason{}, false},
		{"name in two groups", []supervisor.ExitLine{line("worker", 0, 1, false)}, worker,
			append(procs, supervisor.ProcessInfo{Name: "worker", Group: "mail"}), issues.ExitReason{}, false},
		{"no lines", nil, worker, procs, issues.ExitReason{}, false},
	}
	for _, c := range cases {
		got, ok := exitReasonFrom(c.lines, c.info, c.procs)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}
