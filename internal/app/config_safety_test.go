package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// offlineRuntime is a runtime that never connects, on a config whose
// include glob is glob.
func offlineRuntime(t *testing.T, mainConf string, globs ...string) *Runtime {
	t.Helper()
	rt := NewRuntime(RuntimeOptions{ConfigPath: mainConf, StateDir: t.TempDir()})
	rt.d = config.Discovery{ConfigPath: mainConf, IncludeGlobs: globs}
	return rt
}

// I-1: an include glob that also matches supervisord.conf (directly or
// through a symlink) never lets Visor read, write, back up, restore,
// create or remove the main config.
func TestConfigServiceRefusesMainConfig(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "supervisord.conf")
	const content = "[supervisord]\nlogfile=/tmp/x.log\n\n[program:x]\ncommand=sleep 1\n"
	if err := os.WriteFile(main, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "conf.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "conf.d", "main.conf")
	if err := os.Symlink("../supervisord.conf", link); err != nil {
		t.Fatal(err)
	}
	rt := offlineRuntime(t, main, filepath.Join(dir, "*.conf"), filepath.Join(dir, "conf.d", "*.conf"))
	cs, svc := NewConfigService(rt), NewServerService(rt)
	for _, p := range []string{main, link} {
		if _, err := cs.Read(p); !errors.Is(err, ErrMainConfig) {
			t.Errorf("Read(%s): err = %v, want ErrMainConfig", p, err)
		}
		if _, err := cs.LoadedDiff(p); !errors.Is(err, ErrMainConfig) {
			t.Errorf("LoadedDiff(%s): err = %v, want ErrMainConfig", p, err)
		}
		if _, err := cs.Save(p, "[program:x]\ncommand=sleep 2\n", apply.Hash([]byte(content))); !errors.Is(err, ErrMainConfig) {
			t.Errorf("Save(%s): err = %v, want ErrMainConfig", p, err)
		}
		if _, err := cs.Backups(p); !errors.Is(err, ErrMainConfig) {
			t.Errorf("Backups(%s): err = %v, want ErrMainConfig", p, err)
		}
		if _, err := cs.Restore(p, "2026-11-01T09-30-00.000000000Z"); !errors.Is(err, ErrMainConfig) {
			t.Errorf("Restore(%s): err = %v, want ErrMainConfig", p, err)
		}
		if _, err := cs.ApplyFile(p); !errors.Is(err, ErrMainConfig) {
			t.Errorf("ApplyFile(%s): err = %v, want ErrMainConfig", p, err)
		}
		if _, err := svc.RemoveFromSupervisor(p); !errors.Is(err, ErrMainConfig) {
			t.Errorf("RemoveFromSupervisor(%s): err = %v, want ErrMainConfig", p, err)
		}
	}
	// A file removed meanwhile and created again by name in the main
	// config's folder is the main config too.
	if _, err := cs.Create("supervisord.conf", "[program:y]\ncommand=sleep 1\n"); !errors.Is(err, ErrMainConfig) {
		t.Errorf("Create: err = %v, want ErrMainConfig", err)
	}
	if got := mustRead(t, main); got != content {
		t.Errorf("main config changed: %q", got)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink touched: %v %v", fi, err)
	}
}

// failReread is a value Visor's checks pass but supervisord refuses: it
// can't expand an environment variable it does not have.
const failReread = "[program:worker]\ncommand=%(ENV_VISOR_TEST_NO_SUCH_VAR)s/run\n"

// I-2: when supervisord refuses the saved file, Visor puts the previous
// version back, says so, and supervisord keeps running what it ran.
func TestSaveRejectedBySupervisordRestoresPrevious(t *testing.T) {
	cs, svc, rt, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	pid := pidIn(t, svc, "worker:worker")

	_, err := cs.Save(p, failReread, v.Hash)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
	t.Logf("refused: %v", err)
	for _, want := range []string{"ENV_VISOR_TEST_NO_SUCH_VAR", "in section 'program:worker'", "put the previous version back", "still in the editor"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to say %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "ENV_HOME") {
		t.Errorf("err = %q lists supervisord's whole environment", err)
	}
	if got := mustRead(t, p); got != v.Content {
		t.Errorf("file = %q, want the previous version back", got)
	}
	if list, _ := rt.backups().List(p); len(list) != 0 {
		t.Errorf("backups = %+v, want none (nothing changed in the end)", list)
	}
	if got := pidIn(t, svc, "worker:worker"); got != pid {
		t.Errorf("worker PID %d -> %d", pid, got)
	}
	// supervisord can re-read again: every other apply still works.
	if _, err := svc.CheckForChanges(); err != nil {
		t.Errorf("supervisord still can't re-read: %v", err)
	}

	// A new file supervisord refuses is removed again.
	_, err = cs.Create("broken.ini", strings.Replace(failReread, "worker", "broken", 1))
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "removed the new file") {
		t.Fatalf("Create: err = %v, want ErrRejected saying the file was removed", err)
	}
	if _, err := os.Lstat(confFile(inst, "broken")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("refused new file still there: %v", err)
	}
	if _, err := svc.CheckForChanges(); err != nil {
		t.Errorf("supervisord still can't re-read: %v", err)
	}
}

