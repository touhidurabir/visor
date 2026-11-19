package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// applyElsewhere does what supervisorctl update <groups> or a deploy
// script does: supervisord loads groups without Visor.
func applyElsewhere(t *testing.T, inst *supervisortest.Instance, groups ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := apply.Reload(ctx, inst.Client(), groups); err != nil {
		t.Fatal(err)
	}
}

// While a file stays flagged, supervisord is asked again every
// ConfigRecheck: a file applied outside Visor then stops reading as changed.
func TestFlaggedFileAppliedElsewhereClearsOnItsOwn(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	rt.mu.Lock()
	rt.prefs.ConfigRecheck = 300 * time.Millisecond // below the minimum a user can set, to keep the test short
	rt.mu.Unlock()
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")

	inst.WriteProgram(t, supervisortest.Program{Name: "deployed", Command: "sleep 3600"})
	waitOverview(t, svc, 5*time.Second, "deployed.ini flagged", func(OverviewView) bool { return flagged(changesOf(rt)) == 1 })
	applyElsewhere(t, inst, "deployed")
	waitOverview(t, svc, 5*time.Second, "deployed.ini no longer flagged", func(OverviewView) bool { return flagged(changesOf(rt)) == 0 })
}

// Opening the card asks supervisord: a file it already runs is recorded as
// loaded; one it doesn't stays flagged and is confirmed.
func TestLoadedDiffAsksSupervisord(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")

	inst.WriteProgram(t, supervisortest.Program{Name: "deployed", Command: "sleep 3600"})
	inst.WriteProgram(t, supervisortest.Program{Name: "waiting", Command: "sleep 3600"})
	waitOverview(t, svc, 5*time.Second, "both flagged", func(OverviewView) bool { return flagged(changesOf(rt)) == 2 })
	applyElsewhere(t, inst, "deployed")

	cs := NewConfigService(rt)
	v, err := cs.LoadedDiff(confFile(inst, "deployed"))
	if err != nil || v.Confirmed || v.CheckError != "" {
		t.Fatalf("deployed: %+v, %v", v, err)
	}
	if rt.flagged(confFile(inst, "deployed")) {
		t.Error("deployed.ini still flagged after supervisord said it runs it")
	}
	v, err = cs.LoadedDiff(confFile(inst, "waiting"))
	if err != nil || !v.Confirmed || v.CheckError != "" {
		t.Fatalf("waiting: %+v, %v", v, err)
	}
	if !rt.flagged(confFile(inst, "waiting")) {
		t.Error("waiting.ini should stay flagged: supervisord hasn't loaded it")
	}
}

// A Read only server is never asked: asking re-reads supervisord's config.
func TestReadOnlyServerIsNotAsked(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	f := startRemoteRuntime(t, inst, func(s *settings.Server) { s.ReadOnly = true })
	waitRunning(t, f.svc, "sleeper:sleeper")

	inst.WriteProgram(t, supervisortest.Program{Name: "deployed", Command: "sleep 3600"})
	if err := f.rt.refreshChanges(); err != nil {
		t.Fatal(err)
	}
	path := confFile(inst, "deployed")
	if !f.rt.flagged(path) {
		t.Fatalf("not flagged: %+v", changesOf(f.rt))
	}
	applyElsewhere(t, inst, "deployed")
	v, err := NewConfigService(f.rt).LoadedDiff(path)
	if err != nil || v.Confirmed || v.CheckError != "" {
		t.Fatalf("LoadedDiff = %+v, %v", v, err)
	}
	if !f.rt.flagged(path) {
		t.Error("a Read only server was asked: the file is no longer flagged")
	}
}

// An [eventlistener:] section is a group too (the inventory reads only
// [program:] and [group:]): a new listener stays flagged while a file
// applied elsewhere beside it clears, from Check for changes as well.
func TestEventListenerStaysFlaggedBesideAnAppliedFile(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")

	listener := confFile(inst, "listener")
	if err := os.WriteFile(listener, []byte("[eventlistener:listen]\ncommand=sleep 3600\nevents=PROCESS_STATE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst.WriteProgram(t, supervisortest.Program{Name: "deployed", Command: "sleep 3600"})
	waitOverview(t, svc, 5*time.Second, "both flagged", func(OverviewView) bool { return flagged(changesOf(rt)) == 2 })
	applyElsewhere(t, inst, "deployed")

	diff, err := svc.CheckForChanges()
	if err != nil || len(diff.Added) != 1 || diff.Added[0] != "listen" {
		t.Fatalf("CheckForChanges = %+v, %v", diff, err)
	}
	if rt.flagged(confFile(inst, "deployed")) {
		t.Error("deployed.ini still flagged after supervisord said it runs it")
	}
	if !rt.flagged(listener) {
		t.Error("listener.ini cleared, but supervisord hasn't added the listener")
	}
}

// A differing group Visor can't place in any file might be in any flagged
// file, so none is recorded as loaded.
func TestUnplacedGroupMarksNothing(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	rt.mu.Lock()
	rt.prefs.ConfigRecheck = time.Hour // only the first ask, which happens before the outside apply
	rt.mu.Unlock()
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")

	inst.WriteProgram(t, supervisortest.Program{Name: "deployed", Command: "sleep 3600"})
	waitOverview(t, svc, 5*time.Second, "deployed flagged", func(OverviewView) bool { return flagged(changesOf(rt)) == 1 })
	applyElsewhere(t, inst, "deployed")
	rt.applyMu.Lock()
	err := rt.markInSync(changesOf(rt), supervisor.ConfigDiff{Changed: []string{"ghost"}})
	rt.applyMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !rt.flagged(confFile(inst, "deployed")) {
		t.Error("deployed.ini recorded as loaded although supervisord reported a group Visor can't place")
	}
}
