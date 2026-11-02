package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
)

const (
	inboxFile = "notifications.json"
	// inboxMax is how many notifications the inbox keeps.
	inboxMax = 200
	// inboxKeep is how long it keeps them.
	inboxKeep = 30 * 24 * time.Hour
)

// ErrNoEntry is returned for a notification the inbox does not have.
var ErrNoEntry = errors.New("that notification is no longer in the list")

// Entry is one notification in Visor's own list. Read is whether the
// person has seen it in Visor; Shown is whether macOS showed it as a banner.
// Count is how many times its failure happened (see issues.Issue.Count).
type Entry struct {
	ID      string      `json:"id"`
	IssueID string      `json:"issueId"`
	Kind    issues.Kind `json:"kind"`
	Process string      `json:"process"`
	Config  string      `json:"config"`
	Title   string      `json:"title"`
	Detail  string      `json:"detail"`
	At      time.Time   `json:"at"`
	Count   int         `json:"count"`
	Read    bool        `json:"read"`
	Shown   bool        `json:"shown"`
}

// Inbox is the list of notifications Visor shows in its own window, with
// read and unread state, so nothing depends on macOS showing a banner. It
// is kept in dir/notifications.json; with dir "" it lives in memory only.
// It knows nothing about Wails.
type Inbox struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	entries []Entry // newest first
	seq     int
}

type inboxData struct {
	Version int     `json:"version"`
	Seq     int     `json:"seq"`
	Entries []Entry `json:"entries"`
}

// OpenInbox loads the inbox kept in dir. A missing or unreadable file
// starts an empty list: the inbox is a convenience, never a reason to fail.
func OpenInbox(dir string, now func() time.Time) *Inbox {
	if now == nil {
		now = time.Now
	}
	b := &Inbox{now: now}
	if dir == "" {
		return b
	}
	b.path = filepath.Join(dir, inboxFile)
	if raw, err := os.ReadFile(b.path); err == nil {
		var d inboxData
		if json.Unmarshal(raw, &d) == nil && d.Version == 1 {
			b.entries, b.seq = d.Entries, d.Seq
		}
	}
	return b
}

// Record adds the notification for an issue that opened, or that
// happened again: a repeat updates the issue's unread entry and moves it
// to the top, and adds a new entry once the old one has been read.
func (b *Inbox) Record(is issues.Issue) (Entry, error) {
	m := Compose(is)
	b.mu.Lock()
	defer b.mu.Unlock()
	if k := slices.IndexFunc(b.entries, func(e Entry) bool { return e.IssueID == is.ID && !e.Read }); k >= 0 {
		e := b.entries[k]
		e.Title, e.Detail, e.At, e.Count, e.Shown = m.Title, m.Detail, b.now(), is.Times(), false
		b.entries = slices.Delete(b.entries, k, k+1)
		return e, b.push(e)
	}
	return b.add(Entry{IssueID: is.ID, Kind: is.Kind, Process: is.Process, Config: is.Config,
		Title: m.Title, Detail: m.Detail, Count: is.Times()})
}

// Recovered adds a note that supervisord answers again after the
// unreachable issue is closed. It is good news with nothing to do, so it
// is added as read: it shows under All, not in the unread count.
func (b *Inbox) Recovered(is issues.Issue) (Entry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	end := is.ClosedAt
	if end.IsZero() {
		end = b.now()
	}
	return b.add(Entry{IssueID: is.ID, Kind: is.Kind, Title: "supervisord is responding again",
		Detail: "It was unreachable for " + span(end.Sub(is.OpenedAt)), Read: true})
}

// add stamps e with an ID and the time and puts it first. Caller holds mu.
func (b *Inbox) add(e Entry) (Entry, error) {
	b.seq++
	e.ID = "n" + strconv.Itoa(b.seq)
	e.At = b.now()
	return e, b.push(e)
}

// push puts e first, drops what is too old or too many, and saves.
// Caller holds mu.
func (b *Inbox) push(e Entry) error {
	b.entries = append([]Entry{e}, b.entries...)
	cutoff := b.now().Add(-inboxKeep)
	b.entries = slices.DeleteFunc(b.entries, func(x Entry) bool { return x.At.Before(cutoff) })
	if len(b.entries) > inboxMax {
		b.entries = b.entries[:inboxMax]
	}
	return b.save()
}

// MarkShown records that macOS showed id as a banner.
func (b *Inbox) MarkShown(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if k := slices.IndexFunc(b.entries, func(e Entry) bool { return e.ID == id }); k >= 0 && !b.entries[k].Shown {
		b.entries[k].Shown = true
		_ = b.save()
	}
}

// MarkRead marks one notification read.
func (b *Inbox) MarkRead(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := slices.IndexFunc(b.entries, func(e Entry) bool { return e.ID == id })
	if k < 0 {
		return ErrNoEntry
	}
	if b.entries[k].Read {
		return nil
	}
	b.entries[k].Read = true
	return b.save()
}

// MarkAllRead marks every notification read.
func (b *Inbox) MarkAllRead() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	changed := false
	for k := range b.entries {
		if !b.entries[k].Read {
			b.entries[k].Read, changed = true, true
		}
	}
	if !changed {
		return nil
	}
	return b.save()
}

// List returns every notification, newest first.
func (b *Inbox) List() []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.entries)
}

// Unread counts the notifications not read yet.
func (b *Inbox) Unread() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.entries {
		if !e.Read {
			n++
		}
	}
	return n
}

// save writes the inbox atomically. Caller holds mu.
func (b *Inbox) save() error {
	if b.path == "" {
		return nil
	}
	raw, err := json.Marshal(inboxData{Version: 1, Seq: b.seq, Entries: b.entries})
	if err != nil {
		return err
	}
	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("save notifications: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "notifications-*.tmp")
	if err != nil {
		return fmt.Errorf("save notifications: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save notifications: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save notifications: %w", err)
	}
	if err := os.Rename(tmp.Name(), b.path); err != nil {
		return fmt.Errorf("save notifications: %w", err)
	}
	return nil
}
