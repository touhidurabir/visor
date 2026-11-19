package app

import (
	"context"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/settings"
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
