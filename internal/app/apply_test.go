package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/issues"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

var (
	doomed = supervisortest.Program{Name: "doomed", Command: "sleep 3600"}
	worker = supervisortest.Program{Name: "worker", Command: "sleep 3600"}
)

func changesOf(rt *Runtime) config.ChangeSet {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.changes
}

func flagged(cs config.ChangeSet) int {
	return len(cs.Changed) + len(cs.Added) + len(cs.Removed) + len(cs.Uncertain)
}

func confFile(inst *supervisortest.Instance, name string) string {
	return filepath.Join(inst.Dir(), "conf.d", name+".ini")
}

func pidIn(t *testing.T, svc *ServerService, fullName string) int {
	t.Helper()
	p, _, ok := findProc(svc.Overview(), fullName)
	if !ok {
		t.Fatalf("%s not in the overview", fullName)
	}
	return p.PID
}

func waitRunning(t *testing.T, svc *ServerService, fullName string) ProcessView {
	t.Helper()
	ov := waitOverview(t, svc, 15*time.Second, fullName+" running", func(ov OverviewView) bool {
		p, _, ok := findProc(ov, fullName)
		return ok && p.State == "RUNNING" && p.PID > 0
	})
	p, _, _ := findProc(ov, fullName)
	return p
}

func TestApplyChangesAppliesAllAndMarksLoaded(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")
	oldWorker := waitRunning(t, svc, "worker:worker").PID
	sleeperPID := pidIn(t, svc, "sleeper:sleeper")

	inst.WriteProgram(t, supervisortest.Program{Name: "worker", Command: "sleep 3601"})
	inst.WriteProgram(t, supervisortest.Program{Name: "fresh", Command: "sleep 3600"})
	waitOverview(t, svc, 5*time.Second, "worker and fresh flagged", func(OverviewView) bool {
		cs := changesOf(rt)
		return len(cs.Changed) == 1 && len(cs.Added)+len(cs.Uncertain) == 1
	})

	res, err := svc.ApplyChanges(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Added, []string{"fresh"}) || !slices.Equal(res.Changed, []string{"worker"}) || len(res.Removed) != 0 || len(res.Errors) != 0 {
		t.Errorf("result = %+v", res)
	}
	if res.Removed == nil || res.Errors == nil {
		t.Error("nil lists in the result; the frontend expects arrays")
	}
	if cs := changesOf(rt); flagged(cs) != 0 {
		t.Errorf("after applying everything, still flagged: %+v", cs)
	}
	// The overview may still show the poll from before the apply, so wait
	// for the new PID rather than reading the next view once.
	waitNewPID(t, svc, "worker:worker", oldWorker)
	waitRunning(t, svc, "fresh:fresh")
	if got := pidIn(t, svc, "sleeper:sleeper"); got != sleeperPID {
		t.Errorf("sleeper PID %d -> %d", sleeperPID, got)
	}
	// Nothing left to apply: an empty result, not an error.
	if res, err := svc.ApplyChanges(nil); err != nil || len(res.Added)+len(res.Changed)+len(res.Removed) != 0 {
		t.Errorf("second apply = %+v, %v", res, err)
	}
}

func TestApplyChangesLimitsToGroups(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	oldWorker := waitRunning(t, svc, "worker:worker").PID

	inst.WriteProgram(t, supervisortest.Program{Name: "worker", Command: "sleep 3601"})
	inst.WriteProgram(t, supervisortest.Program{Name: "fresh", Command: "sleep 3600"})
	waitOverview(t, svc, 5*time.Second, "worker flagged", func(OverviewView) bool { return len(changesOf(rt).Changed) == 1 })

	res, err := svc.ApplyChanges([]string{"fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Added, []string{"fresh"}) || len(res.Changed) != 0 {
		t.Errorf("result = %+v, want only fresh added", res)
	}
	waitRunning(t, svc, "fresh:fresh")
	cs := changesOf(rt)
	if !slices.Equal(cs.Changed, []string{confFile(inst, "worker")}) {
		t.Errorf("changed = %v, want worker.ini still flagged", cs.Changed)
	}
	if slices.Contains(cs.Added, confFile(inst, "fresh")) || slices.Contains(cs.Uncertain, confFile(inst, "fresh")) {
		t.Errorf("fresh.ini still flagged: %+v", cs)
	}
	if got := pidIn(t, svc, "worker:worker"); got != oldWorker {
		t.Errorf("worker restarted (PID %d -> %d) though not asked", oldWorker, got)
	}
}

// A comment-only edit is not a change supervisord reports, but applying the
// file still records it as loaded.
func TestApplyChangesClearsCommentOnlyEdit(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "worker:worker")
	p := confFile(inst, "worker")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append([]byte("; owned by the mail team\n"), b...), 0o600); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 5*time.Second, "worker flagged", func(OverviewView) bool { return len(changesOf(rt).Changed) == 1 })
	res, err := svc.ApplyChanges([]string{"worker"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added)+len(res.Changed)+len(res.Removed) != 0 {
		t.Errorf("result = %+v, want nothing for supervisord to do", res)
	}
	if cs := changesOf(rt); flagged(cs) != 0 {
		t.Errorf("still flagged: %+v", cs)
	}
}

