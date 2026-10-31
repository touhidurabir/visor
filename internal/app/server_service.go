// Package app is the only package that touches Wails: thin services that
// the frontend calls, built on the core packages.
package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// ServerService exposes one supervisord to the frontend: the live
// overview, process and group control, and daemon actions. Its Wails
// lifecycle starts and stops the shared Runtime.
type ServerService struct {
	rt *Runtime
}

// NewServerService returns a service backed by rt.
func NewServerService(rt *Runtime) *ServerService {
	return &ServerService{rt: rt}
}

// ServiceStartup starts the Runtime when the app starts. Connection
// problems do not stop the app; the overview reports them.
func (s *ServerService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return s.rt.Start(ctx)
}

// ServiceShutdown stops polling and waits for the Runtime's goroutines.
func (s *ServerService) ServiceShutdown() error {
	s.rt.Stop()
	return nil
}

// Reconnect drops the connection to supervisord and connects again with
// the current settings, re-reading the config. It waits for the first poll
// (a few seconds at most) and returns the overview after it, which says
// whether supervisord answered. Doctor's "Check again" calls it.
func (s *ServerService) Reconnect() (OverviewView, error) {
	err := s.rt.Reconnect()
	return s.rt.Overview(), err
}

// Overview returns the current dashboard view. The same view is pushed as
// the visor:overview event after every poll and action.
func (s *ServerService) Overview() OverviewView { return s.rt.Overview() }

// SetWatch turns notifications for one config file on or off. The choice
// is saved; new configs are watched.
func (s *ServerService) SetWatch(path string, on bool) error {
	if err := s.rt.watch.set(path, on); err != nil {
		return err
	}
	s.rt.emitOverview()
	return nil
}

// Start starts a process ("group:name") and waits until it is running.
func (s *ServerService) Start(name string) error {
	return s.rt.act(expectProcess(name), func(ctx context.Context, c *supervisor.Client) error {
		return wrap("start "+name, c.StartProcess(ctx, name, true))
	})
}

// Stop stops a process and waits until it has stopped.
func (s *ServerService) Stop(name string) error {
	return s.rt.act(expectProcess(name), func(ctx context.Context, c *supervisor.Client) error {
		return wrap("stop "+name, c.StopProcess(ctx, name, true))
	})
}

// Restart stops a process if it is running, then starts it.
func (s *ServerService) Restart(name string) error {
	return s.rt.act(expectProcess(name), func(ctx context.Context, c *supervisor.Client) error {
		return wrap("restart "+name, c.RestartProcess(ctx, name))
	})
}

// StartGroup starts every process in a group.
func (s *ServerService) StartGroup(g string) error {
	return s.rt.act(expectGroups(g), func(ctx context.Context, c *supervisor.Client) error {
		return startGroup(ctx, c, g)
	})
}

// StopGroup stops every process in a group.
func (s *ServerService) StopGroup(g string) error {
	return s.rt.act(expectGroups(g), func(ctx context.Context, c *supervisor.Client) error {
		return stopGroup(ctx, c, g)
	})
}

// RestartGroup stops every process in a group, then starts them all.
// supervisord has no restart call for groups.
func (s *ServerService) RestartGroup(g string) error {
	return s.rt.act(expectGroups(g), func(ctx context.Context, c *supervisor.Client) error {
		if err := stopGroup(ctx, c, g); err != nil {
			return err
		}
		return startGroup(ctx, c, g)
	})
}

// StartFile starts every group defined in a config file.
func (s *ServerService) StartFile(path string) error {
	return s.fileAction(path, false, true)
}

// StopFile stops every group defined in a config file.
func (s *ServerService) StopFile(path string) error {
	return s.fileAction(path, true, false)
}

// RestartFile stops every group defined in a config file, then starts them.
func (s *ServerService) RestartFile(path string) error {
	return s.fileAction(path, true, true)
}

func (s *ServerService) fileAction(path string, stop, start bool) error {
	// Without a connection, say so rather than that the file is unknown.
	if _, err := s.rt.conn(); err != nil {
		return err
	}
	groups, err := s.rt.groupsInFile(path)
	if err != nil {
		return err
	}
	return s.rt.act(expectGroups(groups...), func(ctx context.Context, c *supervisor.Client) error {
		var errs []error
		if stop {
			for _, g := range groups {
				errs = append(errs, stopGroup(ctx, c, g))
			}
			if err := errors.Join(errs...); err != nil {
				return err
			}
		}
		if start {
			for _, g := range groups {
				errs = append(errs, startGroup(ctx, c, g))
			}
		}
		return errors.Join(errs...)
	})
}

// ConfigDetail returns the config detail page for one include file: its
// live processes, the paths it refers to, where each process logs, and its
// most recent failure.
func (s *ServerService) ConfigDetail(path string) (ConfigDetailView, error) {
	return s.rt.configDetail(path)
}

