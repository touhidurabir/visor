package logs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// fakeLog mimics supervisord's tailFile over an in-memory log.
type fakeLog struct {
	data    []byte
	err     error
	calls   int
	lastReq [2]int // offset, length of the last call
}

func (f *fakeLog) tail(offset, length int) (supervisor.TailResult, error) {
	f.calls++
	f.lastReq = [2]int{offset, length}
	if f.err != nil {
		return supervisor.TailResult{}, f.err
	}
	sz := len(f.data)
	overflow := false
	if sz > offset+length {
		overflow = true
		offset = sz - 1
	}
	if offset+length > sz {
		if offset > sz-1 {
			length = 0
		}
		offset = sz - length
	}
	if offset < 0 {
		offset = 0
	}
	length = min(length, sz-offset) // python's read stops at EOF
	var b string
	if length > 0 {
		b = string(f.data[offset : offset+length])
	}
	return supervisor.TailResult{Bytes: b, Offset: sz, Overflow: overflow}, nil
}

func (f *fakeLog) TailProcessStdoutLog(_ context.Context, _ string, offset, length int) (supervisor.TailResult, error) {
	return f.tail(offset, length)
}

func (f *fakeLog) TailProcessStderrLog(_ context.Context, _ string, offset, length int) (supervisor.TailResult, error) {
	return f.tail(offset, length)
}

func (f *fakeLog) write(s string) { f.data = append(f.data, s...) }

func poll(t *testing.T, tl *Tailer) Chunk {
	t.Helper()
	c, err := tl.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTailDedupesOverlap(t *testing.T) {
	f := &fakeLog{}
	f.write("one\ntwo\n")
	tl := NewTailer(f, "p", Stdout, 0)
	if c := poll(t, tl); c.Text != "one\ntwo\n" || c.Truncated || c.Skipped {
		t.Fatalf("first = %+v", c)
	}
	if f.lastReq != [2]int{0, 64 << 10} {
		t.Errorf("first request = %v, want [0 65536]", f.lastReq)
	}
	// The server returns up to 256 KiB ending at the log size, overlapping
	// what we already have; only the new bytes must come back.
	f.write("three\n")
	if c := poll(t, tl); c.Text != "three\n" || c.Skipped {
		t.Fatalf("second = %+v", c)
	}
	if f.lastReq != [2]int{8, 256 << 10} {
		t.Errorf("second request = %v, want [8 262144]", f.lastReq)
	}
	if c := poll(t, tl); c.Text != "" {
		t.Errorf("idle poll = %+v", c)
	}
	if got := tl.Buffer(); got != "one\ntwo\nthree\n" {
		t.Errorf("Buffer = %q", got)
	}
}

func TestTailDetectsTruncation(t *testing.T) {
	f := &fakeLog{}
	f.write("old line 1\nold line 2\n")
	tl := NewTailer(f, "p", Stderr, 0)
	poll(t, tl)

	f.data = []byte("new\n") // cleared, then wrote a little
	c := poll(t, tl)
	if !c.Truncated || c.Text != "new\n" {
		t.Fatalf("after truncation = %+v", c)
	}
	if got := tl.Buffer(); got != "new\n" {
		t.Errorf("Buffer = %q, want only the new log", got)
	}
	f.write("next\n")
	if c := poll(t, tl); c.Truncated || c.Text != "next\n" {
		t.Errorf("continue = %+v", c)
	}

	f.data = nil // cleared and still empty
	c = poll(t, tl)
	if !c.Truncated || c.Text != "" || tl.Buffer() != "" {
		t.Errorf("empty after clear = %+v, buffer %q", c, tl.Buffer())
	}
}

func TestTailOverflowSkipped(t *testing.T) {
	f := &fakeLog{}
	f.write("start\n")
	tl := NewTailer(f, "p", Stdout, 0)
	poll(t, tl)

	f.write(strings.Repeat("x", 300<<10) + "\nend\n")
	c := poll(t, tl)
	if !c.Skipped || c.Truncated {
		t.Fatalf("chunk = %+v, want Skipped only", c)
	}
	if len(c.Text) != 256<<10 || !strings.HasSuffix(c.Text, "\nend\n") {
		t.Errorf("text len %d, want the last 256 KiB", len(c.Text))
	}
}

func TestTailFirstPollIsNotSkipped(t *testing.T) {
	f := &fakeLog{}
	f.write(strings.Repeat("y", 100<<10))
	c := poll(t, NewTailer(f, "p", Stdout, 0))
	if c.Skipped || len(c.Text) != 64<<10 {
		t.Errorf("first poll: skipped=%v len=%d, want last 64 KiB", c.Skipped, len(c.Text))
	}
}

func TestTailBufferCap(t *testing.T) {
	f := &fakeLog{}
	tl := NewTailer(f, "p", Stdout, 20)
	for _, l := range []string{"aaaa\n", "bbbb\n", "cccc\n", "dddd\n", "eeee\n"} {
		f.write(l)
		poll(t, tl)
	}
	got := tl.Buffer()
	if len(got) > 20 || !strings.HasSuffix(got, "eeee\n") {
		t.Fatalf("Buffer = %q", got)
	}
	if !strings.HasPrefix(got, "bbbb") && !strings.HasPrefix(got, "cccc") {
		t.Errorf("buffer starts mid-line: %q", got)
	}

	// Never starts mid-rune.
	g := &fakeLog{}
	tr := NewTailer(g, "p", Stdout, 8)
	g.write("héllo wörld héllo wörld\n")
	poll(t, tr)
	if b := tr.Buffer(); b == "" || !utf8.ValidString(b) || len(b) > 8 || !strings.HasSuffix("héllo wörld héllo wörld\n", b) {
		t.Errorf("Buffer = %q", b)
	}

	// A single line longer than the cap keeps its newest bytes.
	h := &fakeLog{}
	th := NewTailer(h, "p", Stdout, 10)
	h.write(strings.Repeat("z", 50) + "\n")
	poll(t, th)
	if b := th.Buffer(); b != strings.Repeat("z", 9)+"\n" {
		t.Errorf("long line: Buffer = %q", b)
	}
}

func TestTailTruncationOverflowSkipped(t *testing.T) {
	f := &fakeLog{}
	f.write(strings.Repeat("o", 400<<10))
	tl := NewTailer(f, "p", Stdout, 0)
	poll(t, tl)
	f.data = []byte(strings.Repeat("n", 300<<10))
	if c := poll(t, tl); !c.Truncated || !c.Skipped {
		t.Errorf("chunk = Truncated %v Skipped %v, want both", c.Truncated, c.Skipped)
	}
}

func TestTailNoFile(t *testing.T) {
	f := &fakeLog{err: supervisor.ErrNoFile}
	_, err := NewTailer(f, "p", Stderr, 0).Poll(context.Background())
	if !errors.Is(err, ErrNoLog) {
		t.Errorf("err = %v, want ErrNoLog", err)
	}
	f.err = supervisor.ErrBadName
	if _, err := NewTailer(f, "p", Stdout, 0).Poll(context.Background()); errors.Is(err, ErrNoLog) || !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("other faults must pass through, got %v", err)
	}
}

