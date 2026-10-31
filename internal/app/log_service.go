package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/logs"
)

// EventLog carries a LogChunkView: new output of one followed log.
const EventLog = "visor:log"

func init() {
	application.RegisterEvent[LogChunkView](EventLog)
}

const (
	// followInterval is how often a followed log is polled.
	followInterval = time.Second
	// maxFollows caps concurrent follows; a new one beyond it stops the
	// oldest. One per stream of a process is kept anyway (a new follow of
	// the same stream replaces the old one).
	maxFollows = 8
	// followBuffer is how much of a followed log the tailer keeps.
	followBuffer = 1 << 20
	// saveBytes is how much of a log Save writes: its newest bytes.
	saveBytes = 4 << 20
	// maxPollFailures is how many polls in a row may fail before a follow
	// gives up and says so.
	maxPollFailures = 5
)

// errReconnected ends a follow whose connection was rebuilt.
const errReconnected = "Stopped following because Visor reconnected to supervisord. Retry to follow the log again."

// LogChunkView is new output from one follow (ID from Follow). The first
// chunk is the last 64 KiB of the log. Truncated means the log was cleared
// or rotated and Text starts the new log; Skipped means some output was
// written too fast to read and is missing before Text. Error is set on a
// follow's last event when it stopped because polls kept failing; the
// frontend can Follow again.
type LogChunkView struct {
	ID        string
	Text      string
	Truncated bool
	Skipped   bool
	Error     string
}

// LogService follows process logs for the frontend: one goroutine per
// follow, polling every second and emitting visor:log events.
type LogService struct {
	rt       *Runtime
	emit     func(name string, data any)
	interval time.Duration

	mu      sync.Mutex
	closed  bool
	next    int
	follows []*follow // oldest first
	wg      sync.WaitGroup
}

type follow struct {
	id, process string
	stream      logs.Stream
	cancel      context.CancelFunc
	done        chan struct{}
}

// NewLogService returns a LogService backed by rt that sends events with
// emit (WailsEmit in the app; nil drops them).
func NewLogService(rt *Runtime, emit func(name string, data any)) *LogService {
	if emit == nil {
		emit = func(string, any) {}
	}
	return &LogService{rt: rt, emit: emit, interval: followInterval}
}

// ServiceShutdown stops every follow and waits for them to end. Follow
// fails afterwards.
func (s *LogService) ServiceShutdown() error {
	s.mu.Lock()
	s.closed = true
	fs := s.follows
	s.follows = nil
	s.mu.Unlock()
	for _, f := range fs {
		f.cancel()
	}
	s.wg.Wait()
	return nil
}

func parseStream(stream string) (logs.Stream, error) {
	switch s := logs.Stream(stream); s {
	case logs.Stdout, logs.Stderr:
		return s, nil
	}
	return "", fmt.Errorf("unknown log stream %q: use stdout or stderr", stream)
}

// checkStream rejects a stream with no log file (from the config, R23).
func (s *LogService) checkStream(process, stream string) (logs.Stream, error) {
	st, err := parseStream(stream)
	if err != nil {
		return "", err
	}
	if prog, ok := s.rt.program(process); ok {
		if err := noLog(prog, st); err != nil {
			return "", err
		}
	}
	return st, nil
}

// Follow starts following a process's stdout or stderr log and returns the
// follow's ID. Every second, new output is sent as a visor:log event
// carrying that ID; the first event has the last 64 KiB of the log. A new
// Follow of the same stream replaces the old one. A stream with no log file
// fails with an error wrapping logs.ErrNoLog.
func (s *LogService) Follow(process, stream string) (string, error) {
	st, err := s.checkStream(process, stream)
	if err != nil {
		return "", err
	}
	ac, err := s.rt.conn()
	if err != nil {
		return "", err
	}
	return s.start(ac.ctx, ac.client, process, st)
}

// start reads the first chunk of a log, registers the follow and starts
// its poller. ctx bounds the follow's life (the Runtime's context).
func (s *LogService) start(ctx context.Context, c logs.TailClient, process string, st logs.Stream) (string, error) {
	t := logs.NewTailer(c, process, st, followBuffer)
	pctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	first, err := t.Poll(pctx)
	cancel()
	if err != nil {
		return "", wrap("follow the "+string(st)+" log of "+process, err)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", errors.New("cannot follow logs: the app is quitting")
	}
	s.next++
	f := &follow{id: "log-" + strconv.Itoa(s.next), process: process, stream: st, done: make(chan struct{})}
	var stop []*follow
	keep := s.follows[:0]
	for _, o := range s.follows {
		if o.process == process && o.stream == st {
			stop = append(stop, o)
		} else {
			keep = append(keep, o)
		}
	}
	if len(keep) >= maxFollows {
		stop = append(stop, keep[0])
		keep = keep[1:]
	}
	var fctx context.Context
	fctx, f.cancel = context.WithCancel(ctx)
	s.follows = append(keep, f)
	s.wg.Add(1)
	s.mu.Unlock()

	for _, o := range stop {
		o.cancel()
		<-o.done
	}
	go s.run(fctx, f, t, first)
	return f.id, nil
}

