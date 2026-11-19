package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// backups is where Visor keeps copies of the config files it changes or
// removes: stateDir/backups, outside every include folder.
func (r *Runtime) backups() apply.Backups {
	return apply.Backups{Dir: filepath.Join(r.opts.StateDir, "backups")}
}

// applyChanges is `supervisorctl update` for groups (nil: every group
// supervisord reports added, changed or removed). On success the config
// files of those groups (with nil, every include file) are recorded as what
// supervisord now runs, which clears their "changed" badge; a file with a
// group that failed, or with a change that was not applied (one that
// turned up after the groups were picked), keeps it. The caller holds
// applyMu.
func (r *Runtime) applyChanges(groups []string) (apply.Result, error) {
	ac, err := r.conn()
	if err != nil {
		return apply.Result{}, err
	}
	all := groups == nil
	if all {
		// Learn the groups first, so the action covers exactly their
		// processes (no other process's crash is taken for the user's).
		ctx, cancel := context.WithTimeout(ac.ctx, rpcTimeout)
		diff, err := ac.client.ReloadConfig(ctx)
		cancel()
		if err != nil {
			return apply.Result{}, wrap("check for changes", reconnecting(ac.ctx, err))
		}
		// Never nil (which Reload takes as "all"): a group that turns up
		// after this check waits for the next apply.
		groups = append(append(append([]string{}, diff.Added...), diff.Changed...), diff.Removed...)
	}
	files := r.filesOfGroups(groups) // before applying: a removed group's file may be gone
	var res apply.Result
	err = r.act(r.expectApply(groups), func(ctx context.Context, c *supervisor.Client) error {
		var err error
		res, err = apply.Reload(ctx, c, groups)
		return err
	})
	if err != nil {
		return res, err
	}
	if all && len(res.Failed) == 0 && len(res.Skipped) == 0 {
		err = r.markAllLoaded()
	} else {
		err = r.markLoaded(filesWithout(files, append(slices.Clone(res.Failed), res.Skipped...)))
	}
	if err != nil {
		return res, fmt.Errorf("applied, but Visor could not record what supervisord now runs: %w", err)
	}
	r.emitOverview()
	return res, nil
}

// expectApply covers groups for an apply. A group some include file still
// defines is being changed (stopped, removed and added again), not removed:
// if its processes go away meanwhile, their issues close as "config
// applied".
func (r *Runtime) expectApply(groups []string) expectation {
	r.mu.Lock()
	d := r.d
	r.mu.Unlock()
	onDisk := map[string]bool{}
	if inv, err := config.LoadInventory(d); err == nil {
		for _, f := range inv.Files {
			for _, p := range f.Programs {
				onDisk[p.Group] = true
			}
		}
	}
	e := expectGroups(groups...)
	e.reapplied = func(p string) bool {
		g, _, _ := strings.Cut(p, ":")
		return onDisk[g] && slices.Contains(groups, g)
	}
	return e
}

// removeFromSupervisor takes an include file out of supervisord: it backs
// the file up, stops the file's groups (waiting until they have stopped),
// moves the file into Visor's backups (apply.RemoveBackedUp), and applies
// the removal for those groups only. Nothing is stopped when the file
// cannot be backed up or moved. Its processes' open issues close as
// removed; being the user's action, the stop opens none and notifies
// nothing. The caller holds applyMu.
func (r *Runtime) removeFromSupervisor(path string) (apply.Result, error) {
	if _, err := r.includeFile(path); err != nil {
		return apply.Result{}, err
	}
	if _, err := r.conn(); err != nil {
		return apply.Result{}, err
	}
	groups, err := r.groupsInFile(path)
	if err != nil {
		return apply.Result{}, err
	}
	d := r.disk()
	if err := d.CheckRemovable(path); err != nil {
		return apply.Result{}, err
	}
	// The backup first, so a backups folder Visor can't write stops nothing.
	b := r.backups()
	bk, err := apply.BackUpForRemove(d, path, b)
	if err != nil {
		return apply.Result{}, err
	}
	var res apply.Result
	removed := false
	err = r.act(expectGroups(groups...), func(ctx context.Context, c *supervisor.Client) error {
		var errs []error
		for _, g := range groups {
			// A group supervisord does not run (never applied) has nothing to stop.
			if err := stopGroup(ctx, c, g); err != nil && !errors.Is(err, supervisor.ErrBadName) {
				errs = append(errs, err)
			}
		}
		if err := errors.Join(errs...); err != nil {
			return err
		}
		if _, err := apply.RemoveBackedUp(d, path, bk, b); err != nil {
			return fmt.Errorf("stopped its processes, but %w", err)
		}
		removed = true
		var err error
		res, err = apply.Reload(ctx, c, groups)
		return err
	})
	if !removed {
		b.Discard(bk) // the file stays, so its removal backup goes
	}
	if err != nil {
		return res, err
	}
	if len(res.Failed) == 0 {
		if err := r.markLoaded([]string{path}); err != nil {
			return res, fmt.Errorf("removed, but Visor could not record it: %w", err)
		}
	}
	r.emitOverview()
	return res, nil
}

