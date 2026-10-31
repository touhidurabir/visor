package app

import (
	"errors"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
)

// errNoTracker: without a supervisord connection no issue can be open.
var errNoTracker = errors.New("no issue is open: Visor is not connected to supervisord")

// IssueService exposes the issue list and process history to the frontend.
type IssueService struct {
	rt *Runtime
}

// NewIssueService returns a service backed by rt (started by ServerService).
func NewIssueService(rt *Runtime) *IssueService {
	return &IssueService{rt: rt}
}

// IssueQuery selects issues for List. Status is "open", "resolved" or
// "all" (or empty); Kind is an issue kind or empty for any. FromMs and ToMs
// bound OpenedMs to [FromMs, ToMs) (0 = unbounded). BeforeMs and BeforeID
// are the "Load older" cursor: the last issue shown (its OpenedMs and ID);
// only issues after it are returned, so issues opened in the same
// millisecond are neither skipped nor repeated. BeforeMs <= 0 means no
// cursor. Limit 0 means no limit; Query matches process, kind or message.
type IssueQuery struct {
	Status   string
	Kind     string
	FromMs   int64
	ToMs     int64
	BeforeMs int64
	BeforeID string
	Limit    int
	Query    string
}

// List returns issues, newest first (by OpenedMs, then ID).
func (s *IssueService) List(q IssueQuery) ([]IssueView, error) {
	store, _, err := s.rt.issueStore()
	if err != nil {
		return nil, err
	}
	f := issues.Filter{Status: q.Status, Kind: issues.Kind(q.Kind), Limit: q.Limit, Query: q.Query}
	if q.FromMs > 0 {
		f.From = time.UnixMilli(q.FromMs)
	}
	if q.ToMs > 0 {
		f.To = time.UnixMilli(q.ToMs)
	}
	if q.BeforeMs > 0 {
		f.Before, f.BeforeID = time.UnixMilli(q.BeforeMs), q.BeforeID
	}
	list, err := store.Issues(f)
	if err != nil {
		return nil, err
	}
	out := make([]IssueView, 0, len(list))
	for _, is := range list {
		out = append(out, toIssueView(is))
	}
	return out, nil
}

// IssueStats are the counts shown above the Issues list.
type IssueStats struct {
	Total    int
	Open     int
	Resolved int
	Last24h  int
	Last7d   int
	Last30d  int
}

// Stats counts every issue, ignoring any filter.
func (s *IssueService) Stats() (IssueStats, error) {
	store, _, err := s.rt.issueStore()
	if err != nil {
		return IssueStats{}, err
	}
	st, err := store.Stats(time.Now())
	return IssueStats(st), err
}

// ResolveMany marks the open issues among ids resolved by the user and
// returns how many it resolved; other ids are skipped.
func (s *IssueService) ResolveMany(ids []string) (int, error) {
	_, tracker, err := s.rt.issueStore()
	if err != nil {
		return 0, err
	}
	if tracker == nil {
		return 0, errNoTracker
	}
	n, err := tracker.ResolveMany(ids)
	if n > 0 {
		s.rt.emitOverview()
	}
	return n, err
}

// Delete removes the closed issues among ids for good and returns how many
// it removed; open issues and unknown ids are skipped. The history stays.
func (s *IssueService) Delete(ids []string) (int, error) {
	store, _, err := s.rt.issueStore()
	if err != nil {
		return 0, err
	}
	n, err := store.Delete(ids...)
	if n > 0 {
		s.rt.emitOverview()
	}
	return n, err
}

// Resolve marks an open issue resolved by the user.
func (s *IssueService) Resolve(id string) error {
	return s.close(id, (*issues.Tracker).Resolve)
}

// Dismiss closes an open issue without resolving it.
func (s *IssueService) Dismiss(id string) error {
	return s.close(id, (*issues.Tracker).Dismiss)
}

func (s *IssueService) close(id string, do func(*issues.Tracker, string) error) error {
	_, tracker, err := s.rt.issueStore()
	if err != nil {
		return err
	}
	if tracker == nil {
		return errNoTracker
	}
	if err := do(tracker, id); err != nil {
		return err
	}
	s.rt.emitOverview()
	return nil
}

// History returns a process's state changes, newest first ("" for every
// process; limit 0 for no limit).
func (s *IssueService) History(process string, limit int) ([]TransitionView, error) {
	store, _, err := s.rt.issueStore()
	if err != nil {
		return nil, err
	}
	list, err := store.History(process, limit)
	if err != nil {
		return nil, err
	}
	out := make([]TransitionView, 0, len(list))
	for _, t := range list {
		out = append(out, toTransitionView(t))
	}
	return out, nil
}
