package supervisor

import (
	"context"
	"fmt"
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

func TestParseMainLogLines(t *testing.T) {
	zone := time.FixedZone("test", 6*3600)
	at := func(sec, ms int) time.Time {
		return time.Date(2026, 10, 31, 14, 36, sec, ms*int(time.Millisecond), zone)
	}
	text := `ial tail of a line cut by the read offset
and its second half
2026-10-31 14:36:32,675 INFO supervisord started with pid 59413
2026-10-31 14:36:34,699 WARN exited: queue_worker_01 (exit status 3; not expected)
2026-10-31 14:36:35,001 CRIT Server 'inet_http_server' running without any HTTP authentication checking
2026-10-31 14:36:36,000 ERRO pool stopped
Traceback (most recent call last):
  File "supervisor/process.py", line 1, in spawn
OSError: boom
2026-10-31 14:36:37,100 DEBG 'x' stdout output:
hello
2026-10-31 14:36:38,200 INFO spawned: 'queue_worker_01' with pid 59454
`
	want := []MainLogLine{
		{at(32, 675), "INFO", "supervisord started with pid 59413"},
		{at(34, 699), "WARN", "exited: queue_worker_01 (exit status 3; not expected)"},
		{at(35, 1), "CRIT", "Server 'inet_http_server' running without any HTTP authentication checking"},
		{at(36, 0), "ERRO", "pool stopped\nTraceback (most recent call last):\n  File \"supervisor/process.py\", line 1, in spawn\nOSError: boom"},
		{at(37, 100), "DEBG", "'x' stdout output:\nhello"},
		{at(38, 200), "INFO", "spawned: 'queue_worker_01' with pid 59454"},
	}
	got := ParseMainLogLines(text, zone)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if got := ParseMainLogLines("only a partial\nline\n", zone); len(got) != 0 {
		t.Errorf("no timestamp: got %+v, want none", got)
	}
}

// fakeLog serves readLog the way supervisord's readFile does, and records
// how many bytes each read asked for.
type fakeLog struct {
	data  string
	asked []int
}

func (f *fakeLog) read(offset, length int) (string, error) {
	f.asked = append(f.asked, offset)
	if offset < 0 {
		pos := max(len(f.data)+offset, 0)
		return f.data[pos:], nil
	}
	if offset > len(f.data) {
		return "", nil
	}
	end := len(f.data)
	if length > 0 {
		end = min(offset+length, end)
	}
	return f.data[offset:end], nil
}

func logLine(i int) string {
	return fmt.Sprintf("2026-11-01 10:00:%02d,%03d INFO line %d\n", i%60, i%1000, i)
}

func TestReadMainLogFollowsWithoutRereading(t *testing.T) {
	f := &fakeLog{}
	for i := range 50 {
		f.data += logLine(i)
	}
	first, err := readMainLog(f.read, "", 1024, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if first.Since || first.Text != f.data || first.Cursor == "" || first.Truncated {
		t.Fatalf("first read = %+v", first)
	}

	// Nothing new: an empty Since read that keeps the cursor working.
	same, _ := readMainLog(f.read, first.Cursor, 1024, 1<<20)
	if !same.Since || same.Text != "" {
		t.Fatalf("no news = %+v", same)
	}

	// Two new lines and half of a third: only the whole new lines, and only
	// the follow window was read.
	f.data += logLine(50) + logLine(51) + "2026-11-01 10:00:52,052 INFO li"
	f.asked = nil
	next, _ := readMainLog(f.read, same.Cursor, 1024, 1<<20)
	if !next.Since || next.Text != logLine(50)+logLine(51) {
		t.Fatalf("follow = %+v", next)
	}
	if len(f.asked) != 1 || f.asked[0] != -1024 {
		t.Errorf("reads = %v, want just the 1024-byte window", f.asked)
	}
	// The rest of the half line arrives.
	f.data += "ne 52\n"
	last, _ := readMainLog(f.read, next.Cursor, 1024, 1<<20)
	if !last.Since || last.Text != logLine(52) {
		t.Fatalf("finished line = %+v", last)
	}
}

func TestReadMainLogFallsBackToFullRead(t *testing.T) {
	f := &fakeLog{}
	for i := range 10 {
		f.data += logLine(i)
	}
	first, _ := readMainLog(f.read, "", 256, 1<<20)

	// More was written than the follow window holds: a full read.
	for i := 10; i < 40; i++ {
		f.data += logLine(i)
	}
	f.asked = nil
	got, _ := readMainLog(f.read, first.Cursor, 256, 1<<20)
	if got.Since || got.Text != f.data {
		t.Fatalf("after a burst = %+v", got)
	}
	if len(f.asked) != 2 || f.asked[1] != -(1<<20) {
		t.Errorf("reads = %v, want the window then a full read", f.asked)
	}

	// Rotated: the new file does not hold the cursor.
	f.data = logLine(100)
	got, _ = readMainLog(f.read, got.Cursor, 256, 1<<20)
	if got.Since || got.Text != logLine(100) {
		t.Fatalf("after rotation = %+v", got)
	}

	// A full read that fills maxBytes is truncated, and starts on a whole line
	// for the parser to find (it skips the partial first one itself).
	for i := range 100 {
		f.data += logLine(i)
	}
	got, _ = readMainLog(f.read, "", 256, 512)
	if got.Since || !got.Truncated || len(got.Text) > 512 {
		t.Fatalf("truncated read = %+v", got)
	}
}
