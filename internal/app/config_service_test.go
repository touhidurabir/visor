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

const newTaskContent = "[program:fresh-task]\ncommand=sleep 3600\nautorestart=unexpected\n"

func TestCreateAddsAndWatches(t *testing.T) {
	cs, svc, rt, inst := configFixture(t)
	if got, want := cs.IncludeDir(), filepath.Join(inst.Dir(), "conf.d"); got != want {
		t.Fatalf("IncludeDir = %q, want %q", got, want)
	}
	if cs.IncludeExt() != ".ini" {
		t.Errorf("IncludeExt = %q", cs.IncludeExt())
	}
	workerPID := pidIn(t, svc, "worker:worker")
	path := confFile(inst, "fresh-task")
	// A person had turned notifications off for this path once: a new config is watched again.
	if err := rt.watch.set(path, false); err != nil {
		t.Fatal(err)
	}

	res, err := cs.Create("fresh-task.ini", newTaskContent)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Added, []string{"fresh-task"}) || len(res.Changed)+len(res.Removed)+len(res.Errors) != 0 {
		t.Errorf("result = %+v, want only fresh-task added", res)
	}
	if got := mustRead(t, path); got != newTaskContent {
		t.Errorf("file = %q", got)
	}
	waitRunning(t, svc, "fresh-task:fresh-task")
	if !rt.watch.watched(path) {
		t.Error("the new config is not watched")
	}
	if got := pidIn(t, svc, "worker:worker"); got != workerPID {
		t.Errorf("worker restarted (PID %d -> %d)", workerPID, got)
	}
	if c := changesOf(rt); len(c.Added)+len(c.Changed)+len(c.Removed) != 0 {
		t.Errorf("changes = %+v, want none: the new file is recorded as loaded", c)
	}
}

func TestCreateRefuses(t *testing.T) {
	cs, _, _, inst := configFixture(t)
	existing := confFile(inst, "worker")
	before := mustRead(t, existing)

	_, err := cs.Create("worker.ini", newTaskContent)
	if !errors.Is(err, apply.ErrExists) || mustRead(t, existing) != before {
		t.Errorf("existing file: err = %v", err)
	}
	if _, err := cs.Create("fresh.ini", "[program:fresh]\ncommand=sleep 1\nnumprocs=2\n"); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("invalid: err = %v", err)
	}
	if _, err := cs.Create("fresh.conf", newTaskContent); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("outside the include pattern: err = %v", err)
	}
	for _, bad := range []string{"", "../x.ini", "sub/x.ini", ".hidden.ini"} {
		if _, err := cs.Create(bad, newTaskContent); err == nil {
			t.Errorf("Create(%q) succeeded", bad)
		}
	}
	// A program name another file defines is a duplicate.
	if _, err := cs.Create("dup.ini", strings.Replace(newTaskContent, "fresh-task", "worker", 1)); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("duplicate name: err = %v", err)
	}
	for _, n := range []string{"fresh.ini", "fresh.conf", "dup.ini"} {
		if _, err := os.Stat(filepath.Join(cs.IncludeDir(), n)); err == nil {
			t.Errorf("%s was written", n)
		}
	}
}

