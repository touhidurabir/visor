package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func writeConf(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func disc(dir string) Discovery {
	return Discovery{IncludeGlobs: []string{filepath.Join(dir, "*.conf")}}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "inventory", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInventoryProgramsAndNumprocs(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "b.conf", fixture(t, "workers.conf"))
	writeConf(t, dir, "a.conf", "[program:solo]\ncommand=sleep 1\n")
	inv, err := LoadInventory(disc(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Files) != 2 || filepath.Base(inv.Files[0].Path) != "a.conf" {
		t.Fatalf("files not sorted: %+v", inv.Files)
	}
	if inv.Files[0].Hash == "" || inv.Files[0].ModTime.IsZero() {
		t.Fatalf("hash/modtime unset: %+v", inv.Files[0])
	}
	solo := inv.Files[0].Programs[0]
	if solo.Name != "solo" || solo.Group != "solo" || solo.NumProcs != 1 {
		t.Fatalf("solo = %+v", solo)
	}
	w := inv.Files[1].Programs[0]
	if w.Name != "worker" || w.Group != "worker" || w.NumProcs != 3 {
		t.Fatalf("worker = %+v", w)
	}
	if p, ok := inv.FileForGroup("worker"); !ok || filepath.Base(p) != "b.conf" {
		t.Fatalf("FileForGroup(worker) = %q, %v", p, ok)
	}
	if _, ok := inv.FileForGroup("nope"); ok {
		t.Fatal("unknown group found")
	}
}

func TestInventoryGroupSection(t *testing.T) {
	dir := t.TempDir()
	p := writeConf(t, dir, "shop.conf", fixture(t, "shop.conf"))
	writeConf(t, dir, "other.conf", "[program:c]\ncommand=true\n")
	inv, err := LoadInventory(disc(dir))
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]string{}
	for _, f := range inv.Files {
		for _, pr := range f.Programs {
			groups[pr.Name] = pr.Group
		}
	}
	want := map[string]string{"a": "shop", "b": "shop", "c": "c"}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %v, want %v", groups, want)
	}
	if got, ok := inv.FileForGroup("shop"); !ok || got != p {
		t.Fatalf("FileForGroup(shop) = %q, %v", got, ok)
	}
	if got := inv.ExpectedExit("shop:a"); !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("ExpectedExit(shop:a) = %v", got)
	}
}

func TestInventoryGroupInDifferentFile(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "1.conf", "[program:a]\ncommand=true\n")
	g := writeConf(t, dir, "2.conf", "[group:g]\nprograms = a , z\n")
	inv, _ := LoadInventory(disc(dir))
	if inv.Files[0].Programs[0].Group != "g" {
		t.Fatalf("cross-file group not applied: %+v", inv.Files[0].Programs)
	}
	if p, ok := inv.FileForGroup("g"); !ok || p != g {
		t.Fatalf("FileForGroup = %q %v", p, ok)
	}
}

