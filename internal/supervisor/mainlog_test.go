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

func TestRecentExitsReadsTheTail(t *testing.T) {
	c, rec := newRecorder(t, value("<string>s 1; not expected)\n2026-10-31 14:36:34,699 WARN exited: w (exit status 3; not expected)\n</string>"))
	got, err := c.RecentExits(context.Background(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "w" || got[0].Status != 3 || got[0].Expected || got[0].At.Location() != time.Local {
		t.Errorf("RecentExits = %+v", got)
	}
	rec.assertCalls(t, "supervisor.readLog(<int>-4096</int>,<int>0</int>)")
}
