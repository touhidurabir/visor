package app

import (
	"context"
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
	if due {
		r.confirmKey, r.confirmedAt = key, time.Now()
	}
	r.mu.Unlock()
	if !due || r.CheckAction() != nil {
		return false, nil
	}
	if now {
		r.applyMu.Lock()
	} else if !r.applyMu.TryLock() { // an apply is running; it records what it loads
		return false, nil
	}
	defer r.applyMu.Unlock()
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
// already runs it as it is on disk. The caller holds applyMu.
func (r *Runtime) markInSync(cs config.ChangeSet, diff supervisor.ConfigDiff) error {
	pending := map[string]bool{}
	for _, f := range r.filesOfGroups(slices.Concat(diff.Added, diff.Changed, diff.Removed)) {
		pending[f.path] = true
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
