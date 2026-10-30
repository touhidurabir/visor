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

// List returns issues, newest first. status is "open", "resolved" or
// "all"; beforeMs > 0 keeps issues opened before that time (for "Load
// older"); limit 0 means no limit; query matches process, kind or message.
func (s *IssueService) List(status string, beforeMs int64, limit int, query string) ([]IssueView, error) {
	store, _, err := s.rt.issueStore()
	if err != nil {
		return nil, err
	}
	f := issues.Filter{Status: status, Limit: limit, Query: query}
	if beforeMs > 0 {
		f.Before = time.UnixMilli(beforeMs)
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