// I-3: Save applies the groups supervisord loaded from the file, not only
// the ones on disk before the save: a program renamed elsewhere (not yet
// applied) is not left running next to its new name.
func TestSaveAppliesGroupsSupervisordLoaded(t *testing.T) {
	cs, svc, rt, inst := configFixture(t)
	p := confFile(inst, "worker")
	renamed := strings.Replace(mustRead(t, p), "[program:worker]", "[program:worker2]", 1)
	if err := os.WriteFile(p, []byte(renamed), 0o600); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 5*time.Second, "worker.ini flagged", func(OverviewView) bool { return len(changesOf(rt).Changed) == 1 })

	v, err := cs.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.Save(p, strings.Replace(v.Content, "sleep 3600", "sleep 3601", 1), v.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Added, []string{"worker2"}) || !slices.Equal(res.Removed, []string{"worker"}) {
		t.Errorf("result = %+v, want worker2 added and worker removed", res)
	}
	if _, err := inst.Client().GetProcessInfo(t.Context(), "worker"); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("the old worker still runs: %v", err)
	}
	waitRunning(t, svc, "worker2:worker2")
}

// I-6: every write, apply, removal and daemon restart waits for the one
// in progress, whichever service (window, tray) started it.
func TestConfigWritesShareOneLock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.ini")
	if err := os.WriteFile(p, []byte("[program:a]\ncommand=sleep 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := offlineRuntime(t, filepath.Join(dir, "supervisord.conf"), filepath.Join(dir, "*.ini"))
	cs, svc := NewConfigService(rt), NewServerService(rt)
	calls := map[string]func(){
		"ConfigService.Save":                 func() { _, _ = cs.Save(p, "[program:a]\ncommand=sleep 2\n", "x") },
		"ConfigService.Restore":              func() { _, _ = cs.Restore(p, "x") },
		"ConfigService.Create":               func() { _, _ = cs.Create("b.ini", "[program:b]\ncommand=sleep 1\n") },
		"ConfigService.ApplyFile":            func() { _, _ = cs.ApplyFile(p) },
		"ServerService.ApplyChanges":         func() { _, _ = svc.ApplyChanges(nil) },
		"ServerService.RemoveFromSupervisor": func() { _, _ = svc.RemoveFromSupervisor(p) },
		"ServerService.RestartDaemon":        func() { _ = svc.RestartDaemon() },
		"ServerService.ShutdownDaemon":       func() { _ = svc.ShutdownDaemon() },
	}
	for name, call := range calls {
		rt.applyMu.Lock()
		done := make(chan struct{})
		go func() {
			call()
			close(done)
		}()
		select {
		case <-done:
			t.Errorf("%s ran while another write held the lock", name)
		case <-time.After(150 * time.Millisecond):
		}
		rt.applyMu.Unlock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s never finished", name)
		}
	}
}

// I-6: the tray's "Apply config changes" and an editor Save at the same
// moment: both succeed, and the saved group restarts once.
func TestConcurrentApplyAndSave(t *testing.T) {
	cs, svc, _, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	var wg sync.WaitGroup
	var saveRes, applyRes ApplyResultView
	var saveErr, applyErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		saveRes, saveErr = cs.Save(p, strings.Replace(v.Content, "sleep 3600", "sleep 3602", 1), v.Hash)
	}()
	go func() {
		defer wg.Done()
		applyRes, applyErr = svc.ApplyChanges(nil)
	}()
	wg.Wait()
	if saveErr != nil || applyErr != nil || len(saveRes.Errors)+len(applyRes.Errors) != 0 {
		t.Fatalf("save %+v %v; apply %+v %v", saveRes, saveErr, applyRes, applyErr)
	}
	restarts := 0
	for _, r := range []ApplyResultView{saveRes, applyRes} {
		for _, g := range append(append(slices.Clone(r.Changed), r.Added...), r.Removed...) {
			if g == "worker" {
				restarts++
			}
		}
	}
	if restarts != 1 {
		t.Errorf("worker applied %d times (save %+v, apply %+v), want once", restarts, saveRes, applyRes)
	}
	waitRunning(t, svc, "worker:worker")
}