func TestTailUsesStreamMethod(t *testing.T) {
	out, errc := &countClient{}, &countClient{}
	c := &splitClient{out: out, err: errc}
	_, _ = NewTailer(c, "p", Stderr, 0).Poll(context.Background())
	if out.n != 0 || errc.n != 1 {
		t.Errorf("stdout calls %d, stderr calls %d", out.n, errc.n)
	}
}

type countClient struct{ n int }
type splitClient struct{ out, err *countClient }

func (s *splitClient) TailProcessStdoutLog(context.Context, string, int, int) (supervisor.TailResult, error) {
	s.out.n++
	return supervisor.TailResult{}, nil
}

func (s *splitClient) TailProcessStderrLog(context.Context, string, int, int) (supervisor.TailResult, error) {
	s.err.n++
	return supervisor.TailResult{}, nil
}

// streamFake serves different stdout and stderr logs.
type streamFake struct{ out, err fakeLog }

func (s *streamFake) TailProcessStdoutLog(_ context.Context, _ string, offset, length int) (supervisor.TailResult, error) {
	return s.out.tail(offset, length)
}

func (s *streamFake) TailProcessStderrLog(_ context.Context, _ string, offset, length int) (supervisor.TailResult, error) {
	return s.err.tail(offset, length)
}

func TestTailNewest(t *testing.T) {
	f := &streamFake{}
	f.out.write("one\ntwo\nthree\n")
	f.err.write("boom\n")
	ctx := context.Background()

	for _, c := range []struct {
		s    Stream
		n    int
		want string
	}{
		{Stdout, 1 << 10, "one\ntwo\nthree\n"}, // the whole log
		{Stderr, 1 << 10, "boom\n"},            // the stream picks the RPC
		{Stdout, 9, "three\n"},                 // "o\nthree\n" starts mid-line: drop to the next line
		{Stdout, 6, "three\n"},                 // starts exactly on a line
		{Stdout, 13, "two\nthree\n"},           // one byte short of the whole log
	} {
		got, err := Tail(ctx, f, "p", c.s, c.n)
		if err != nil || got != c.want {
			t.Errorf("Tail(%s, %d) = %q, %v; want %q", c.s, c.n, got, err, c.want)
		}
	}

	// One long line with no newline in reach: keep its end rather than nothing.
	g := &streamFake{}
	g.out.write("abcdefghij")
	if got, _ := Tail(ctx, g, "p", Stdout, 4); got != "ghij" {
		t.Errorf("long line = %q, want its newest bytes", got)
	}

	f.out.err = supervisor.ErrNoFile
	if _, err := Tail(ctx, f, "p", Stdout, 10); !errors.Is(err, ErrNoLog) {
		t.Errorf("NO_FILE = %v, want ErrNoLog", err)
	}
}

// M7: a follower that never calls Buffer keeps no copy of the log.
func TestNoBufferKeepsNothing(t *testing.T) {
	f := &fakeLog{}
	f.write(strings.Repeat("x", 1000) + "\n")
	tl := NewTailer(f, "p", Stdout, NoBuffer)
	if c := poll(t, tl); len(c.Text) != 1001 {
		t.Fatalf("first = %d bytes", len(c.Text))
	}
	f.write("more\n")
	if c := poll(t, tl); c.Text != "more\n" {
		t.Fatalf("second = %+v", c)
	}
	f.data = []byte("new\n") // cleared
	if c := poll(t, tl); !c.Truncated || c.Text != "new\n" {
		t.Fatalf("after clear = %+v", c)
	}
	if b := tl.Buffer(); b != "" || cap(tl.buf) != 0 {
		t.Errorf("Buffer = %q, cap %d; want nothing kept", b, cap(tl.buf))
	}
}
