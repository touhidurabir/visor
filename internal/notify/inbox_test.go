package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/issues"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func exitIssue(id string, count int, at time.Time) issues.Issue {
	return issues.Issue{ID: id, Kind: issues.KindUnexpectedExit, Process: "flaky:flaky", Config: "/c/flaky.ini",
		Message: "Exited with status 2", OpenedAt: at, LastAt: at, Count: count, Status: issues.StatusOpen}
}

func TestInboxRecordAndRepeat(t *testing.T) {
	c := &fakeClock{time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	b := OpenInbox(t.TempDir(), c.now)
	e, err := b.Record(exitIssue("i1", 1, c.t))
	if err != nil || e.Read || e.Title != "flaky exited unexpectedly" || e.Detail != "It exited with status 2." || e.Config != "/c/flaky.ini" {
		t.Fatalf("Record = %+v, %v", e, err)
	}
	c.t = c.t.Add(time.Minute)
	other, _ := b.Record(issues.Issue{ID: "i2", Kind: issues.KindFatal, Process: "rs:rs", Message: "can't find command"})
	if b.Unread() != 2 || b.List()[0].ID != other.ID {
		t.Fatalf("unread %d, list %+v", b.Unread(), b.List())
	}

	// A repeat while unread updates that entry and moves it to the top.
	c.t = c.t.Add(time.Minute)
	r, _ := b.Record(exitIssue("i1", 3, c.t))
	if r.ID != e.ID || r.Count != 3 || !strings.Contains(r.Detail, "3rd time") {
		t.Fatalf("repeat = %+v", r)
	}
	if l := b.List(); len(l) != 2 || l[0].ID != e.ID || !l[0].At.Equal(c.t) {
		t.Fatalf("after repeat list = %+v", l)
	}

	// Once read, the next repeat is a new entry.
	if err := b.MarkRead(e.ID); err != nil {
		t.Fatal(err)
	}
	r2, _ := b.Record(exitIssue("i1", 4, c.t))
	if r2.ID == e.ID || b.Unread() != 2 || len(b.List()) != 3 {
		t.Fatalf("after read repeat = %+v, unread %d", r2, b.Unread())
	}
	if err := b.MarkAllRead(); err != nil || b.Unread() != 0 {
		t.Fatalf("MarkAllRead: %v, unread %d", err, b.Unread())
	}
	if err := b.MarkRead("nope"); err == nil {
		t.Error("MarkRead of an unknown entry succeeded")
	}
}

func TestInboxPersistsAndShown(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	b := OpenInbox(dir, c.now)
	e, _ := b.Record(exitIssue("i1", 1, c.t))
	b.MarkShown(e.ID)
	again := OpenInbox(dir, c.now)
	l := again.List()
	if len(l) != 1 || l[0].ID != e.ID || !l[0].Shown || l[0].Read || again.Unread() != 1 {
		t.Fatalf("reloaded = %+v", l)
	}
	// IDs stay unique after a reload.
	n, _ := again.Record(issues.Issue{ID: "i9", Kind: issues.KindFatal, Process: "x:x"})
	if n.ID == e.ID {
		t.Fatal("reloaded inbox reused an ID")
	}
}

func TestInboxRecoveredIsRead(t *testing.T) {
	c := &fakeClock{time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	b := OpenInbox("", c.now)
	down := issues.Issue{ID: "u", Kind: issues.KindUnreachable, OpenedAt: c.t.Add(-8 * time.Second), ClosedAt: c.t, Status: issues.StatusResolvedAuto}
	e, _ := b.Recovered(down)
	if !e.Read || e.Title != "supervisord is responding again" || e.Detail != "It was unreachable for 8 seconds." || b.Unread() != 0 || e.Level() != Info {
		t.Fatalf("Recovered = %+v", e)
	}
}

func TestInboxKeepsNewestAndRecent(t *testing.T) {
	dir := t.TempDir()
	c := &fakeClock{time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	b := OpenInbox(dir, c.now)
	b.Record(issues.Issue{ID: "old", Kind: issues.KindFatal, Process: "o:o"})
	c.t = c.t.Add(31 * 24 * time.Hour)
	for i := 0; i < inboxMax+5; i++ {
		b.Record(issues.Issue{ID: string(rune('a'+i%26)) + time.Duration(i).String(), Kind: issues.KindFatal, Process: "p:p"})
	}
	l := b.List()
	if len(l) != inboxMax {
		t.Fatalf("kept %d entries, want %d", len(l), inboxMax)
	}
	for _, e := range l {
		if e.IssueID == "old" {
			t.Fatal("kept an entry older than 30 days")
		}
	}
}

func TestInboxWithoutDirWritesNothing(t *testing.T) {
	wd, _ := os.Getwd()
	b := OpenInbox("", time.Now)
	b.Record(exitIssue("i1", 1, time.Now()))
	if _, err := os.Stat(filepath.Join(wd, inboxFile)); err == nil {
		t.Fatal("an inbox without a state folder wrote a file")
	}
}

func TestInboxIgnoresBrokenFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, inboxFile), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := OpenInbox(dir, time.Now)
	if len(b.List()) != 0 {
		t.Fatal("broken file produced entries")
	}
	if _, err := b.Record(exitIssue("i1", 1, time.Now())); err != nil {
		t.Fatal(err)
	}
}
