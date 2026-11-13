package activity

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// fileName is the activity log inside a state folder: one JSON event per line.
const fileName = "activity.jsonl"

// maxKept is how many events are kept in memory (and in the file after a
// compaction); older ones are dropped.
const maxKept = 5000

// Store keeps events in memory and appends them to dir/activity.jsonl.
// Events older than the retention are dropped when the store opens.
type Store struct {
	mu     sync.Mutex
	path   string
	events []Event // oldest first
}

// Open reads dir/activity.jsonl, dropping events older than retention (0
// keeps everything) and lines it can't read, and rewrites it when it dropped
// any. A missing file is an empty store.
func Open(dir string, retention time.Duration, now time.Time) (*Store, error) {
	s := &Store{path: filepath.Join(dir, fileName)}
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	defer func() { _ = f.Close() }()
	dropped := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			dropped = true
			continue
		}
		if retention > 0 && now.Sub(e.At) > retention {
			dropped = true
			continue
		}
		s.events = append(s.events, e)
	}
	if len(s.events) > maxKept {
		s.events = s.events[len(s.events)-maxKept:]
		dropped = true
	}
	if dropped {
		_ = s.rewrite()
	}
	return s, sc.Err()
}

func (s *Store) rewrite() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "activity-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	w := bufio.NewWriter(tmp)
	for _, e := range s.events {
		b, _ := json.Marshal(e)
		_, _ = w.Write(append(b, '\n'))
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// Add appends events.
func (s *Store) Add(events ...Event) error {
	if len(events) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, events...)
	if len(s.events) > maxKept {
		s.events = s.events[len(s.events)-maxKept:]
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, e := range events {
		b, _ := json.Marshal(e)
		_, _ = w.Write(append(b, '\n'))
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Query selects events for List: Kind is a kind or a mute group
// ("processes", "programs", "files", "daemon") or "" for all; Process
// matches part of the process or config name; Outside keeps only changes
// made outside Visor. Limit 0 means 500.
type Query struct {
	Kind    string
	Process string
	Outside bool
	Limit   int
}

// List returns matching events, newest first.
func (s *Store) List(q Query) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	var out []Event
	for _, e := range slices.Backward(s.events) {
		if q.Kind != "" && string(e.Kind) != q.Kind && e.Kind.Group() != q.Kind {
			continue
		}
		if q.Outside && e.ByVisor {
			continue
		}
		if q.Process != "" && !strings.Contains(strings.ToLower(e.Process+" "+e.Config), strings.ToLower(q.Process)) {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out
}

// Latest is the newest event made outside Visor, if any.
func (s *Store) Latest() (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range slices.Backward(s.events) {
		if !e.ByVisor {
			return e, true
		}
	}
	return Event{}, false
}
