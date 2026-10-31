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
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
	"github.com/touhidurabir/visor/internal/validate"
)

// configFixture runs a sandbox supervisord with sleeper, worker and doomed,
// and a runtime on it.
func configFixture(t *testing.T) (*ConfigService, *ServerService, *Runtime, *supervisortest.Instance) {
	t.Helper()
	inst := supervisortest.Start(t, supervisortest.Sleeper, worker, doomed)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	for _, n := range []string{"sleeper:sleeper", "worker:worker", "doomed:doomed"} {
		waitRunning(t, svc, n)
	}
	return NewConfigService(rt), svc, rt, inst
}

// waitNewPID waits until a process runs with a PID other than old.
func waitNewPID(t *testing.T, svc *ServerService, fullName string, old int) {
	t.Helper()
	waitOverview(t, svc, 15*time.Second, fullName+" restarted", func(ov OverviewView) bool {
		p, _, ok := findProc(ov, fullName)
		return ok && p.State == "RUNNING" && p.PID > 0 && p.PID != old
	})
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConfigServiceReadAndRefuseOutsiders(t *testing.T) {
	cs, _, _, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, err := cs.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if v.Content != mustRead(t, p) || v.Hash != apply.Hash([]byte(v.Content)) || !v.Writable {
		t.Errorf("view = %+v", v)
	}
	outside := filepath.Join(t.TempDir(), "x.ini")
	if err := os.WriteFile(outside, []byte("[program:x]\ncommand=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Read(outside); err == nil {
		t.Error("Read of a file outside the include folder succeeded")
	}
	if _, err := cs.Save(outside, "[program:x]\ncommand=sleep 1\n", "x"); err == nil {
		t.Error("Save of a file outside the include folder succeeded")
	}
	if _, err := cs.Backups(outside); err == nil {
		t.Error("Backups of a file outside the include folder succeeded")
	}
}

func TestConfigServiceSaveBlocksOnErrors(t *testing.T) {
	cs, _, rt, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	before := mustRead(t, p)

	bad := "[program:worker]\ncommand=sleep 3600\nnumprocs=2\n" // numprocs > 1 needs process_name
	if cs := cs.Validate("worker.ini", bad, p); !validate.HasErrors(cs) {
		t.Fatalf("test content has no errors: %+v", cs)
	}
	_, err := cs.Save(p, bad, v.Hash)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	if got := mustRead(t, p); got != before {
		t.Errorf("file changed: %q", got)
	}
	if list, _ := rt.backups().List(p); len(list) != 0 {
		t.Errorf("backups = %v, want none", list)
	}
}

func TestConfigServiceSaveAppliesOnlyFileGroups(t *testing.T) {
	cs, svc, rt, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	workerPID := pidIn(t, svc, "worker:worker")
	doomedPID := pidIn(t, svc, "doomed:doomed")
	// A change in another file, not saved through the editor, must stay pending.
	inst.WriteProgram(t, supervisortest.Program{Name: "doomed", Command: "sleep 3601"})
	waitOverview(t, svc, 5*time.Second, "doomed flagged", func(OverviewView) bool { return len(changesOf(rt).Changed) == 1 })

	res, err := cs.Save(p, strings.Replace(v.Content, "sleep 3600", "sleep 3602", 1), v.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Changed, []string{"worker"}) || len(res.Added)+len(res.Removed)+len(res.Errors) != 0 {
		t.Errorf("result = %+v, want only worker changed", res)
	}
	if got := mustRead(t, p); !strings.Contains(got, "sleep 3602") {
		t.Errorf("file = %q", got)
	}
	waitNewPID(t, svc, "worker:worker", workerPID)
	if got := pidIn(t, svc, "doomed:doomed"); got != doomedPID {
		t.Errorf("doomed restarted (PID %d -> %d) though it was not in the saved file", doomedPID, got)
	}
	if got := changesOf(rt).Changed; !slices.Equal(got, []string{confFile(inst, "doomed")}) {
		t.Errorf("changed = %v, want doomed.ini still flagged and worker.ini cleared", got)
	}
	list, err := cs.Backups(p)
	if err != nil || len(list) != 1 || list[0].Content != v.Content {
		t.Errorf("backups = %+v, %v; want one with the old content", list, err)
	}
}

// Removing a program from a file removes its group from supervisord.
func TestConfigServiceSaveRemovedProgramStops(t *testing.T) {
	cs, svc, _, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	content := "[program:other]\ncommand=sleep 3600\n"
	res, err := cs.Save(p, content, v.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Removed, []string{"worker"}) || !slices.Equal(res.Added, []string{"other"}) {
		t.Errorf("result = %+v", res)
	}
	waitRunning(t, svc, "other:other")
}

func TestConfigServiceSaveRejectsStaleHash(t *testing.T) {
	cs, _, rt, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	other := v.Content + "; edited elsewhere\n"
	if err := os.WriteFile(p, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := cs.Save(p, v.Content+"; mine\n", v.Hash)
	if !errors.Is(err, apply.ErrChangedOnDisk) {
		t.Fatalf("err = %v, want ErrChangedOnDisk", err)
	}
	if got := mustRead(t, p); got != other {
		t.Errorf("the other edit was lost: %q", got)
	}
	if list, _ := rt.backups().List(p); len(list) != 0 {
		t.Errorf("backups = %v, want none", list)
	}
}

func TestConfigServiceReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anything")
	}
	cs, _, _, inst := configFixture(t)
	p := confFile(inst, "worker")
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	v, err := cs.Read(p)
	if err != nil || v.Writable {
		t.Fatalf("Read = %+v, %v; want Writable=false", v, err)
	}
	_, err = cs.Save(p, v.Content+"; x\n", v.Hash)
	if !errors.Is(err, apply.ErrNotWritable) {
		t.Errorf("err = %v, want ErrNotWritable", err)
	}
}

// Two saves from the same starting point: exactly one wins, the other is
// refused, and nothing interleaves.
func TestConfigServiceConcurrentSaves(t *testing.T) {
	cs, _, _, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = cs.Save(p, strings.Replace(v.Content, "sleep 3600", "sleep 360"+string(rune('3'+i)), 1), v.Hash)
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, apply.ErrChangedOnDisk):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d saves won, want 1 (%v)", wins, errs)
	}
}

