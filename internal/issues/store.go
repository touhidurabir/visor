package issues

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	issuesFile  = "issues.jsonl"
	historyFile = "history.jsonl"
)

// Filter selects issues. Status is "open", "resolved" (anything closed) or
// "all" (also the empty string). Before and BeforeID are a paging cursor,
// the last issue of the previous page: only issues strictly after it in
// newest-first order are kept (opened earlier, or opened in the same
// millisecond with a smaller ID). A zero Before means no cursor; an empty
// BeforeID keeps only issues opened earlier. Limit 0 means no limit. Query
// is a case-insensitive substring of the process, kind or message.
type Filter struct {
	Status   string
	Before   time.Time
	BeforeID string
	Limit    int
	Query    string
}

// Store keeps issues and history as append-only JSONL files in one directory.
// An issue status change appends a new record with the same ID; readers keep
// the last record per ID.
type Store struct {
	dir string
	mu  sync.Mutex
}

// OpenStore creates dir (mode 0700) if needed and compacts it: issues closed,
// and history recorded, longer ago than retention are dropped. Open issues
// are always kept.
func OpenStore(dir string, retention time.Duration) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create issues dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure issues dir: %w", err)
	}
	s := &Store{dir: dir}
	if err := s.compact(time.Now().Add(-retention)); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// pastCursor reports whether is comes after the cursor (before, beforeID)
// in newestFirst order. Times compare to the millisecond, like the sort.
func pastCursor(is Issue, before time.Time, beforeID string) bool {
	ms, cur := is.OpenedAt.UnixMilli(), before.UnixMilli()
	return ms < cur || (ms == cur && beforeID != "" && is.ID < beforeID)
}

// Issues returns the matching issues, newest opened first (ties broken by
// ID, descending).
func (s *Store) Issues(f Filter) ([]Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readIssues()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(f.Query)
	var out []Issue
	for _, is := range all {
		switch f.Status {
		case "open":
			if is.Status != StatusOpen {
				continue
			}
		case "resolved":
			if is.Status == StatusOpen {
				continue
			}
		}
		if !f.Before.IsZero() && !pastCursor(is, f.Before, f.BeforeID) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(is.Process+" "+string(is.Kind)+" "+is.Message), q) {
			continue
		}
		out = append(out, is)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

// Counts returns how many issues are open and how many are closed (resolved
// or dismissed), in one pass over the log.
func (s *Store) Counts() (open, closed int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readIssues()
	if err != nil {
		return 0, 0, err
	}
	for _, is := range all {
		if is.Status == StatusOpen {
			open++
		} else {
			closed++
		}
	}
	return open, closed, nil
}

// History returns state transitions, newest first. An empty process means
// every process. A limit of 0 or less means no limit.
func (s *Store) History(process string, limit int) ([]Transition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := readJSONL[Transition](s.path(historyFile))
	if err != nil {
		return nil, err
	}
	var out []Transition
	for i := len(all) - 1; i >= 0; i-- {
		if process != "" && all[i].Process != process {
			continue
		}
		out = append(out, all[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Store) putIssue(is Issue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return appendJSONL(s.path(issuesFile), is)
}

func (s *Store) addTransitions(ts []Transition) error {
	if len(ts) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range ts {
		if err := appendJSONL(s.path(historyFile), t); err != nil {
			return err
		}
	}
	return nil
}

// readIssues folds the log to the last record per ID, in newestFirst order.
func (s *Store) readIssues() ([]Issue, error) {
	recs, err := readJSONL[Issue](s.path(issuesFile))
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	var out []Issue
	for _, r := range recs {
		if i, ok := idx[r.ID]; ok {
			out[i] = r
			continue
		}
		idx[r.ID] = len(out)
		out = append(out, r)
	}
	slices.SortStableFunc(out, newestFirst)
	return out, nil
}

func (s *Store) compact(cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readIssues()
	if err != nil {
		return err
	}
	var keep []Issue
	for i := len(all) - 1; i >= 0; i-- { // oldest first, like the log
		if is := all[i]; is.Status == StatusOpen || !is.ClosedAt.Before(cutoff) {
			keep = append(keep, is)
		}
	}
	if err := rewriteJSONL(s.path(issuesFile), keep); err != nil {
		return err
	}
	hist, err := readJSONL[Transition](s.path(historyFile))
	if err != nil {
		return err
	}
	var hkeep []Transition
	for _, t := range hist {
		if !t.At.Before(cutoff) {
			hkeep = append(hkeep, t)
		}
	}
	return rewriteJSONL(s.path(historyFile), hkeep)
}

// appendJSONL appends v as one line. If the file does not end in a newline
// (a write torn by a crash), the record starts on a fresh line, so only
// the torn record is lost.
func appendJSONL(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, fi.Size()-1); err == nil && last[0] != '\n' {
			b = append([]byte{'\n'}, b...)
		}
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func rewriteJSONL[T any](path string, recs []T) error {
	var buf bytes.Buffer
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// maxRecord is the longest line readJSONL parses; longer ones are skipped.
const maxRecord = 4 << 20

// readJSONL reads every record; a missing file is empty, and a line that
// does not parse (e.g. torn by a crash) or is over maxRecord is skipped.
func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	r := bufio.NewReaderSize(f, 64<<10)
	var line []byte
	tooLong := false
	for {
		chunk, more, err := r.ReadLine()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		if !tooLong {
			line = append(line, chunk...)
			tooLong = len(line) > maxRecord
		}
		if more {
			continue
		}
		var v T
		if !tooLong && json.Unmarshal(line, &v) == nil {
			out = append(out, v)
		}
		line, tooLong = line[:0], false
	}
}
