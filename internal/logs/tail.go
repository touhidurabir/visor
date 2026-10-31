// Package logs tails a supervised process's stdout or stderr log over
// supervisord's XML-RPC API.
//
// supervisord's tailProcess*Log(name, offset, length) does not read forward
// from offset. It returns the last min(length, size) bytes of the log, so a
// result covers [Offset-len(Bytes), Offset) and can overlap what the caller
// already has. Tailer turns that into a stream of new bytes only.
package logs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/touhidurabir/visor/internal/supervisor"
)

const (
	defaultMaxBuffer = 1 << 20
	firstLength      = 64 << 10
	pollLength       = 256 << 10
)

// NoBuffer, as NewTailer's maxBuffer, keeps no copy of the log: Buffer
// returns "". For a follower that only passes chunks on.
const NoBuffer = -1

// ErrNoLog means the process has no log file for this stream (for example
// stderr_logfile=NONE, or stderr redirected into stdout).
var ErrNoLog = errors.New("this stream has no log file")

// Stream selects which log to tail.
type Stream string

const (
	Stdout Stream = "stdout"
	Stderr Stream = "stderr"
)

// TailClient is the part of *supervisor.Client a Tailer needs.
type TailClient interface {
	TailProcessStdoutLog(ctx context.Context, name string, offset, length int) (supervisor.TailResult, error)
	TailProcessStderrLog(ctx context.Context, name string, offset, length int) (supervisor.TailResult, error)
}

// Chunk is what one Poll found. Text is raw log bytes: a multi-byte character
// written across two polls can be split between chunks (Buffer is always
// whole). A log cleared and regrown past the old offset between two polls
// cannot be detected; that is a limit of the tail RPC.
type Chunk struct {
	Text      string
	Truncated bool // the log was cleared or rotated; Text is the new log
	Skipped   bool // more output arrived than one poll returns; some was lost
}

// Tailer follows one log. Poll must be called from one goroutine at a time;
// Buffer is safe to call concurrently with Poll.
type Tailer struct {
	c       TailClient
	process string
	stream  Stream
	max     int

	known   int // log offset consumed so far
	started bool

	mu  sync.Mutex
	buf []byte
}

// NewTailer tails process's stream. maxBuffer is how many of the newest
// bytes Buffer keeps; zero means 1 MiB and NoBuffer (or any negative)
// nothing.
func NewTailer(c TailClient, process string, s Stream, maxBuffer int) *Tailer {
	if maxBuffer == 0 {
		maxBuffer = defaultMaxBuffer
	}
	return &Tailer{c: c, process: process, stream: s, max: maxBuffer}
}

func (t *Tailer) fetch(ctx context.Context, offset, length int) (supervisor.TailResult, error) {
	return fetch(ctx, t.c, t.process, t.stream, offset, length)
}

// fetch calls the tail RPC for stream s, mapping NO_FILE to ErrNoLog.
func fetch(ctx context.Context, c TailClient, process string, s Stream, offset, length int) (supervisor.TailResult, error) {
	var r supervisor.TailResult
	var err error
	if s == Stderr {
		r, err = c.TailProcessStderrLog(ctx, process, offset, length)
	} else {
		r, err = c.TailProcessStdoutLog(ctx, process, offset, length)
	}
	if errors.Is(err, supervisor.ErrNoFile) {
		return r, ErrNoLog
	}
	return r, err
}

// Tail returns the newest bytes of a log, at most n, starting at a whole
// line: a partial first line is dropped, unless the newest n bytes hold
// no line start at all (one long line), which keeps them. One RPC.
func Tail(ctx context.Context, c TailClient, process string, s Stream, n int) (string, error) {
	// One byte more than asked shows whether the text starts on a line.
	r, err := fetch(ctx, c, process, s, 0, n+1)
	if err != nil {
		return "", err
	}
	text := r.Bytes
	if r.Offset <= len(text) && len(text) <= n { // the whole log
		return text, nil
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 && i+1 < len(text) {
		return text[i+1:], nil
	}
	return trimLeadingPartialRune(text[1:]), nil
}

// Poll returns the output written since the previous Poll. The first call
// returns the last 64 KiB of the log.
func (t *Tailer) Poll(ctx context.Context) (Chunk, error) {
	if !t.started {
		r, err := t.fetch(ctx, 0, firstLength)
		if err != nil {
			return Chunk{}, err
		}
		t.started = true
		t.known = r.Offset
		text := r.Bytes
		if r.Offset > len(text) {
			text = trimLeadingPartialRune(text)
		}
		t.append(text)
		return Chunk{Text: text}, nil
	}

	r, err := t.fetch(ctx, t.known, pollLength)
	if err != nil {
		return Chunk{}, err
	}
	if r.Offset < t.known {
		return t.restart(ctx)
	}
	if r.Offset == t.known {
		return Chunk{}, nil
	}
	start := r.Offset - len(r.Bytes)
	c := Chunk{Skipped: r.Overflow || start > t.known}
	text := r.Bytes
	if start < t.known {
		text = text[t.known-start:]
	} else if c.Skipped && start > 0 {
		text = trimLeadingPartialRune(text)
	}
	t.known = r.Offset
	c.Text = text
	t.append(text)
	return c, nil
}

// restart handles a log that shrank: it reads the new log from the start.
func (t *Tailer) restart(ctx context.Context) (Chunk, error) {
	r, err := t.fetch(ctx, 0, pollLength)
	if err != nil {
		return Chunk{}, err
	}
	t.known = r.Offset
	text := r.Bytes
	skipped := r.Overflow || r.Offset > len(text)
	if skipped {
		text = trimLeadingPartialRune(text)
	}
	t.mu.Lock()
	t.buf = t.buf[:0]
	t.mu.Unlock()
	t.append(text)
	return Chunk{Text: text, Truncated: true, Skipped: skipped}, nil
}

// Buffer returns the newest output seen, at most maxBuffer bytes, starting
// at a line boundary where possible.
func (t *Tailer) Buffer() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

func (t *Tailer) append(text string) {
	if text == "" || t.max < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, text...)
	if len(t.buf) <= t.max {
		return
	}
	cut := len(t.buf) - t.max
	// Prefer starting just after a newline within the kept part.
	// A newline that is the last byte would leave nothing, so it doesn't count.
	if t.buf[cut-1] != '\n' {
		for i := cut; i+1 < len(t.buf); i++ {
			if t.buf[i] == '\n' {
				cut = i + 1
				break
			}
		}
	}
	for cut < len(t.buf) && !utf8.RuneStart(t.buf[cut]) {
		cut++
	}
	t.buf = append(t.buf[:0], t.buf[cut:]...)
}

func trimLeadingPartialRune(s string) string {
	i := 0
	for i < len(s) && i < utf8.UTFMax && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}
