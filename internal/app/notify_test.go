package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/notify"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// fakeSender records the notifications Visor shows.
type fakeSender struct {
	mu   sync.Mutex
	sent []notify.Message
	err  error
}

func (f *fakeSender) Send(m notify.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeSender) messages() []notify.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]notify.Message(nil), f.sent...)
}

// fataler gives up straight away: supervisord marks it FATAL.
var fataler = supervisortest.Program{
	Name:    "fataler",
	Command: "sh -c 'exit 1'",
	Extra:   map[string]string{"startsecs": "1", "startretries": "0", "autorestart": "false"},
}

// notifyRuntime starts a Runtime on inst with sender and prefs.
func notifyRuntime(t *testing.T, inst *supervisortest.Instance, stateDir string, sender notify.Sender, prefs settings.NotifyPrefs) (*Runtime, *ServerService, *recorder) {
	t.Helper()
	s := settings.Defaults()
	s.Notify = prefs
	rt, rec := newRuntimeWith(t, RuntimeOptions{ConfigPath: confOf(inst), StateDir: stateDir, Interval: 100 * time.Millisecond, Settings: s, Notifier: sender})
	return rt, NewServerService(rt), rec
}

func allNotify() settings.NotifyPrefs {
	return settings.NotifyPrefs{Enabled: true, Fatal: true, UnexpectedExit: true, CrashLoop: true, Unreachable: true}
}

// waitFatal waits for fataler's FATAL issue and a few polls after it.
func waitFatal(t *testing.T, svc *ServerService, rec *recorder) IssueView {
	t.Helper()
	waitOverview(t, svc, 15*time.Second, "fataler issue", func(OverviewView) bool { return len(openOfKind(svc, "fatal")) == 1 })
	settle(t, rec, 5)
	return openOfKind(svc, "fatal")[0]
}

func TestFatalInWatchedConfigNotifiesOnce(t *testing.T) {
	inst := supervisortest.Start(t, fataler)
	sender := &fakeSender{}
	rt, svc, rec := notifyRuntime(t, inst, t.TempDir(), sender, allNotify())
	is := waitFatal(t, svc, rec)

	deadline := time.Now().Add(5 * time.Second)
	for !openOfKind(svc, "fatal")[0].Notified {
		if time.Now().After(deadline) {
			t.Fatal("issue never marked Notified")
		}
		time.Sleep(20 * time.Millisecond)
	}
	rt.Stop() // waits for sends in flight
	// The Issues page can list only the notified ones.
	got, err := NewIssueService(rt).List(IssueQuery{Status: "all", Notified: true})
	if err != nil || !slices.ContainsFunc(got, func(v IssueView) bool { return v.ID == is.ID }) {
		t.Errorf("notified only = %+v, %v", got, err)
	}
	for _, v := range got {
		if !v.Notified {
			t.Errorf("notified only listed %+v", v)
		}
	}
	var fatal []notify.Message
	for _, m := range sender.messages() {
		if m.IssueID == is.ID {
			fatal = append(fatal, m)
		}
	}
	if len(fatal) != 1 {
		t.Fatalf("notifications for the fatal issue = %+v, want one", sender.messages())
	}
	m := fatal[0]
	if m.Title != "🔴 fataler can't start" || m.Subtitle != "" || !strings.HasPrefix(m.Body, "fataler.ini: ") ||
		!strings.HasSuffix(m.Body, ".") || filepath.Base(m.Config) != "fataler.ini" || m.Thread != m.Config || m.Process != "fataler:fataler" {
		t.Errorf("message = %+v", m)
	}
	// The bell lists the same words, without the icon and the file name.
	entries := rt.inbox.List()
	k := slices.IndexFunc(entries, func(e notify.Entry) bool { return e.IssueID == is.ID })
	if k < 0 || entries[k].Title != "fataler can't start" || entries[k].Detail != m.Detail || m.Detail == "" {
		t.Errorf("inbox entry for %s = %+v, message detail %q", is.ID, entries, m.Detail)
	}
}

