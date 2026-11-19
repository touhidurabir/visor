package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// confirmFlagged asks supervisord whether it already runs the files flagged
// as changed, added, removed or uncertain: they may have been applied
// outside Visor (supervisorctl update, a deploy script), which Visor cannot
// see otherwise. It asks when the flagged files differ from the last time it
// asked, or Settings.ConfigRecheck later while files stay flagged; now asks
// at once. A server set to Read only is never asked. It returns whether it
// asked, and supervisord's error if that failed.
func (r *Runtime) confirmFlagged(ctx context.Context, c *supervisor.Client, cs config.ChangeSet, now bool) (bool, error) {
	key := strings.Join(flaggedPaths(cs), "\n")
	r.mu.Lock()
	due := key != "" && c != nil && (now || key != r.confirmKey || time.Since(r.confirmedAt) >= r.prefs.ConfigRecheck)
	r.mu.Unlock()
	if !due || r.CheckAction() != nil {
		return false, nil
	}
	if now {
		r.applyMu.Lock()
	} else if !r.applyMu.TryLock() { // an apply is running; the next poll asks
		return false, nil
	}
	defer r.applyMu.Unlock()
	r.mu.Lock()
	r.confirmKey, r.confirmedAt = key, time.Now()
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	diff, err := c.ReloadConfig(ctx)
	cancel()
	if err != nil {
		return true, wrap("check with supervisord", err)
	}
	if err := r.markInSync(cs, diff); err != nil {
		return true, err
	}
	r.mu.Lock()
	r.confirmKey = strings.Join(flaggedPaths(r.changes), "\n") // what is left needs no second ask now
	r.mu.Unlock()
	return true, nil
}

// markInSync records as loaded each file flagged in cs that defines none of
// the groups in diff (what differs from what supervisord runs): supervisord
// already runs it as it is on disk. A flagged file Visor can't read as
// supervisord does is never marked, and when a differing group can't be
// placed in any file (a section Visor can't read), none is: it might be in
// any of them. The caller holds applyMu.
func (r *Runtime) markInSync(cs config.ChangeSet, diff supervisor.ConfigDiff) error {
	want := slices.Concat(diff.Added, diff.Changed, diff.Removed)
	pending, placed := map[string]bool{}, map[string]bool{}
	for _, f := range r.filesOfGroups(want) {
		pending[f.path] = true
		for _, g := range f.groups {
			placed[g] = true
		}
	}
	// filesOfGroups knows [program:] and [group:] sections only.
	for _, p := range flaggedPaths(cs) {
		gs, ok := r.sectionGroups(p)
		if !ok || slices.ContainsFunc(gs, func(g string) bool { return slices.Contains(want, g) }) {
			pending[p] = true
		}
		for _, g := range gs {
			placed[g] = true
		}
	}
	for _, g := range want {
		if !placed[g] {
			return nil
		}
	}
	var done []string
	for _, p := range flaggedPaths(cs) {
		if !pending[p] {
			done = append(done, p)
		}
	}
	return r.markLoaded(done)
}

func flaggedPaths(cs config.ChangeSet) []string {
	out := slices.Concat(cs.Changed, cs.Added, cs.Removed, cs.Uncertain)
	slices.Sort(out)
	return slices.Compact(out)
}

// changeSet is the files flagged at the last check.
func (r *Runtime) changeSet() config.ChangeSet {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changes
}

// flagged reports whether path is flagged as changed, added, removed or
// uncertain.
func (r *Runtime) flagged(path string) bool {
	return slices.Contains(flaggedPaths(r.changeSet()), path)
}

// sectionGroups lists the groups path defines as supervisord reads it: its
// [group:], [program:], [eventlistener:] and [fcgi-program:] sections, from
// disk or, for a file gone from disk, from Visor's snapshot. ok is false when
// Visor can't read or parse it.
func (r *Runtime) sectionGroups(path string) ([]string, bool) {
	content, err := r.files().ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		var ok bool
		if content, ok = r.loadedSnapshot(path); !ok {
			return nil, true // nothing on disk and nothing loaded
		}
	} else if err != nil {
		return nil, false
	}
	f, err := config.ParseINI(bytes.NewReader(content), path)
	if err != nil {
		return nil, false
	}
	var out []string
	for _, prefix := range []string{"group:", "program:", "eventlistener:", "fcgi-program:"} {
		for _, sec := range f.SectionsWithPrefix(prefix) {
			out = append(out, strings.TrimPrefix(sec.Name, prefix))
		}
	}
	return out, true
}