// A file deleted outside Visor is applied (its group removed), and its
// "removed" flag clears.
func TestApplyChangesDeletedFile(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "worker:worker")
	if err := os.Remove(confFile(inst, "worker")); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 5*time.Second, "worker.ini removed", func(OverviewView) bool { return len(changesOf(rt).Removed) == 1 })
	res, err := svc.ApplyChanges([]string{"worker"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Removed, []string{"worker"}) {
		t.Errorf("result = %+v", res)
	}
	if cs := changesOf(rt); flagged(cs) != 0 {
		t.Errorf("still flagged: %+v", cs)
	}
}

func TestRemoveFromSupervisorNotifiesNothing(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, doomed)
	stateDir := t.TempDir()
	sender := &fakeSender{}
	rt, svc, rec := notifyRuntime(t, inst, stateDir, sender, allNotify())
	waitRunning(t, svc, "doomed:doomed")
	sleeperPID := waitRunning(t, svc, "sleeper:sleeper").PID
	p := confFile(inst, "doomed")
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.RemoveFromSupervisor(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Removed, []string{"doomed"}) || len(res.Errors) != 0 {
		t.Errorf("result = %+v", res)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still in the include folder: %v", err)
	}
	list, err := rt.backups().List(p)
	if err != nil || len(list) != 1 || !list[0].Removed {
		t.Fatalf("backups = %+v, %v", list, err)
	}
	if got, err := rt.backups().Read(list[0]); err != nil || string(got) != string(content) {
		t.Errorf("backup = %q, %v", got, err)
	}
	if _, err := inst.Client().GetProcessInfo(t.Context(), "doomed"); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("doomed still in supervisord: %v", err)
	}
	waitOverview(t, svc, 5*time.Second, "doomed gone", func(ov OverviewView) bool {
		_, _, ok := findProc(ov, "doomed:doomed")
		return !ok
	})
	settle(t, rec, 5)
	if cs := changesOf(rt); flagged(cs) != 0 {
		t.Errorf("flagged after removing: %+v", cs)
	}
	if got := pidIn(t, svc, "sleeper:sleeper"); got != sleeperPID {
		t.Errorf("sleeper PID %d -> %d", sleeperPID, got)
	}
	rt.Stop() // waits for notifications in flight
	if got := sender.messages(); len(got) != 0 {
		t.Errorf("removing a program notified: %+v", got)
	}
	if got := rec.issuesOpened(); len(got) != 0 {
		t.Errorf("removing a program opened issues: %+v", got)
	}
	if got := openIssues(svc); len(got) != 0 {
		t.Errorf("open issues: %+v", got)
	}
	// History says the stop was the user's.
	hist, err := NewIssueService(rt).History("doomed:doomed", 0)
	if err != nil {
		t.Fatal(err)
	}
	stops := 0
	for _, h := range hist {
		if h.To == "STOPPED" {
			stops++
			if h.Note != "by you" {
				t.Errorf("stop noted %q, want \"by you\"", h.Note)
			}
		}
	}
	if stops == 0 {
		t.Errorf("no stop in doomed's history: %+v", hist)
	}
}

