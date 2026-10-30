package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ChangeSet lists include files (by path) whose on-disk state differs from
// what supervisord is running.
type ChangeSet struct {
	Changed, Added, Removed, Uncertain []string
}

// Watcher compares include files on disk against snapshots of what
// supervisord last loaded. It never calls supervisord and never writes into
// the include directory.
type Watcher struct {
	d        Discovery
	stateDir string

	mu     sync.Mutex
	loaded map[string]string // path -> content
	known  map[string]bool   // paths seen by an earlier Check; nil before the first
	added  map[string]bool
}

// NewWatcher reads any snapshots persisted in stateDir/loaded.json.
func NewWatcher(d Discovery, stateDir string) *Watcher {
	w := &Watcher{d: d, stateDir: stateDir, loaded: map[string]string{}, added: map[string]bool{}}
	if b, err := os.ReadFile(filepath.Join(stateDir, "loaded.json")); err == nil {
		var m map[string]string
		if json.Unmarshal(b, &m) == nil && m != nil {
			w.loaded = m
		}
	}
	return w
}

// Check classifies each include file. A file without a snapshot is
// snapshotted as loaded when it is no newer than the pidfile (supervisord's
// start); otherwise it is Added (it appeared after an earlier Check) or
// Uncertain.
func (w *Watcher) Check() (ChangeSet, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	paths, err := expandGlobs(w.d)
	if err != nil {
		return ChangeSet{}, err
	}
	var cs ChangeSet
	present := map[string]bool{}
	dirty := false
	for _, p := range paths {
		present[p] = true
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		if snap, ok := w.loaded[p]; ok {
			if snap != string(data) {
				cs.Changed = append(cs.Changed, p)
			}
			continue
		}
		if w.added[p] || (w.known != nil && !w.known[p]) {
			w.added[p] = true
			cs.Added = append(cs.Added, p)
			continue
		}
		if w.olderThanPid(p) {
			w.loaded[p] = string(data)
			dirty = true
			continue
		}
		cs.Uncertain = append(cs.Uncertain, p)
	}
	for p := range w.loaded {
		if !present[p] {
			cs.Removed = append(cs.Removed, p)
		}
	}
	for p := range w.added {
		if !present[p] {
			delete(w.added, p)
		}
	}
	w.known = present
	sort.Strings(cs.Removed)
	if dirty {
		if err := w.persist(); err != nil {
			return cs, err
		}
	}
	return cs, nil
}

func (w *Watcher) olderThanPid(path string) bool {
	if w.d.PidFile == "" {
		return false
	}
	pi, err := os.Stat(w.d.PidFile)
	if err != nil {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fi.ModTime().After(pi.ModTime())
}

// MarkLoaded snapshots the current content of each path as what supervisord
// runs. A path that no longer exists has its snapshot dropped.
func (w *Watcher) MarkLoaded(paths ...string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		switch {
		case err == nil:
			w.loaded[p] = string(data)
			delete(w.added, p)
		case errors.Is(err, os.ErrNotExist):
			delete(w.loaded, p)
			delete(w.added, p)
		default:
			return err
		}
	}
	return w.persist()
}

// Loaded returns the snapshot taken for path.
func (w *Watcher) Loaded(path string) ([]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.loaded[path]
	if !ok {
		return nil, false
	}
	return []byte(s), true
}

// persist writes loaded.json atomically (temp file + rename).
func (w *Watcher) persist() error {
	if err := os.MkdirAll(w.stateDir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(w.loaded)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(w.stateDir, "loaded-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(w.stateDir, "loaded.json"))
}
