package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/logs"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

func (r *recorder) logChunks(id string) []LogChunkView {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []LogChunkView
	for _, e := range r.events {
		if c, ok := e.data.(LogChunkView); ok && e.name == EventLog && (id == "" || c.ID == id) {
			out = append(out, c)
		}
	}
	return out
}

func waitChunks(t *testing.T, rec *recorder, id string, ok func([]LogChunkView) bool) []LogChunkView {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		cs := rec.logChunks(id)
		if ok(cs) {
			return cs
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for log chunks of %s; got %d", id, len(cs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func joined(cs []LogChunkView) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.Text)
	}
	return b.String()
}

func newLogService(t *testing.T, rt *Runtime) (*LogService, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := NewLogService(rt, rec.emit)
	s.interval = 100 * time.Millisecond
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	return s, rec
}

func TestLogServiceFollowEmits(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Spammer)
	inst.WaitFor(t, "spammer:spammer", supervisor.StateRunning)
	time.Sleep(200 * time.Millisecond) // some output before the first poll
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), time.Minute)
	s, rec := newLogService(t, rt)

	id, err := s.Follow("spammer:spammer", "stdout")
	if err != nil {
		t.Fatal(err)
	}
	first := waitChunks(t, rec, id, func(cs []LogChunkView) bool { return len(cs) > 0 })[0]
	if !strings.HasPrefix(first.Text, "line 1\n") {
		t.Errorf("first chunk starts %q, want the whole log so far", first.Text[:min(len(first.Text), 20)])
	}
	// Later chunks carry new lines only: the stream stays gap- and
	// duplicate-free.
	cs := waitChunks(t, rec, id, func(cs []LogChunkView) bool { return len(cs) >= 3 })
	lines := strings.Split(strings.TrimSuffix(joined(cs), "\n"), "\n")
	for i, l := range lines[:len(lines)-1] { // the last line may be partial
		if want := "line " + strconv.Itoa(i+1); l != want {
			t.Fatalf("line %d = %q, want %q", i, l, want)
		}
	}

	s.Unfollow(id)
	n := len(rec.logChunks(id))
	time.Sleep(300 * time.Millisecond)
	if m := len(rec.logChunks(id)); m != n {
		t.Errorf("%d chunks after Unfollow", m-n)
	}
	s.Unfollow(id) // twice is fine
	s.Unfollow("no-such-id")
}

func TestLogServiceReplacesFollowOfSameStream(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Spammer)
	inst.WaitFor(t, "spammer:spammer", supervisor.StateRunning)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), time.Minute)
	s, rec := newLogService(t, rt)

	a, err := s.Follow("spammer:spammer", "stdout")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Follow("spammer:spammer", "stdout")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("same id for two follows")
	}
	if got := s.active(); got != 1 {
		t.Errorf("%d follows active, want 1 (the second replaces the first)", got)
	}
	waitChunks(t, rec, b, func(cs []LogChunkView) bool { return len(cs) >= 2 })
	n := len(rec.logChunks(a))
	time.Sleep(300 * time.Millisecond)
	if m := len(rec.logChunks(a)); m != n {
		t.Errorf("replaced follow still emitting (%d new chunks)", m-n)
	}
	if _, err := s.Follow("spammer:spammer", "stderr"); err != nil {
		t.Fatal(err)
	}
	if got := s.active(); got != 2 {
		t.Errorf("%d follows active, want 2 (stdout and stderr)", got)
	}

	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if got := s.active(); got != 0 {
		t.Errorf("%d follows after shutdown", got)
	}
	total := len(rec.logChunks(""))
	time.Sleep(300 * time.Millisecond)
	if m := len(rec.logChunks("")); m != total {
		t.Errorf("%d chunks emitted after shutdown", m-total)
	}
	if _, err := s.Follow("spammer:spammer", "stdout"); err == nil {
		t.Error("Follow after shutdown: want error")
	}
}

// R23: supervisord returns empty data, not NO_FILE, for a stream without a
// log, so Visor decides from the config.
func TestLogServiceNoLogStreams(t *testing.T) {
	quiet := supervisortest.Program{Name: "quiet", Command: "sleep 3600",
		Extra: map[string]string{"stdout_logfile": "NONE", "stderr_logfile": "none"}}
	merged := supervisortest.Program{Name: "merged", Command: "sleep 3600",
		Extra: map[string]string{"redirect_stderr": "true"}}
	inst := supervisortest.Start(t, quiet, merged)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), time.Minute)
	s, _ := newLogService(t, rt)

	for _, c := range []struct{ proc, stream, msg string }{
		{"quiet:quiet", "stdout", "no log file"},
		{"quiet:quiet", "stderr", "no log file"},
		{"merged:merged", "stderr", "stderr is sent to the stdout log"},
	} {
		_, err := s.Follow(c.proc, c.stream)
		if !errors.Is(err, logs.ErrNoLog) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("Follow(%s, %s) = %v, want ErrNoLog saying %q", c.proc, c.stream, err, c.msg)
		}
	}
	if _, err := s.Follow("merged:merged", "stdout"); err != nil {
		t.Errorf("merged stdout has a log: %v", err)
	}
	if _, err := s.Follow("merged:merged", "nope"); err == nil {
		t.Error("unknown stream: want error")
	}
	if _, err := s.Save("quiet:quiet", "stdout"); !errors.Is(err, logs.ErrNoLog) {
		t.Errorf("Save of a stream with no log = %v, want ErrNoLog", err)
	}
}