func TestConfigServiceRestore(t *testing.T) {
	cs, svc, _, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, _ := cs.Read(p)
	if _, err := cs.Save(p, strings.Replace(v.Content, "sleep 3600", "sleep 3602", 1), v.Hash); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, svc, "worker:worker")
	list, err := cs.Backups(p)
	if err != nil || len(list) != 1 {
		t.Fatalf("backups = %+v, %v", list, err)
	}
	newPID := pidIn(t, svc, "worker:worker")

	res, err := cs.Restore(p, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Changed, []string{"worker"}) {
		t.Errorf("result = %+v", res)
	}
	if got := mustRead(t, p); got != v.Content {
		t.Errorf("file = %q, want the original", got)
	}
	waitNewPID(t, svc, "worker:worker", newPID)
	// The restore itself is backed up, so it can be undone.
	if list, _ := cs.Backups(p); len(list) != 2 {
		t.Errorf("backups after restore = %d, want 2", len(list))
	}
	if _, err := cs.Restore(p, "2000-01-01T00-00-00.000000000Z"); err == nil {
		t.Error("restoring an unknown backup succeeded")
	}
	if _, err := cs.Restore(p, "../../etc/passwd"); err == nil {
		t.Error("restoring a crafted id succeeded")
	}
}

// A file removed from supervisor comes back from its backup.
func TestConfigServiceRestoreRemovedFile(t *testing.T) {
	cs, svc, _, inst := configFixture(t)
	p := confFile(inst, "doomed")
	want := mustRead(t, p)
	if _, err := svc.RemoveFromSupervisor(p); err != nil {
		t.Fatal(err)
	}
	list, err := cs.Backups(p)
	if err != nil || len(list) != 1 || !list[0].Removed {
		t.Fatalf("backups = %+v, %v", list, err)
	}
	res, err := cs.Restore(p, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Added, []string{"doomed"}) {
		t.Errorf("result = %+v", res)
	}
	if got := mustRead(t, p); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	waitRunning(t, svc, "doomed:doomed")
}

func TestLoadedDiffUnknown(t *testing.T) {
	cs, _, rt, inst := configFixture(t)
	p := confFile(inst, "worker")
	v, err := cs.LoadedDiff(p)
	if err != nil || !v.Known || v.Loaded != v.Disk {
		t.Fatalf("loaded diff = %+v, %v; want known and equal", v, err)
	}
	if err := os.WriteFile(p, []byte(v.Disk+"; edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, _ := cs.LoadedDiff(p); !v.Known || v.Loaded == v.Disk {
		t.Errorf("after an edit: %+v, want known and different", v)
	}
	// A file Visor never saw supervisord load: unknown.
	inst.WriteProgram(t, supervisortest.Program{Name: "fresh", Command: "sleep 3600"})
	fresh := confFile(inst, "fresh")
	waitOverview(t, NewServerService(rt), 5*time.Second, "fresh seen", func(OverviewView) bool {
		cs := changesOf(rt)
		return slices.Contains(cs.Added, fresh) || slices.Contains(cs.Uncertain, fresh)
	})
	v, err = cs.LoadedDiff(fresh)
	if err != nil || v.Known || v.Loaded != "" || v.Disk == "" {
		t.Errorf("fresh file: %+v, %v; want unknown with the disk text", v, err)
	}
}

func TestConfigServiceApplyFile(t *testing.T) {
	cs, svc, rt, inst := configFixture(t)
	p := confFile(inst, "worker")
	pid := pidIn(t, svc, "worker:worker")
	inst.WriteProgram(t, supervisortest.Program{Name: "worker", Command: "sleep 3605"})
	waitOverview(t, svc, 5*time.Second, "worker flagged", func(OverviewView) bool { return len(changesOf(rt).Changed) == 1 })
	res, err := cs.ApplyFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Changed, []string{"worker"}) {
		t.Errorf("result = %+v", res)
	}
	waitNewPID(t, svc, "worker:worker", pid)
	if cs := changesOf(rt); flagged(cs) != 0 {
		t.Errorf("still flagged: %+v", cs)
	}
}

func TestConfigServiceDescribe(t *testing.T) {
	cs, _, _, _ := configFixture(t)
	ps, err := cs.Describe("[program:a]\ncommand=php artisan queue:work\n")
	if err != nil || len(ps) != 1 || ps[0].Name != "a" || ps[0].Text == "" {
		t.Errorf("describe = %+v, %v", ps, err)
	}
}
