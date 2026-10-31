package supervisor

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestParseExitLines(t *testing.T) {
	zone := time.FixedZone("test", 6*3600)
	at := func(sec, ms int) time.Time {
		return time.Date(2026, 10, 31, 14, 36, sec, ms*int(time.Millisecond), zone)
	}
	text := `5 WARN exited: cut (exit status 9; not expected)
2026-10-31 14:36:32,675 INFO supervisord started with pid 59413
2026-10-31 14:36:34,698 INFO exited: ok (exit status 0; expected)
2026-10-31 14:36:34,699 WARN exited: queue_worker_01 (exit status 3; not expected)
2026-10-31 14:36:34,706 INFO spawned: 'queue_worker_01' with pid 59454
2026-10-31 14:36:34,710 WARN exited: sig (terminated by SIGSEGV; not expected)
2026-10-31 14:36:35,001 WARN exited: dumper (terminated by SIGABRT (core dumped); not expected)
2026-10-31 14:36:35,002 INFO exited: term (terminated by SIGTERM; expected)
2026-10-31 14:36:36,768 WARN stopped: sig (terminated by SIGTERM)
2026-10-31 14:36:36,800 INFO waiting for graceful to stop
2026-10-31 14:36:36,900 INFO stopped: graceful (exit status 0)
2026-10-31 14:36:36,950 WARN stopped: noparen
garbage exited: x (exit status 1; not expected)
2026-10-31 14:36:37,000 WARN exited: broken (exit status 1; maybe)
2026-10-31 14:36:37,100 WARN exited: noreason
2026-10-31 25:99:37,100 WARN exited: badtime (exit status 1; not expected)
2026-10-31 14:36:38,123 WARN exited: last (exit status 2; not expected)`
	want := []ExitLine{
		{At: at(34, 698), Name: "ok", Desc: "exit status 0", Status: 0, Expected: true},
		{At: at(34, 699), Name: "queue_worker_01", Desc: "exit status 3", Status: 3},
		{At: at(34, 710), Name: "sig", Desc: "terminated by SIGSEGV", Status: -1},
		{At: at(35, 1), Name: "dumper", Desc: "terminated by SIGABRT (core dumped)", Status: -1},
		{At: at(35, 2), Name: "term", Desc: "terminated by SIGTERM", Status: -1, Expected: true},
		{At: at(36, 768), Name: "sig", Desc: "terminated by SIGTERM", Status: -1, Stopped: true},
		{At: at(36, 900), Name: "graceful", Desc: "exit status 0", Status: 0, Stopped: true},
		{At: at(38, 123), Name: "last", Desc: "exit status 2", Status: 2},
	}
	got := ParseExitLines(text, zone)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseExitLines:\n got %+v\nwant %+v", got, want)
	}
	if got := ParseExitLines("", zone); len(got) != 0 {
		t.Errorf("empty text: %+v", got)
	}
	// CRLF line ends and a final line without a newline still parse.
	if got := ParseExitLines("2026-10-31 14:36:34,698 INFO exited: ok (exit status 0; expected)\r\n", zone); len(got) != 1 || got[0].Name != "ok" {
		t.Errorf("CRLF: %+v", got)
	}
}

func TestParseSpawnLines(t *testing.T) {
	zone := time.FixedZone("test", 6*3600)
	text := `0 INFO spawned: 'cut' with pid 1
2026-10-31 17:01:29,142 WARN stopped: web (terminated by SIGTERM)
2026-10-31 17:01:29,147 INFO spawned: 'web' with pid 67857
2026-10-31 17:01:30,149 INFO success: web entered RUNNING state, process has stayed up for > than 1 seconds (startsecs)
2026-10-31 17:01:32,158 INFO spawned: 'queue_worker_01' with pid 67902
2026-10-31 17:01:32,200 INFO spawned: 'bad' with pid x
2026-10-31 17:01:32,300 INFO spawnerr: can't find command 'nope'`
	want := []SpawnLine{
		{At: time.Date(2026, 10, 31, 17, 1, 29, 147*int(time.Millisecond), zone), Name: "web", PID: 67857},
		{At: time.Date(2026, 10, 31, 17, 1, 32, 158*int(time.Millisecond), zone), Name: "queue_worker_01", PID: 67902},
	}
	got := ParseProcessLog(text, zone)
	if !reflect.DeepEqual(got.Spawns, want) {
		t.Fatalf("spawns:\n got %+v\nwant %+v", got.Spawns, want)
	}
	if len(got.Exits) != 1 || !got.Exits[0].Stopped || got.Exits[0].Name != "web" {
		t.Errorf("exits = %+v", got.Exits)
	}
}

func TestRecentProcessLogReadsTheTail(t *testing.T) {
	c, rec := newRecorder(t, value("<string>s 1; not expected)\n"+
		"2026-10-31 14:36:34,699 WARN exited: w (exit status 3; not expected)\n"+
		"2026-10-31 14:36:34,705 INFO spawned: 'w' with pid 42\n</string>"))
	got, err := c.RecentProcessLog(context.Background(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Exits) != 1 || got.Exits[0].Name != "w" || got.Exits[0].Status != 3 || got.Exits[0].Expected || got.Exits[0].At.Location() != time.Local {
		t.Errorf("exits = %+v", got.Exits)
	}
	if len(got.Spawns) != 1 || got.Spawns[0].PID != 42 {
		t.Errorf("spawns = %+v", got.Spawns)
	}
	rec.assertCalls(t, "supervisor.readLog(<int>-4096</int>,<int>0</int>)")
}