// markLoaded records paths as what supervisord runs (a path gone from disk
// has its snapshot dropped).
func (r *Runtime) markLoaded(paths []string) error {
	r.mu.Lock()
	w := r.watcher
	r.mu.Unlock()
	if w == nil || len(paths) == 0 {
		return nil
	}
	if err := w.MarkLoaded(paths...); err != nil {
		return err
	}
	return r.refreshChanges()
}

// refreshChanges re-runs the watcher so the overview stops showing files
// just applied as changed, without waiting for the next poll.
func (r *Runtime) refreshChanges() error {
	r.mu.Lock()
	w := r.watcher
	r.mu.Unlock()
	if w == nil {
		return nil
	}
	cs, err := w.Check()
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.watcher == w {
		r.changes = cs
	}
	r.mu.Unlock()
	return nil
}

// filesOfGroups lists the include files that define any of groups: from the
// files on disk, and for a file deleted since supervisord loaded it, from
// the snapshot Visor kept of it.
func (r *Runtime) filesOfGroups(groups []string) []fileGroups {
	r.mu.Lock()
	d, w, removed := r.d, r.watcher, slices.Clone(r.changes.Removed)
	r.mu.Unlock()
	var out []fileGroups
	if inv, err := config.LoadInventory(d); err == nil {
		for _, f := range inv.Files {
			var gs []string
			for _, p := range f.Programs {
				gs = append(gs, p.Group)
			}
			for _, g := range groups {
				if fp, ok := inv.FileForGroup(g); ok && fp == f.Path {
					gs = append(gs, g)
				}
			}
			out = appendIfAny(out, f.Path, gs, groups)
		}
	}
	if w == nil {
		return out
	}
	for _, p := range removed {
		if snap, ok := w.Loaded(p); ok {
			out = appendIfAny(out, p, groupsIn(snap, p), groups)
		}
	}
	return out
}

// fileGroups is an include file and the groups it defines.
type fileGroups struct {
	path   string
	groups []string
}

func appendIfAny(out []fileGroups, path string, gs, want []string) []fileGroups {
	if slices.ContainsFunc(gs, func(g string) bool { return slices.Contains(want, g) }) {
		return append(out, fileGroups{path, gs})
	}
	return out
}

// groupsIn lists the groups a config's content defines: its [group:x]
// sections, and its programs (a program in a group of another file counts
// under its own name, which is close enough to find the file).
func groupsIn(content []byte, path string) []string {
	f, err := config.ParseINI(bytes.NewReader(content), path)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range f.SectionsWithPrefix("group:") {
		out = append(out, strings.TrimPrefix(s.Name, "group:"))
	}
	for _, s := range f.SectionsWithPrefix("program:") {
		out = append(out, strings.TrimPrefix(s.Name, "program:"))
	}
	return out
}

// filesWithout lists the paths of files with none of the failed groups.
func filesWithout(files []fileGroups, failed []string) []string {
	var out []string
	for _, f := range files {
		if !slices.ContainsFunc(f.groups, func(g string) bool { return slices.Contains(failed, g) }) {
			out = append(out, f.path)
		}
	}
	return out
}

// toApplyResultView never has nil lists, so the frontend gets arrays.
func toApplyResultView(res apply.Result) ApplyResultView {
	orEmpty := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	return ApplyResultView{
		Added:   orEmpty(res.Added),
		Changed: orEmpty(res.Changed),
		Removed: orEmpty(res.Removed),
		Errors:  orEmpty(res.Errors),
	}
}
