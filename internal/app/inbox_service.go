package app

import (
	"path/filepath"

	"github.com/touhidurabir/visor/internal/notify"
)

// NotificationsView is Visor's own notification list, newest first, with
// how many are unread.
type NotificationsView struct {
	// Server is the tab the list belongs to (LocalID for this computer).
	Server string
	Unread int
	Items  []NotificationView
}

// NotificationView is one notification. Config is the config file's path
// ("" for supervisord itself) and ConfigName its file name; AtMs is when
// it last happened. Shown says whether macOS showed it as a banner.
type NotificationView struct {
	ID         string
	IssueID    string
	Kind       string
	Process    string
	Config     string
	ConfigName string
	Title      string
	Detail     string
	AtMs       int64
	Count      int
	Read       bool
	Shown      bool
	Severity   string // "error", "warning" or "info"
}

func notificationsView(b *notify.Inbox) NotificationsView {
	list := b.List()
	v := NotificationsView{Items: make([]NotificationView, 0, len(list))}
	for _, e := range list {
		if !e.Read {
			v.Unread++
		}
		name := ""
		if e.Config != "" {
			name = filepath.Base(e.Config)
		}
		v.Items = append(v.Items, NotificationView{
			ID: e.ID, IssueID: e.IssueID, Kind: string(e.Kind), Process: e.Process, Config: e.Config, ConfigName: name,
			Title: e.Title, Detail: e.Detail, AtMs: ms(e.At), Count: e.Count, Read: e.Read, Shown: e.Shown, Severity: string(e.Level()),
		})
	}
	return v
}

// InboxService exposes Visor's own notification list (the bell) to the
// frontend. Changes also arrive as EventNotifications.
type InboxService struct {
	rt *Runtime
}

// NewInboxService returns a service backed by rt.
func NewInboxService(rt *Runtime) *InboxService { return &InboxService{rt: rt} }

// Notifications returns the list, newest first.
func (s *InboxService) Notifications() NotificationsView { return notificationsView(s.rt.inbox) }

// MarkRead marks one notification read.
func (s *InboxService) MarkRead(id string) error {
	if err := s.rt.inbox.MarkRead(id); err != nil {
		return err
	}
	s.rt.emitNotifications()
	return nil
}

// MarkAllRead marks every notification read.
func (s *InboxService) MarkAllRead() error {
	if err := s.rt.inbox.MarkAllRead(); err != nil {
		return err
	}
	s.rt.emitNotifications()
	return nil
}