// Reveal shows a file or folder in Finder.
func (s *ServerService) Reveal(path string) error { return platform.Reveal(path) }

// OpenInEditor opens a file with its default app, or a folder in Finder.
// It never launches anything: executable files are refused, and app
// bundles and file types macOS would run are revealed in Finder instead.
func (s *ServerService) OpenInEditor(path string) error { return platform.Open(path) }

// OpenLog opens a log file in the default text editor (open -t), whatever
// its extension, so no other app can run it. Folders are refused.
func (s *ServerService) OpenLog(path string) error { return platform.OpenText(path) }

// StartAll starts every process.
func (s *ServerService) StartAll() error {
	return s.rt.act(expectAll(), startAll)
}

// StopAll stops every process.
func (s *ServerService) StopAll() error {
	return s.rt.act(expectAll(), stopAll)
}

// RestartAll stops every process, then starts them all.
func (s *ServerService) RestartAll() error {
	return s.rt.act(expectAll(), func(ctx context.Context, c *supervisor.Client) error {
		if err := stopAll(ctx, c); err != nil {
			return err
		}
		return startAll(ctx, c)
	})
}

// Signal sends a signal ("HUP", "USR1", or a number) to a process.
func (s *ServerService) Signal(name, sig string) error {
	return s.rt.act(expectProcess(name), func(ctx context.Context, c *supervisor.Client) error {
		return wrap("send "+sig+" to "+name, c.SignalProcess(ctx, name, sig))
	})
}

// ClearLogs empties a process's stdout and stderr logs.
func (s *ServerService) ClearLogs(name string) error {
	return s.rt.act(expectation{}, func(ctx context.Context, c *supervisor.Client) error {
		return wrap("clear the logs of "+name, c.ClearProcessLogs(ctx, name))
	})
}

// CheckForChanges asks supervisord to re-read its config files (reread)
// and reports which groups were added, changed or removed. Nothing is
// applied.
func (s *ServerService) CheckForChanges() (supervisor.ConfigDiff, error) {
	var diff supervisor.ConfigDiff
	err := s.rt.act(expectation{}, func(ctx context.Context, c *supervisor.Client) error {
		var err error
		diff, err = c.ReloadConfig(ctx)
		return wrap("check for changes", err)
	})
	return diff, err
}

// ApplyChanges applies supervisord's re-read config (supervisorctl update):
// changed groups restart, added ones are added (and start if autostart),
// removed ones stop and go. groups limits it to those groups; nil applies
// every change. The files applied are recorded as loaded, which clears
// their "changed" badge. Per-group failures are in the result's Errors.
// An error means nothing was applied (no connection, or supervisord could
// not re-read its config), unless it says otherwise.
func (s *ServerService) ApplyChanges(groups []string) (ApplyResultView, error) {
	res, err := s.rt.applyChanges(groups)
	return toApplyResultView(res), err
}

// RemoveFromSupervisor takes a config file out of supervisord: it stops
// the file's processes, moves the file into Visor's backups (where
// "Restore a backup…" can bring it back), and removes its groups from
// supervisord, so a later update does not add them again. It never
// deletes the file outright, and stops nothing when the file cannot be
// moved (a folder Visor may not write).
func (s *ServerService) RemoveFromSupervisor(path string) (ApplyResultView, error) {
	res, err := s.rt.removeFromSupervisor(path)
	return toApplyResultView(res), err
}

// RestartDaemon restarts supervisord, which re-reads every config file and
// restarts every process.
func (s *ServerService) RestartDaemon() error {
	err := s.rt.act(expectDaemonRestart(), func(ctx context.Context, c *supervisor.Client) error {
		return wrap("restart supervisord", c.Restart(ctx))
	})
	if err != nil {
		return err
	}
	if err := s.rt.markAllLoaded(); err != nil {
		return fmt.Errorf("supervisord restarted, but Visor could not record the configs it loaded: %w", err)
	}
	return nil
}

// ShutdownDaemon stops supervisord and every process it runs. The outage
// that follows is the user's choice, so it opens no unreachable issue.
func (s *ServerService) ShutdownDaemon() error {
	return s.rt.act(expectDaemonShutdown(), func(ctx context.Context, c *supervisor.Client) error {
		return wrap("shut down supervisord", c.Shutdown(ctx))
	})
}

const (
	mainLogReadBytes    = 4 << 20
	mainLogFollowBytes  = 64 << 10
	defaultMainLogLimit = 2000
	maxMainLogLimit     = 10000
)

// MainLogQuery picks lines of supervisord's main log. Empty Levels means
// every level; FromMs and ToMs bound the time as [from, to) in Unix
// milliseconds, 0 meaning open; Query is a case-insensitive substring.
// After is the Cursor of an earlier result: only lines written since are
// read and returned (the result says Since), unless the log was rotated or
// grew too much meanwhile, when it is read in full as without a cursor.
type MainLogQuery struct {
	Levels []string
	FromMs int64
	ToMs   int64
	Query  string
	Limit  int // newest lines to return: 2000 if <= 0, at most 10000
	After  string
}

