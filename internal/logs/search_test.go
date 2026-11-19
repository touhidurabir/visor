package logs

import (
	"context"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// readLog adds supervisord's read RPC to fakeLog: a read that isn't whole
// UTF-8 fails with HTTP 500, as supervisord's strict decode does.
type readLog struct{ fakeLog }

func (f *readLog) read(offset, length int) (string, error) {
	f.calls++
	b := f.data[offset:min(offset+length, len(f.data))]
	if !utf8.Valid(b) {
		return "", supervisor.ErrInternal
	}
	return string(b), nil
}

func (f *readLog) ReadProcessStdoutLog(_ context.Context, _ string, offset, length int) (string, error) {
	return f.read(offset, length)
}

func (f *readLog) ReadProcessStderrLog(_ context.Context, _ string, offset, length int) (string, error) {
	return f.read(offset, length)
}

func searchFake(t *testing.T, f *readLog, query string, step, limit int) []string {
	t.Helper()
	var got []string
	before := -1
	for range 10000 {
		p, err := SearchBack(context.Background(), f, "p", Stdout, query, before, step, limit-len(got))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p.Lines...)
		if p.Next == 0 || p.Capped {
			return got
		}
		before = p.Next
	}
	t.Fatal("search did not reach the start of the log")
	return nil
}

func TestSearchBack(t *testing.T) {
	f := &readLog{}
	f.write("one ✓ match\ntwo\nthree ✓ MATCH\r\nfour\n" + strings.Repeat("x", 50) + " match long\nlast match")
	want := []string{"last match", strings.Repeat("x", 50) + " match long", "three ✓ MATCH", "one ✓ match"}
	for step := 3; step <= 100; step++ {
		got := searchFake(t, f, "Match", step, 100) // reaches the start, whatever the step
		if step > 62 && !slices.Equal(got, want) {
			t.Fatalf("step %d: got %q, want %q", step, got, want)
		}
		// A line longer than a step is searched in pieces (a match across two
		// pieces is missed); shorter lines are found whole.
		if step > 16 && (!slices.Contains(got, "last match") || !slices.Contains(got, "three ✓ MATCH") || !slices.Contains(got, "one ✓ match")) {
			t.Fatalf("step %d: got %q", step, got)
		}
	}
	if got := searchFake(t, f, "match", 100, 2); !slices.Equal(got, want[:2]) {
		t.Errorf("limit 2: got %q, want the newest two", got)
	}
}

func TestSearchBackNotText(t *testing.T) {
	f := &readLog{}
	f.write("ok\n\xff bad\n")
	if _, err := SearchBack(context.Background(), f, "p", Stdout, "ok", -1, 100, 10); err != ErrNotText {
		t.Fatalf("err = %v, want ErrNotText", err)
	}
}