// Removing a failing program closes its open issue as resolved (the
// program is gone) and sends nothing more.
func TestRemoveFromSupervisorClosesItsIssues(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, fataler)
	sender := &fakeSender{}
	rt, svc, rec := notifyRuntime(t, inst, t.TempDir(), sender, allNotify())
	is := waitFatal(t, svc, rec)
	deadline := time.Now().Add(5 * time.Second)
	for len(sender.messages()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the FATAL issue never notified")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := svc.RemoveFromSupervisor(confFile(inst, "fataler")); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 5*time.Second, "fatal issue closed", func(OverviewView) bool { return len(openOfKind(svc, "fatal")) == 0 })
	settle(t, rec, 5)
	rt.Stop()
	if got := sender.messages(); len(got) != 1 || got[0].IssueID != is.ID {
		t.Errorf("notifications = %+v, want only the FATAL one from before", got)
	}
	all, err := NewIssueService(rt).List(IssueQuery{Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range all {
		if v.ID == is.ID && (v.Status != string(issues.StatusResolvedAuto) || v.Note != "program removed") {
			t.Errorf("issue after removal = %+v, want resolved automatically, noted program removed", v)
		}
		if v.ID != is.ID {
			t.Errorf("another issue opened: %+v", v)
		}
	}
}

// Review Focus 4 for removal: a folder Visor may not write stops nothing.
func TestRemoveFromSupervisorNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a 0500 folder")
	}
	inst := supervisortest.Start(t, supervisortest.Sleeper, doomed)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	pid := waitRunning(t, svc, "doomed:doomed").PID
	dir := filepath.Join(inst.Dir(), "conf.d")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := svc.RemoveFromSupervisor(confFile(inst, "doomed"))
	if !errors.Is(err, apply.ErrNotWritable) {
		t.Fatalf("err = %v, want ErrNotWritable", err)
	}
	info, err := inst.Client().GetProcessInfo(t.Context(), "doomed")
	if err != nil || info.State != supervisor.StateRunning || info.PID != pid {
		t.Errorf("doomed = %+v, %v; want it still running as PID %d", info, err, pid)
	}
	if list, _ := rt.backups().List(confFile(inst, "doomed")); len(list) != 0 {
		t.Errorf("orphan backups: %+v", list)
	}
}

func TestRemoveFromSupervisorUnknownFile(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")
	outside := filepath.Join(t.TempDir(), "x.ini")
	if err := os.WriteFile(outside, []byte("[program:x]\ncommand=sleep 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RemoveFromSupervisor(outside); err == nil {
		t.Error("removed a file that is not one of supervisord's configs")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("file outside the include folder touched: %v", err)
	}
}

// A file edited after startup shows up as Changed in the overview within the
// next watch check (the runtime checks on every poll), with no explicit check.
func TestRuntimeFlagsChangedFile(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "worker:worker")
	waitOverview(t, svc, 5*time.Second, "nothing flagged yet", func(ov OverviewView) bool {
		for _, c := range ov.Configs {
			if c.Changed {
				return false
			}
		}
		return true
	})

	inst.WriteProgram(t, supervisortest.Program{Name: "worker", Command: "sleep 3602"})
	waitOverview(t, svc, 5*time.Second, "worker.ini flagged", func(ov OverviewView) bool {
		for _, c := range ov.Configs {
			if c.Path == confFile(inst, "worker") && c.Changed {
				return true
			}
			if c.Path != confFile(inst, "worker") && c.Changed {
				return false
			}
		}
		return false
	})
}

func TestApplyChangesClearsFlag(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "worker:worker")

	inst.WriteProgram(t, supervisortest.Program{Name: "worker", Command: "sleep 3603"})
	waitOverview(t, svc, 5*time.Second, "worker.ini flagged", func(ov OverviewView) bool {
		for _, c := range ov.Configs {
			if c.Path == confFile(inst, "worker") && c.Changed {
				return true
			}
		}
		return false
	})
	if _, err := svc.ApplyChanges(nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range svc.Overview().Configs {
		if c.Changed || c.Uncertain {
			t.Errorf("%s still flagged after applying (Changed=%v Uncertain=%v)", c.FileName, c.Changed, c.Uncertain)
		}
	}
}

// A file added and applied outside Visor (supervisorctl update) reads as
// changed until supervisord says nothing differs: then the badge clears.
func TestCheckForChangesClearsAFileAppliedElsewhere(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")

	inst.WriteProgram(t, supervisortest.Program{Name: "elsewhere", Command: "sleep 3600"})
	waitOverview(t, svc, 5*time.Second, "elsewhere flagged", func(OverviewView) bool { return flagged(changesOf(rt)) == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := apply.Reload(ctx, inst.Client(), nil); err != nil {
		t.Fatal(err)
	}
	if cs := changesOf(rt); flagged(cs) != 1 {
		t.Fatalf("before the check: %+v", cs)
	}

	diff, err := svc.CheckForChanges()
	if err != nil || len(diff.Added)+len(diff.Changed)+len(diff.Removed) != 0 {
		t.Fatalf("CheckForChanges = %+v, %v", diff, err)
	}
	if cs := changesOf(rt); flagged(cs) != 0 {
		t.Errorf("still flagged after supervisord said nothing differs: %+v", cs)
	}
}