func TestCopyDraft(t *testing.T) {
	dir := t.TempDir()
	confd, extra := filepath.Join(dir, "conf.d"), filepath.Join(dir, "extra")
	for _, d := range []string{confd, extra, filepath.Join(confd, "mailer-copy.ini")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const mailer = "; mail jobs\n[program:mailer-worker]\ncommand=sleep 3600\nstdout_logfile=/tmp/mailer-worker.log\n\n" +
		"[program:mailer-digest]\ncommand=sleep 3600\n"
	mailerPath := filepath.Join(confd, "mailer.ini")
	write(mailerPath, mailer)
	// A program and a group elsewhere already have the first names.
	write(filepath.Join(confd, "taken.ini"), "[program:mailer-worker-copy]\ncommand=x\n[group:mailer-digest-copy]\nprograms=mailer-worker-copy\n")
	xPath := filepath.Join(extra, "x.conf")
	write(xPath, "[program:x]\ncommand=x\n")
	main := filepath.Join(dir, "supervisord.conf")
	write(main, "[supervisord]\n")
	rt := offlineRuntime(t, main, filepath.Join(confd, "*.ini"), filepath.Join(extra, "*.conf"))
	cs := NewConfigService(rt)
	before, _ := os.ReadDir(confd)

	v, err := cs.CopyDraft(mailerPath)
	if err != nil {
		t.Fatal(err)
	}
	// mailer-copy.ini is a folder in the include dir, so it is taken too.
	if v.FileName != "mailer-copy-2.ini" {
		t.Errorf("file name = %q", v.FileName)
	}
	want := strings.NewReplacer("[program:mailer-worker]", "[program:mailer-worker-copy-2]",
		"[program:mailer-digest]", "[program:mailer-digest-copy-2]").Replace(mailer)
	if v.Content != want {
		t.Errorf("content = %q, want %q", v.Content, want)
	}
	wantRenames := []config.Rename{
		{Kind: "program", From: "mailer-worker", To: "mailer-worker-copy-2"},
		{Kind: "program", From: "mailer-digest", To: "mailer-digest-copy-2"},
	}
	if !slices.Equal(v.Renames, wantRenames) {
		t.Errorf("renames = %+v", v.Renames)
	}
	if len(v.Doubts) != 1 || v.Doubts[0].Key != "stdout_logfile" || v.Doubts[0].Line != 4 {
		t.Errorf("doubts = %+v", v.Doubts)
	}
	// The draft passes the name checks as a new file.
	for _, c := range cs.Validate(v.FileName, v.Content, "") {
		if c.Level == validate.Error && strings.Contains(c.Message, "already exists") {
			t.Errorf("the draft reuses a name: %+v", c)
		}
	}
	if after, _ := os.ReadDir(confd); len(after) != len(before) {
		t.Errorf("CopyDraft wrote to the include folder: %v", after)
	}
	if got := mustRead(t, mailerPath); got != mailer {
		t.Errorf("original changed: %q", got)
	}

	// From another include pattern, the copy takes the include folder's extension.
	if v, err := cs.CopyDraft(xPath); err != nil || v.FileName != "x-copy.ini" {
		t.Errorf("CopyDraft(x.conf) = %q, %v", v.FileName, err)
	}

	outside := filepath.Join(t.TempDir(), "o.ini")
	write(outside, "[program:o]\ncommand=x\n")
	if _, err := cs.CopyDraft(outside); err == nil {
		t.Error("CopyDraft of a file outside the include folder succeeded")
	}
	if _, err := cs.CopyDraft(filepath.Join(confd, "missing.ini")); err == nil {
		t.Error("CopyDraft of a missing file succeeded")
	}
	if _, err := cs.CopyDraft(main); err == nil {
		t.Error("CopyDraft of supervisord.conf succeeded")
	}
}

// The copy's file name matches the include folder's own pattern, so
// supervisord loads it.
func TestCopyDraftMatchesTheIncludePattern(t *testing.T) {
	dir := t.TempDir()
	confd := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confd, 0o700); err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(confd, "web-svc.ini")
	if err := os.WriteFile(web, []byte("[program:web]\ncommand=sleep 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "supervisord.conf")
	if err := os.WriteFile(main, []byte("[supervisord]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Another folder's pattern does not count for this one.
	cs := NewConfigService(offlineRuntime(t, main, filepath.Join(confd, "*-svc.ini"), filepath.Join(dir, "other", "*.ini")))
	v, err := cs.CopyDraft(web)
	if err != nil {
		t.Fatal(err)
	}
	if v.FileName != "web-copy-svc.ini" || len(v.Doubts) != 0 {
		t.Errorf("CopyDraft = %q, doubts %+v; want web-copy-svc.ini and none", v.FileName, v.Doubts)
	}
	for _, c := range cs.Validate(v.FileName, v.Content, "") {
		if c.Level == validate.Error {
			t.Errorf("the draft fails a check: %+v", c)
		}
	}
}

func TestFolderViewForTheReadOnlyBanner(t *testing.T) {
	dir := t.TempDir()
	root := func(os.FileInfo) string { return "root" }
	if v := folderView(dir, os.Stat, func(string) bool { return true }, root); !v.Writable || v.Owner != "" || v.Dir != dir {
		t.Errorf("writable = %+v", v)
	}
	if v := folderView(dir, os.Stat, func(string) bool { return false }, root); v.Writable || v.Owner != "root" || v.Missing {
		t.Errorf("root's folder = %+v", v)
	}
	if v := folderView(filepath.Join(dir, "gone"), os.Stat, func(string) bool { return true }, root); v.Writable || !v.Missing {
		t.Errorf("missing = %+v", v)
	}
	if v := folderView("", os.Stat, func(string) bool { return false }, root); !v.Writable {
		t.Errorf("no include folder = %+v", v)
	}
	fi, _ := os.Stat(dir)
	if ownerName(fi) == "" {
		t.Error("ownerName of a temp dir is empty")
	}
}
