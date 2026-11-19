package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
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
	loaded map[string]snapshot // path -> what supervisord runs
	known  map[string]bool     // paths seen by an earlier Check; nil before the first
	added  map[string]bool
}

// snapshot is the content supervisord is believed to run, and when it was
// recorded (compared with the pidfile mtime to spot daemon restarts).
type snapshot struct {
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

// stateFile is the loaded.json format. Version 1 (a bare path -> content map)
// is discarded on read; its files are then treated as seen for the first time.
type stateFile struct {
	Version int                 `json:"version"`
	Files   map[string]snapshot `json:"files"`
}

// NewWatcher reads any snapshots persisted in stateDir/loaded.json.
func NewWatcher(d Discovery, stateDir string) *Watcher {
	w := &Watcher{d: d, stateDir: stateDir, loaded: map[string]snapshot{}, added: map[string]bool{}}
	if b, err := os.ReadFile(filepath.Join(stateDir, "loaded.json")); err == nil {
		var sf stateFile
		if json.Unmarshal(b, &sf) == nil && sf.Version == 2 && sf.Files != nil {
			w.loaded = sf.Files
		}
	}
	return w
}

// Check classifies each include file. A file without a snapshot is
// snapshotted as loaded when it is no newer than the pidfile (supervisord's
// start); otherwise it is Added (it appeared after an earlier Check) or
// Uncertain. A snapshot of a file gone from disk is Removed, until
// supervisord restarts (the pidfile is newer than the snapshot), which
// drops it.
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
	pm, pidOK := w.mtime(w.d.PidFile)
	for _, p := range paths {
		present[p] = true
		data, rerr := w.d.Files().ReadFile(p)
		if rerr != nil {
			continue
		}
		if snap, ok := w.loaded[p]; ok {
			if snap.Content == string(data) {
				// supervisord restarted since the snapshot and runs this
				// content still: restamp it with the restart (the pidfile's
				// clock), or a later edit would look Uncertain.
				if pidOK && pm.After(snap.At) {
					w.loaded[p] = snapshot{Content: snap.Content, At: pm}
					dirty = true
				}
				continue
			}
			// supervisord restarted since the snapshot: it re-read the
			// files then, so a file untouched since the restart is loaded.
			if pidOK && pm.After(snap.At) {
				if fm, ok := w.mtime(p); ok && !fm.After(pm) {
					w.loaded[p] = snapshot{Content: string(data), At: time.Now()}
					dirty = true
				} else {
					cs.Uncertain = append(cs.Uncertain, p)
				}
				continue
			}
			cs.Changed = append(cs.Changed, p)
			continue
		}
		if w.added[p] || (w.known != nil && !w.known[p]) {
			w.added[p] = true
			cs.Added = append(cs.Added, p)
			continue
		}
		if w.olderThanPid(p) {
			w.loaded[p] = snapshot{Content: string(data), At: time.Now()}
			dirty = true
			continue
		}
		cs.Uncertain = append(cs.Uncertain, p)
	}
	for p, snap := range w.loaded {
		if present[p] {
			continue
		}
		// supervisord restarted since the snapshot, after the file was
		// gone: it no longer runs it.
		if pidOK && pm.After(snap.At) {
			delete(w.loaded, p)
			dirty = true
			continue
		}
		cs.Removed = append(cs.Removed, p)
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

func (w *Watcher) mtime(path string) (time.Time, bool) {
	if path == "" {
		return time.Time{}, false
	}
	fi, err := w.d.Files().Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

func (w *Watcher) olderThanPid(path string) bool {
	pm, ok := w.mtime(w.d.PidFile)
	if !ok {
		return false
	}
	fm, ok := w.mtime(path)
	return ok && !fm.After(pm)
}

// MarkLoaded snapshots the current content of each path as what supervisord
// runs. A path that no longer exists has its snapshot dropped.
func (w *Watcher) MarkLoaded(paths ...string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range paths {
		data, err := w.d.Files().ReadFile(p)
		switch {
		case err == nil:
			w.loaded[p] = snapshot{Content: string(data), At: time.Now()}
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
	return []byte(s.Content), true
}

// persist writes loaded.json atomically (temp file + rename).
func (w *Watcher) persist() error {
	if err := os.MkdirAll(w.stateDir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(stateFile{Version: 2, Files: w.loaded})
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