// MainLogLineView is one entry of the main log. Text may hold several
// lines (a traceback).
type MainLogLineView struct {
	AtMs  int64
	Level string
	Text  string
}

// MainLogResult is the newest matching lines, oldest first. With Since,
// they are only the lines written after the query's After cursor, to add
// to the ones shown; otherwise they replace them.
type MainLogResult struct {
	Lines     []MainLogLineView
	Truncated bool // the log is longer than what was read, so older lines were not searched
	Matched   int  // lines matching the query, which may exceed len(Lines)
	Since     bool
	Cursor    string // pass as After to read only newer lines
}

// MainLogLines reads the last 4 MiB of supervisord's own log, parses it and
// returns the newest lines matching q. With q.After (following), it reads
// only the last 64 KiB and returns the lines written since.
func (s *ServerService) MainLogLines(q MainLogQuery) (MainLogResult, error) {
	var chunk supervisor.MainLogChunk
	err := s.rt.act(expectation{}, func(ctx context.Context, c *supervisor.Client) error {
		var err error
		chunk, err = c.ReadMainLog(ctx, q.After, mainLogFollowBytes, mainLogReadBytes)
		if errors.Is(err, supervisor.ErrNoFile) {
			return noMainLogError{err}
		}
		return wrap("read the supervisord log", err)
	})
	if err != nil {
		return MainLogResult{}, err
	}
	res := filterMainLog(supervisor.ParseMainLogLines(chunk.Text, time.Local), q)
	res.Truncated, res.Since, res.Cursor = chunk.Truncated, chunk.Since, chunk.Cursor
	return res, nil
}

// filterMainLog applies q to lines (oldest first).
func filterMainLog(lines []supervisor.MainLogLine, q MainLogQuery) MainLogResult {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultMainLogLimit
	}
	limit = min(limit, maxMainLogLimit)
	needle := strings.ToLower(q.Query)
	res := MainLogResult{Lines: []MainLogLineView{}}
	var hits []supervisor.MainLogLine
	for _, l := range lines {
		ms := l.At.UnixMilli()
		switch {
		case len(q.Levels) > 0 && !slices.Contains(q.Levels, l.Level):
		case q.FromMs > 0 && ms < q.FromMs:
		case q.ToMs > 0 && ms >= q.ToMs:
		case needle != "" && !strings.Contains(strings.ToLower(l.Text), needle):
		default:
			hits = append(hits, l)
		}
	}
	res.Matched = len(hits)
	if len(hits) > limit {
		hits = hits[len(hits)-limit:]
	}
	for _, l := range hits {
		res.Lines = append(res.Lines, MainLogLineView{AtMs: l.At.UnixMilli(), Level: l.Level, Text: l.Text})
	}
	return res
}

// noMainLogError is readLog's NO_FILE, which here means supervisord keeps
// no log file of its own (not a missing command, as NO_FILE means when
// starting a process). It still matches supervisor.ErrNoFile.
type noMainLogError struct{ err error }

func (e noMainLogError) Error() string {
	return "supervisord writes no log file (logfile= is unset or syslog). Set [supervisord] logfile= to see it here."
}

func (e noMainLogError) Unwrap() error { return e.err }

func startGroup(ctx context.Context, c *supervisor.Client, g string) error {
	res, err := c.StartProcessGroup(ctx, g, true)
	return resultsErr("start "+g, res, err)
}

func stopGroup(ctx context.Context, c *supervisor.Client, g string) error {
	res, err := c.StopProcessGroup(ctx, g, true)
	return resultsErr("stop "+g, res, err)
}

func startAll(ctx context.Context, c *supervisor.Client) error {
	res, err := c.StartAllProcesses(ctx, true)
	return resultsErr("start all", res, err)
}

func stopAll(ctx context.Context, c *supervisor.Client) error {
	res, err := c.StopAllProcesses(ctx, true)
	return resultsErr("stop all", res, err)
}

// Fault codes that mean a per-process result needs no attention.
const (
	codeSuccess        = 80
	codeAlreadyStarted = 60
	codeNotRunning     = 70
)

// resultsErr turns a group or "all" call's per-process failures into one
// error that still matches the supervisor.Err* sentinels.
func resultsErr(what string, res []supervisor.ProcessResult, err error) error {
	if err != nil {
		return wrap(what, err)
	}
	var errs []error
	for _, r := range res {
		switch r.Status {
		case codeSuccess, codeAlreadyStarted, codeNotRunning:
			continue
		}
		f := &supervisor.Fault{Code: r.Status, String: r.Description}
		errs = append(errs, fmt.Errorf("%s:%s: %w", r.Group, r.Name, f))
	}
	return wrap(what, errors.Join(errs...))
}

func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cannot %s: %w", what, err)
}