// I-7: the backup is taken before anything stops: when it can't be, the
// processes keep running and the file stays.
func TestRemoveFromSupervisorBackupFailsStopsNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a 0500 folder")
	}
	inst := supervisortest.Start(t, supervisortest.Sleeper, doomed)
	stateDir := t.TempDir()
	rt, _ := startRuntime(t, confOf(inst), stateDir, 100*time.Millisecond)
	svc := NewServerService(rt)
	pid := waitRunning(t, svc, "doomed:doomed").PID
	bdir := filepath.Join(stateDir, "backups")
	if err := os.MkdirAll(bdir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bdir, 0o700) })

	if _, err := svc.RemoveFromSupervisor(confFile(inst, "doomed")); err == nil || !strings.Contains(err.Error(), "back up") {
		t.Fatalf("err = %v, want the backup failure", err)
	}
	info, err := inst.Client().GetProcessInfo(t.Context(), "doomed")
	if err != nil || info.State != supervisor.StateRunning || info.PID != pid {
		t.Errorf("doomed = %+v, %v; want it still running as PID %d", info, err, pid)
	}
	if _, err := os.Stat(confFile(inst, "doomed")); err != nil {
		t.Errorf("file gone: %v", err)
	}
}

// I-5: a removed file stays listed (with its backup) until it is back, so
// it can be restored after the Undo in the report is gone.
func TestConfigServiceRemovedFiles(t *testing.T) {
	cs, svc, _, inst := configFixture(t)
	if got, err := cs.RemovedFiles(); err != nil || len(got) != 0 {
		t.Fatalf("RemovedFiles = %+v, %v; want none", got, err)
	}
	p := confFile(inst, "doomed")
	content := mustRead(t, p)
	if _, err := svc.RemoveFromSupervisor(p); err != nil {
		t.Fatal(err)
	}
	got, err := cs.RemovedFiles()
	if err != nil || len(got) != 1 || got[0].Path != p || got[0].FileName != "doomed.ini" || got[0].BackupID == "" || got[0].At.IsZero() {
		t.Fatalf("RemovedFiles = %+v, %v; want doomed.ini", got, err)
	}
	if _, err := cs.Restore(p, got[0].BackupID); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, p) != content {
		t.Error("restored content differs")
	}
	waitRunning(t, svc, "doomed:doomed")
	if got, _ := cs.RemovedFiles(); len(got) != 0 {
		t.Errorf("RemovedFiles after restore = %+v", got)
	}
}

// M-10: applying some groups leaves a file flagged when it holds another
// change that was not applied (not in the confirmed list).
func TestApplyChangesKeepsUnappliedFileFlagged(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "worker:worker")
	p := confFile(inst, "worker")
	both := strings.Replace(mustRead(t, p), "sleep 3600", "sleep 3601", 1) + "\n[program:extra]\ncommand=sleep 3600\n"
	if err := os.WriteFile(p, []byte(both), 0o600); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 5*time.Second, "worker.ini flagged", func(OverviewView) bool { return len(changesOf(rt).Changed) == 1 })

	res, err := svc.ApplyChanges([]string{"worker"})
	if err != nil || !slices.Equal(res.Changed, []string{"worker"}) || len(res.Added) != 0 {
		t.Fatalf("result = %+v, %v; want only worker changed", res, err)
	}
	if got := changesOf(rt).Changed; !slices.Equal(got, []string{p}) {
		t.Errorf("changed = %v, want worker.ini still flagged for extra", got)
	}
	if _, err := inst.Client().GetProcessInfo(t.Context(), "extra"); !errors.Is(err, supervisor.ErrBadName) {
		t.Errorf("extra was added though it was not confirmed: %v", err)
	}
}

// M-13: a restart caused by Save is the user's: no notification, no
// unexpected-exit issue, and history notes it as by you.
func TestSaveRestartIsByYou(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker)
	sender := &fakeSender{}
	rt, svc, rec := notifyRuntime(t, inst, t.TempDir(), sender, allNotify())
	pid := waitRunning(t, svc, "worker:worker").PID
	if err := rt.watch.set(confFile(inst, "worker"), true); err != nil {
		t.Fatal(err)
	}
	cs := NewConfigService(rt)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	before := time.Now().UnixMilli()
	res, err := cs.Save(p, strings.Replace(v.Content, "sleep 3600", "sleep 3601", 1), v.Hash)
	if err != nil || !slices.Equal(res.Changed, []string{"worker"}) {
		t.Fatalf("save = %+v, %v", res, err)
	}
	waitNewPID(t, svc, "worker:worker", pid)
	settle(t, rec, 5)
	rt.Stop() // waits for notifications in flight
	if got := sender.messages(); len(got) != 0 {
		t.Errorf("saving notified: %+v", got)
	}
	if got := rec.issuesOpened(); len(got) != 0 {
		t.Errorf("saving opened issues: %+v", got)
	}
	hist, err := NewIssueService(rt).History("worker:worker", 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, h := range hist {
		if h.AtMs < before {
			continue // startup
		}
		n++
		if !strings.Contains(strings.ToLower(h.Note), "by you") {
			t.Errorf("transition %+v not noted as by you", h)
		}
	}
	if n == 0 {
		t.Errorf("no history for the save's restart: %+v", hist)
	}
}