// Unfollow stops a follow. No event for it is sent after Unfollow returns.
// Unknown or already stopped IDs are ignored.
func (s *LogService) Unfollow(id string) {
	s.mu.Lock()
	var f *follow
	for i, o := range s.follows {
		if o.id == id {
			f = o
			s.follows = append(s.follows[:i], s.follows[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	if f != nil {
		f.cancel()
		<-f.done
	}
}

// active counts running follows.
func (s *LogService) active() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.follows)
}

func (s *LogService) run(ctx context.Context, f *follow, t *logs.Tailer, first logs.Chunk) {
	defer s.wg.Done()
	defer close(f.done)
	defer f.cancel()
	send := func(c LogChunkView) {
		if ctx.Err() == nil {
			c.ID = f.id
			s.emit(EventLog, c)
		}
	}
	sendChunk := func(c logs.Chunk) {
		if c.Text != "" || c.Truncated {
			send(LogChunkView{Text: c.Text, Truncated: c.Truncated, Skipped: c.Skipped})
		}
	}
	sendChunk(first)
	tick := time.NewTicker(s.interval)
	defer tick.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			// Still registered: neither Unfollow, a replacing Follow nor
			// shutdown stopped it, so the Runtime's connection ended
			// (Reconnect or a settings change). Say so; the frontend
			// offers Retry, which follows over the new connection.
			if s.forget(f) {
				s.emit(EventLog, LogChunkView{ID: f.id, Error: errReconnected})
			}
			return
		case <-tick.C:
		}
		pctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		c, err := t.Poll(pctx)
		cancel()
		if err == nil {
			failures = 0
			sendChunk(c)
			continue
		}
		// A blip (supervisord restarting) is retried; a log that stays
		// unreadable ends the follow, and the frontend offers Retry.
		if failures++; failures < maxPollFailures {
			continue
		}
		s.forget(f)
		send(LogChunkView{Error: wrap("follow the "+string(f.stream)+" log of "+f.process, err).Error()})
		return
	}
}

// forget drops f from the running follows (it is stopping by itself) and
// reports whether it was still registered.
func (s *LogService) forget(f *follow) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := slices.Index(s.follows, f); i >= 0 {
		s.follows = slices.Delete(s.follows, i, i+1)
		return true
	}
	return false
}

// Save asks where to save a process's stdout or stderr log (the newest
// 4 MiB) and writes it there. It returns the saved path, or "" when the
// person cancels.
func (s *LogService) Save(process, stream string) (string, error) {
	st, err := s.checkStream(process, stream)
	if err != nil {
		return "", err
	}
	ac, err := s.rt.conn()
	if err != nil {
		return "", err
	}
	a := application.Get()
	if a == nil {
		return "", errors.New("cannot show the save dialog: Visor is not running as an app")
	}
	name := strings.NewReplacer(":", "-", "/", "-").Replace(process) + "-" + stream + ".log"
	path, err := a.Dialog.SaveFile().
		SetFilename(name).
		CanCreateDirectories(true).
		AttachToWindow(a.Window.Current()).
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("cannot show the save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ac.ctx, rpcTimeout)
	defer cancel()
	if err := saveLog(ctx, ac.client, process, st, path); err != nil {
		return "", err
	}
	return path, nil
}

// saveLog writes the newest saveBytes of a process's log to path.
func saveLog(ctx context.Context, c logs.TailClient, process string, s logs.Stream, path string) error {
	text, err := logs.Tail(ctx, c, process, s, saveBytes)
	if err != nil {
		return wrap("read the "+string(s)+" log of "+process, err)
	}
	return writeLogFile(path, text)
}

// writeLogFile replaces path with text (mode 0644) atomically: a failed
// write leaves neither a partial file nor a temp file behind.
func writeLogFile(path, text string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".visor-log-*")
	if err != nil {
		return fmt.Errorf("cannot save the log to %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, werr := tmp.WriteString(text)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o644)); err != nil {
		return fmt.Errorf("cannot save the log to %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("cannot save the log to %s: %w", path, err)
	}
	return nil
}
