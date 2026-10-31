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
	if b := tr.Buffer(); !utf8.ValidString(b) || len(b) > 8 {
		t.Errorf("Buffer = %q", b)
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