func TestInventoryExitCodes(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "x.conf", fixture(t, "workers.conf"))
	inv, _ := LoadInventory(disc(dir))
	cases := map[string][]int{
		"worker:worker_00": {0, 3},
		"worker:worker_02": {0, 3},
		"worker:worker_03": {0}, // outside numprocs
		"plain:plain":      {0, 1, 2},
		"missing:missing":  {0},
		"garbage":          {0},
	}
	for name, want := range cases {
		if got := inv.ExpectedExit(name); !reflect.DeepEqual(got, want) {
			t.Errorf("ExpectedExit(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestInventoryBadFileListedWithErr(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "bad.conf", "[program:x\ncommand=true\n")
	writeConf(t, dir, "good.conf", "[program:y]\ncommand=true\n")
	inv, err := LoadInventory(disc(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Files) != 2 || inv.Files[0].Err == nil || inv.Files[1].Err != nil {
		t.Fatalf("files = %+v", inv.Files)
	}
	if len(inv.Files[1].Programs) != 1 {
		t.Fatal("good file lost")
	}
}

func TestInventoryDedupAcrossGlobs(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "a.conf", "[program:a]\ncommand=true\n")
	d := Discovery{IncludeGlobs: []string{filepath.Join(dir, "*.conf"), filepath.Join(dir, "a.*")}}
	inv, _ := LoadInventory(d)
	if len(inv.Files) != 1 {
		t.Fatalf("files = %d", len(inv.Files))
	}
}

func newWatchFixture(t *testing.T) (dir, state string, d Discovery) {
	t.Helper()
	dir = t.TempDir()
	state = filepath.Join(t.TempDir(), "state")
	d = disc(dir)
	d.PidFile = writeConf(t, t.TempDir(), "supervisord.pid", "1\n")
	return
}

func chtimes(t *testing.T, p string, tm time.Time) {
	t.Helper()
	if err := os.Chtimes(p, tm, tm); err != nil {
		t.Fatal(err)
	}
}

func TestWatcherFirstSightOlderThanPid(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	now := time.Now()
	chtimes(t, d.PidFile, now)
	f := writeConf(t, dir, "a.conf", "[program:a]\ncommand=true\n")
	chtimes(t, f, now.Add(-2*time.Hour))
	w := NewWatcher(d, state)
	cs, err := w.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Changed)+len(cs.Added)+len(cs.Removed)+len(cs.Uncertain) != 0 {
		t.Fatalf("cs = %+v", cs)
	}
	if b, ok := w.Loaded(f); !ok || string(b) != "[program:a]\ncommand=true\n" {
		t.Fatalf("Loaded = %q %v", b, ok)
	}
	// edit afterwards (newer mtime) is a change, not uncertain
	writeConf(t, dir, "a.conf", "[program:a]\ncommand=false\n")
	cs, _ = w.Check()
	if !reflect.DeepEqual(cs.Changed, []string{f}) || len(cs.Uncertain) != 0 {
		t.Fatalf("cs = %+v", cs)
	}
	if fi, err := os.Stat(filepath.Join(state, "loaded.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("loaded.json: %v %v", fi, err)
	}
	if fi, _ := os.Stat(state); fi.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode %v", fi.Mode())
	}
}

func TestWatcherFirstSightNewerIsUncertain(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	now := time.Now()
	chtimes(t, d.PidFile, now.Add(-time.Hour))
	f := writeConf(t, dir, "a.conf", "[program:a]\ncommand=true\n")
	chtimes(t, f, now)
	w := NewWatcher(d, state)
	cs, _ := w.Check()
	if !reflect.DeepEqual(cs.Uncertain, []string{f}) {
		t.Fatalf("cs = %+v", cs)
	}
	if _, ok := w.Loaded(f); ok {
		t.Fatal("uncertain file must not be snapshotted")
	}
}

func TestWatcherNoPidfileIsUncertain(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	d.PidFile = ""
	f := writeConf(t, dir, "a.conf", "x")
	chtimes(t, f, time.Now().Add(-time.Hour))
	cs, _ := NewWatcher(d, state).Check()
	if !reflect.DeepEqual(cs.Uncertain, []string{f}) {
		t.Fatalf("cs = %+v", cs)
	}
}

func TestWatcherDetectsChangeAddRemove(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	now := time.Now()
	chtimes(t, d.PidFile, now)
	a := writeConf(t, dir, "a.conf", "[program:a]\ncommand=1\n")
	b := writeConf(t, dir, "b.conf", "[program:b]\ncommand=1\n")
	chtimes(t, a, now.Add(-time.Hour))
	chtimes(t, b, now.Add(-time.Hour))
	w := NewWatcher(d, state)
	if cs, _ := w.Check(); len(cs.Uncertain) != 0 {
		t.Fatalf("cs = %+v", cs)
	}
	writeConf(t, dir, "a.conf", "[program:a]\ncommand=2\n")
	c := writeConf(t, dir, "c.conf", "[program:c]\ncommand=1\n")
	os.Remove(b)
	cs, err := w.Check()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cs.Changed, []string{a}) || !reflect.DeepEqual(cs.Added, []string{c}) ||
		!reflect.DeepEqual(cs.Removed, []string{b}) || len(cs.Uncertain) != 0 {
		t.Fatalf("cs = %+v", cs)
	}
	// applying clears everything
	if err := w.MarkLoaded(a, b); err != nil {
		t.Fatal(err)
	}
	cs, _ = w.Check()
	if !reflect.DeepEqual(cs.Added, []string{c}) || len(cs.Changed)+len(cs.Removed) != 0 {
		t.Fatalf("after MarkLoaded(a,b): %+v", cs)
	}
	if err := w.MarkLoaded(c); err != nil {
		t.Fatal(err)
	}
	cs, _ = w.Check()
	if len(cs.Added)+len(cs.Changed)+len(cs.Removed)+len(cs.Uncertain) != 0 {
		t.Fatalf("after MarkLoaded(c): %+v", cs)
	}
}

func TestWatcherMarkLoadedPersists(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	d.PidFile = ""
	f := writeConf(t, dir, "a.conf", "v1")
	w := NewWatcher(d, state)
	if err := w.MarkLoaded(f); err != nil {
		t.Fatal(err)
	}
	w2 := NewWatcher(d, state)
	if b, ok := w2.Loaded(f); !ok || string(b) != "v1" {
		t.Fatalf("Loaded = %q %v", b, ok)
	}
	cs, _ := w2.Check()
	if len(cs.Changed)+len(cs.Uncertain)+len(cs.Added) != 0 {
		t.Fatalf("cs = %+v", cs)
	}
	for _, e := range mustReadDir(t, dir) {
		if e != "a.conf" {
			t.Fatalf("stray file in include dir: %s", e)
		}
	}
}

func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestWatcherRefreshesAfterDaemonRestart(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	now := time.Now()
	chtimes(t, d.PidFile, now.Add(-time.Hour))
	f := writeConf(t, dir, "a.conf", "v1")
	chtimes(t, f, now.Add(-2*time.Hour))
	w := NewWatcher(d, state)
	if cs, _ := w.Check(); len(cs.Changed)+len(cs.Uncertain) != 0 {
		t.Fatalf("cs = %+v", cs)
	}
	writeConf(t, dir, "a.conf", "v2")
	chtimes(t, f, now.Add(time.Minute))
	if cs, _ := w.Check(); !reflect.DeepEqual(cs.Changed, []string{f}) {
		t.Fatalf("want Changed, got %+v", cs)
	}
	// supervisord restarts after the edit
	chtimes(t, d.PidFile, now.Add(time.Hour))
	cs, _ := w.Check()
	if len(cs.Changed)+len(cs.Uncertain)+len(cs.Added)+len(cs.Removed) != 0 {
		t.Fatalf("after restart: %+v", cs)
	}
	if b, _ := w.Loaded(f); string(b) != "v2" {
		t.Fatalf("Loaded = %q", b)
	}
	// persisted
	if b, _ := NewWatcher(d, state).Loaded(f); string(b) != "v2" {
		t.Fatalf("persisted Loaded = %q", b)
	}
}

func TestWatcherEditAfterRestartStillChanged(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	now := time.Now()
	chtimes(t, d.PidFile, now.Add(-time.Hour))
	f := writeConf(t, dir, "a.conf", "v1")
	chtimes(t, f, now.Add(-2*time.Hour))
	w := NewWatcher(d, state)
	w.Check()
	// restart, then an edit after it
	chtimes(t, d.PidFile, now.Add(time.Hour))
	writeConf(t, dir, "a.conf", "v2")
	chtimes(t, f, now.Add(2*time.Hour))
	cs, _ := w.Check()
	if len(cs.Changed)+len(cs.Uncertain) != 1 {
		t.Fatalf("edit after restart silently accepted: %+v", cs)
	}
	if b, _ := w.Loaded(f); string(b) != "v1" {
		t.Fatalf("snapshot overwritten: %q", b)
	}
}

func TestWatcherConcurrentCheckAndMarkLoaded(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	var paths []string
	for _, n := range []string{"a", "b", "c"} {
		paths = append(paths, writeConf(t, dir, n+".conf", n))
	}
	w := NewWatcher(d, state)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if i%2 == 0 {
					if _, err := w.Check(); err != nil {
						t.Error(err)
					}
				} else {
					if err := w.MarkLoaded(paths[j%3]); err != nil {
						t.Error(err)
					}
					w.Loaded(paths[0])
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestInventoryLookup(t *testing.T) {
	dir := t.TempDir()
	workers := writeConf(t, dir, "w.conf", fixture(t, "workers.conf"))
	writeConf(t, dir, "1.conf", "[program:a]\ncommand=true\n")
	writeConf(t, dir, "2.conf", "[group:g]\nprograms = a\n")
	one := filepath.Join(dir, "1.conf")
	inv, err := LoadInventory(disc(dir))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		full, path, program string
		ok                  bool
	}{
		{"worker:worker_01", workers, "worker", true},
		{"plain:plain", workers, "plain", true},
		{"g:a", one, "a", true}, // program lives in 1.conf, its [group:g] in 2.conf
		{"worker:worker_03", "", "", false},
		{"a:a", "", "", false}, // a belongs to g, not to a group of its own
		{"garbage", "", "", false},
	}
	for _, c := range cases {
		path, prog, ok := inv.Lookup(c.full)
		if ok != c.ok || path != c.path || prog.Name != c.program {
			t.Errorf("Lookup(%q) = %q, %q, %v; want %q, %q, %v", c.full, path, prog.Name, ok, c.path, c.program, c.ok)
		}
	}
}

func TestInventoryAutorestart(t *testing.T) {
	dir := t.TempDir()
	writeConf(t, dir, "a.conf", "[program:always]\ncommand=true\nautorestart = TRUE\n\n"+
		"[program:never]\ncommand=true\nautorestart=false\n\n[program:plain]\ncommand=true\n")
	inv, err := LoadInventory(disc(dir))
	if err != nil {
		t.Fatal(err)
	}
	for full, want := range map[string]string{
		"always:always": "true",
		"never:never":   "false",
		"plain:plain":   "unexpected", // supervisord's default
		"gone:gone":     "unexpected", // unknown process
	} {
		if got := inv.Autorestart(full); got != want {
			t.Errorf("Autorestart(%q) = %q, want %q", full, got, want)
		}
	}
}

func TestWatcherDropsRemovedFilesAfterDaemonRestart(t *testing.T) {
	dir, state, d := newWatchFixture(t)
	now := time.Now()
	chtimes(t, d.PidFile, now.Add(-time.Hour))
	f := writeConf(t, dir, "a.conf", "v1")
	chtimes(t, f, now.Add(-2*time.Hour))
	w := NewWatcher(d, state)
	if cs, _ := w.Check(); len(cs.Changed)+len(cs.Uncertain) != 0 {
		t.Fatalf("cs = %+v", cs)
	}
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if cs, _ := w.Check(); !reflect.DeepEqual(cs.Removed, []string{f}) {
		t.Fatalf("want Removed, got %+v", cs)
	}
	// supervisord restarts outside Visor: it no longer runs the file.
	chtimes(t, d.PidFile, now.Add(time.Hour))
	if cs, _ := w.Check(); len(cs.Removed) != 0 {
		t.Fatalf("after restart: %+v", cs)
	}
	if _, ok := w.Loaded(f); ok {
		t.Fatal("snapshot of a removed file kept")
	}
	if _, ok := NewWatcher(d, state).Loaded(f); ok {
		t.Fatal("snapshot of a removed file persisted")
	}
}