func TestLogServiceWithoutConnection(t *testing.T) {
	rt, _ := startRuntime(t, filepath.Join(t.TempDir(), "missing.conf"), t.TempDir(), time.Minute)
	s, _ := newLogService(t, rt)
	if _, err := s.Follow("a:a", "stdout"); err == nil {
		t.Error("Follow without a connection: want error")
	}
}

func TestSaveLogText(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Spammer)
	inst.WaitFor(t, "spammer:spammer", supervisor.StateRunning)
	time.Sleep(200 * time.Millisecond)
	dir := t.TempDir()
	out := filepath.Join(dir, "spammer.log")
	if err := os.WriteFile(out, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveLog(t.Context(), inst.Client(), "spammer:spammer", logs.Stdout, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "line 1\nline 2\n") {
		t.Errorf("saved %q…, want the log from its start", b[:min(len(b), 20)])
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
	if err := writeLogFile(filepath.Join(dir, "no-such-dir", "x.log"), "x"); err == nil {
		t.Error("write into a missing folder: want error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left %d files in %s, want only the saved log", len(entries), dir)
	}
}

// flakyTail is a TailClient whose log is "hello\n" until fail is set.
type flakyTail struct {
	mu    sync.Mutex
	fail  bool
	calls int
}

func (f *flakyTail) tail() (supervisor.TailResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return supervisor.TailResult{}, errors.New("supervisord: unreachable: connection refused")
	}
	return supervisor.TailResult{Bytes: "hello\n", Offset: 6}, nil
}

func (f *flakyTail) TailProcessStdoutLog(context.Context, string, int, int) (supervisor.TailResult, error) {
	return f.tail()
}

func (f *flakyTail) TailProcessStderrLog(context.Context, string, int, int) (supervisor.TailResult, error) {
	return f.tail()
}

func TestLogServiceStopsAfterRepeatedPollFailures(t *testing.T) {
	rec := &recorder{}
	s := NewLogService(nil, rec.emit)
	s.interval = 10 * time.Millisecond
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	f := &flakyTail{}

	id, err := s.start(t.Context(), f, "a:a", logs.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	waitChunks(t, rec, id, func(cs []LogChunkView) bool { return len(cs) == 1 && cs[0].Text == "hello\n" })
	f.mu.Lock()
	f.fail = true
	start := f.calls
	f.mu.Unlock()

	cs := waitChunks(t, rec, id, func(cs []LogChunkView) bool { return len(cs) == 2 })
	if last := cs[1]; last.Error == "" || !strings.Contains(last.Error, "unreachable") || last.Text != "" {
		t.Errorf("last event = %+v, want the poll error", last)
	}
	f.mu.Lock()
	polls := f.calls - start
	f.mu.Unlock()
	if polls != maxPollFailures {
		t.Errorf("gave up after %d failed polls, want %d", polls, maxPollFailures)
	}
	if n := s.active(); n != 0 {
		t.Errorf("%d follows still active after giving up", n)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(rec.logChunks(id)); n != 2 {
		t.Errorf("%d events after giving up, want none", n-2)
	}
	s.Unfollow(id) // already gone: a no-op
}

// A blip shorter than maxPollFailures polls keeps the follow alive.
func TestLogServiceSurvivesShortPollFailures(t *testing.T) {
	rec := &recorder{}
	s := NewLogService(nil, rec.emit)
	s.interval = 10 * time.Millisecond
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	f := &flakyTail{}
	id, err := s.start(t.Context(), f, "a:a", logs.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.fail = true
	start := f.calls
	f.mu.Unlock()
	for {
		f.mu.Lock()
		n := f.calls - start
		if n >= maxPollFailures-1 {
			f.fail = false
		}
		f.mu.Unlock()
		if n >= maxPollFailures-1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if s.active() != 1 {
		t.Error("follow stopped after a short blip")
	}
	for _, c := range rec.logChunks(id) {
		if c.Error != "" {
			t.Errorf("error event after a short blip: %+v", c)
		}
	}
}