func TestUnwatchedConfigDoesNotNotify(t *testing.T) {
	inst := supervisortest.Start(t, fataler)
	stateDir := t.TempDir()
	conf := filepath.Join(inst.Dir(), "conf.d", "fataler.ini")
	b, _ := json.Marshal(watchFile{Version: 1, Files: map[string]bool{conf: false}})
	if err := os.WriteFile(filepath.Join(stateDir, "watch.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{}
	rt, svc, rec := notifyRuntime(t, inst, stateDir, sender, allNotify())
	is := waitFatal(t, svc, rec)
	if is.Config != conf {
		t.Fatalf("issue config = %q, want %q (the unwatched file)", is.Config, conf)
	}
	rt.Stop()
	if got := sender.messages(); len(got) != 0 {
		t.Errorf("unwatched config notified: %+v", got)
	}
	if openOfKind(svc, "fatal")[0].Notified {
		t.Error("issue marked Notified")
	}
	if got, _ := NewIssueService(rt).List(IssueQuery{Status: "all", Notified: true}); len(got) != 0 {
		t.Errorf("notified only = %+v, want none", got)
	}
}

func TestNotifyPrefsOffDoNotNotify(t *testing.T) {
	for name, prefs := range map[string]settings.NotifyPrefs{
		"master off": {Enabled: false, Fatal: true, UnexpectedExit: true, CrashLoop: true, Unreachable: true},
		"fatal off":  {Enabled: true, Fatal: false, UnexpectedExit: true, CrashLoop: true, Unreachable: true},
	} {
		t.Run(name, func(t *testing.T) {
			inst := supervisortest.Start(t, fataler)
			sender := &fakeSender{}
			rt, svc, rec := notifyRuntime(t, inst, t.TempDir(), sender, prefs)
			waitFatal(t, svc, rec)
			rt.Stop()
			for _, m := range sender.messages() {
				if m.Process == "fataler:fataler" && m.Title == "fataler can't start" {
					t.Errorf("notified with %s: %+v", name, m)
				}
			}
		})
	}
}

// A failed send (no notification centre, permission not given, any other
// error) leaves the issue un-notified.
func TestFailedSendDoesNotMarkNotified(t *testing.T) {
	for _, sendErr := range []error{notify.ErrUnavailable, notify.ErrNotAllowed, errors.New("boom")} {
		t.Run(sendErr.Error(), func(t *testing.T) {
			inst := supervisortest.Start(t, fataler)
			sender := &fakeSender{err: sendErr}
			rt, svc, rec := notifyRuntime(t, inst, t.TempDir(), sender, allNotify())
			waitFatal(t, svc, rec)
			rt.Stop()
			if openOfKind(svc, "fatal")[0].Notified {
				t.Error("issue marked Notified after a failed send")
			}
		})
	}
}

// A second issue for the same process and kind within a minute is held
// back; another process or kind is not.
func TestRepeatNotificationsAreLimited(t *testing.T) {
	sender := &fakeSender{}
	rt := NewRuntime(RuntimeOptions{StateDir: t.TempDir(), Notifier: sender})
	exit := func(id, proc string) issues.Issue {
		return issues.Issue{ID: id, Kind: issues.KindUnexpectedExit, Process: proc, Status: issues.StatusOpen}
	}
	rt.notifyOpened([]issues.Issue{exit("1", "flaky:flaky")}, nil)
	rt.notifyOpened([]issues.Issue{exit("2", "flaky:flaky"), exit("3", "other:other")}, nil)
	rt.notifyOpened([]issues.Issue{{ID: "4", Kind: issues.KindCrashLoop, Process: "flaky:flaky"}}, nil)
	rt.sends.Wait()
	got := map[string]bool{}
	for _, m := range sender.messages() {
		got[m.IssueID] = true
	}
	if len(got) != 3 || !got["1"] || got["2"] || !got["3"] || !got["4"] {
		t.Errorf("sent = %+v, want issues 1, 3 and 4", sender.messages())
	}
}

// A slow notification centre never holds up the poll loop.
func TestSlowSenderDoesNotBlockPolling(t *testing.T) {
	inst := supervisortest.Start(t, fataler)
	block := make(chan struct{})
	sender := &blockingSender{release: block}
	_, svc, rec := notifyRuntime(t, inst, t.TempDir(), sender, allNotify())
	t.Cleanup(func() { close(block) }) // before the Runtime's Stop, which waits for it
	waitFatal(t, svc, rec)             // polls go on while the send is stuck
	settle(t, rec, 5)
}

type blockingSender struct{ release chan struct{} }

func (b *blockingSender) Send(notify.Message) error {
	<-b.release
	return nil
}

// fakeNative is a notification centre with a set permission.
type fakeNative struct {
	fakeSender
	perm    string
	started bool
}

func (f *fakeNative) start(context.Context, func()) { f.started = true }
func (f *fakeNative) send(m notify.Message) error   { return f.Send(m) }
func (f *fakeNative) status() string                { return f.perm }

func TestNotifyService(t *testing.T) {
	for _, tc := range []struct {
		perm    string
		wantErr string
	}{
		{NotifyAllowed, ""},
		{NotifyDenied, "System Settings → Notifications → Visor"},
		{NotifyUnavailable, "packaged Visor.app"},
	} {
		native := &fakeNative{perm: tc.perm}
		svc := &NotifyService{native: native}
		if err := svc.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil || !native.started {
			t.Fatalf("%s: ServiceStartup = %v, started %v", tc.perm, err, native.started)
		}
		if got := svc.NotificationStatus(); got != tc.perm {
			t.Errorf("status = %q, want %q", got, tc.perm)
		}
		err := svc.SendTestNotification()
		sent := native.messages()
		if tc.wantErr == "" {
			if err != nil || len(sent) != 1 || sent[0].Title != "🔵 Visor notifications are on" || sent[0].ID == "" {
				t.Fatalf("%s: test = %v, sent %+v", tc.perm, err, sent)
			}
			if m := sent[0]; m.Subtitle != "" || m.Thread != testThread || !strings.Contains(m.Body, "🔴 for an error") {
				t.Errorf("%s: test notification = %+v", tc.perm, m)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) || len(sent) != 0 {
			t.Errorf("%s: test = %v, sent %+v", tc.perm, err, sent)
		}
	}
}

// The server build's notifier, and a desktop one outside Visor.app, show
// nothing and say so.
func TestNotifierUnavailableWithoutBundle(t *testing.T) {
	if application.System.IsServer() {
		svc := NewNotifyService(nil)
		if svc.NotificationStatus() != NotifyUnavailable || !errors.Is(svc.Send(notify.Message{ID: "x", Title: "x"}), notify.ErrUnavailable) {
			t.Error("server notifier is not a no-op")
		}
	}
	// A test binary has no bundle identifier, like `task dev`: starting
	// leaves notifications off instead of failing or crashing.
	svc := NewNotifyService(nil)
	if err := svc.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup = %v, want nil", err)
	}
	if got := svc.NotificationStatus(); got != NotifyUnavailable {
		t.Errorf("status without a bundle = %q", got)
	}
	if err := svc.Send(notify.Message{ID: "x", Title: "x"}); !errors.Is(err, notify.ErrUnavailable) {
		t.Errorf("send without a bundle = %v", err)
	}
}

// A notification that could not be shown does not use up the limit: the
// next issue for the same process and kind is still notified.
func TestFailedSendDoesNotUseUpTheLimit(t *testing.T) {
	sender := &fakeSender{err: notify.ErrNotAllowed}
	rt := NewRuntime(RuntimeOptions{StateDir: t.TempDir(), Notifier: sender})
	exit := func(id string) issues.Issue {
		return issues.Issue{ID: id, Kind: issues.KindUnexpectedExit, Process: "flaky:flaky", Status: issues.StatusOpen}
	}
	rt.notifyOpened([]issues.Issue{exit("1")}, nil)
	rt.sends.Wait()
	sender.mu.Lock()
	sender.err = nil
	sender.mu.Unlock()
	rt.notifyOpened([]issues.Issue{exit("2")}, nil)
	rt.sends.Wait()
	if got := sender.messages(); len(got) != 1 || got[0].IssueID != "2" {
		t.Errorf("sent = %+v, want issue 2", got)
	}
}

// Quitting waits only briefly for a notification centre that does not
// answer.
func TestStopDoesNotWaitLongForStuckSends(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	rt := NewRuntime(RuntimeOptions{StateDir: t.TempDir(), Notifier: &blockingSender{release: block}})
	rt.sendWait = 100 * time.Millisecond
	rt.notifyOpened([]issues.Issue{{ID: "1", Kind: issues.KindUnreachable, Status: issues.StatusOpen}}, nil)
	start := time.Now()
	rt.Stop()
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Stop took %v with a stuck send", d)
	}
}
