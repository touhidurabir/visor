package apply

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// Result is what Reload applied: the groups supervisord added, changed
// (stopped, removed and added again) and removed, and one message per
// group that failed. Groups that failed are in Failed, not in the other
// lists. Skipped are the groups reloadConfig reported as added, changed or
// removed that the caller's only list left out: they still differ.
type Result struct {
	Added, Changed, Removed []string
	Errors                  []string
	Failed                  []string
	Skipped                 []string
}

// stillRunningWait bounds how long Reload keeps retrying removeProcessGroup
// while supervisord says the group's processes are still running.
const stillRunningWait = 10 * time.Second

// stillRunningPoll is the pause between those retries.
const stillRunningPoll = 100 * time.Millisecond

// Reload is `supervisorctl update`: it asks supervisord to re-read its
// config files (reloadConfig), then applies the differences group by group:
//   - removed: stop (waiting until stopped), then removeProcessGroup;
//   - changed: the same, then addProcessGroup (which starts autostart ones);
//   - added: addProcessGroup.
//
// only, when not nil, limits this to those groups; the others stay as
// supervisord runs them (and the next reloadConfig reports them again).
// An error means reloadConfig itself failed and nothing was applied;
// per-group failures are in Result.Errors.
func Reload(ctx context.Context, c *supervisor.Client, only []string) (Result, error) {
	diff, err := c.ReloadConfig(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("cannot re-read the config files: %w", err)
	}
	var res Result
	pick := func(groups []string) []string {
		if only == nil {
			return groups
		}
		var out []string
		for _, g := range groups {
			if slices.Contains(only, g) {
				out = append(out, g)
			} else {
				res.Skipped = append(res.Skipped, g)
			}
		}
		return out
	}
	fail := func(g, what string, err error) {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: cannot %s: %v", g, what, err))
		res.Failed = append(res.Failed, g)
	}
	for _, g := range pick(diff.Removed) {
		if err := stopAndRemove(ctx, c, g); err != nil {
			fail(g, "remove", err)
			continue
		}
		res.Removed = append(res.Removed, g)
	}
	for _, g := range pick(diff.Changed) {
		if err := stopAndRemove(ctx, c, g); err != nil {
			fail(g, "update", err)
			continue
		}
		if err := c.AddProcessGroup(ctx, g); err != nil {
			fail(g, "add it back after the update", err)
			continue
		}
		res.Changed = append(res.Changed, g)
	}
	for _, g := range pick(diff.Added) {
		if err := c.AddProcessGroup(ctx, g); err != nil {
			fail(g, "add", err)
			continue
		}
		res.Added = append(res.Added, g)
	}
	return res, nil
}

// Per-process result codes of stopProcessGroup that mean "stopped".
const (
	codeSuccess    = 80
	codeNotRunning = 70
)

// stopAndRemove stops every process of group g, waiting until they have
// stopped, then removes the group. supervisord refuses to remove a group
// with a process still running (STILL_RUNNING), so that is retried for a
// while.
func stopAndRemove(ctx context.Context, c *supervisor.Client, g string) error {
	res, err := c.StopProcessGroup(ctx, g, true)
	if err != nil && !errors.Is(err, supervisor.ErrNotRunning) {
		return fmt.Errorf("stop: %w", err)
	}
	var errs []error
	for _, r := range res {
		if r.Status != codeSuccess && r.Status != codeNotRunning {
			errs = append(errs, fmt.Errorf("stop %s: %w", r.Name, &supervisor.Fault{Code: r.Status, String: r.Description}))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	deadline := time.Now().Add(stillRunningWait)
	for {
		err := c.RemoveProcessGroup(ctx, g)
		if !errors.Is(err, supervisor.ErrStillRunning) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(stillRunningPoll):
		}
	}
}
