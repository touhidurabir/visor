package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/supervisor/supervisortest"
)

// fileProblems are the messages of v's problems about the file as a whole.
func fileProblems(v ExplainView) []string {
	var out []string
	for _, p := range v.FileProblems {
		out = append(out, p.Message)
	}
	return out
}

// hasProblem reports whether one of v's problems, on a line or the whole
// file, contains text.
func hasProblem(v ExplainView, text string) bool {
	if v.ParseError != nil && strings.Contains(v.ParseError.Message, text) {
		return true
	}
	for _, m := range fileProblems(v) {
		if strings.Contains(m, text) {
			return true
		}
	}
	for _, s := range v.Sections {
		for _, l := range s.Lines {
			for _, p := range l.Problems {
				if strings.Contains(p.Message, text) {
					return true
				}
			}
		}
	}
	return false
}

func TestCheckImport(t *testing.T) {
	cs, _, _, _ := configFixture(t)
	items := []ImportItem{
		{FileName: "imp-a.ini", Content: "[program:imp-a]\ncommand=sleep 3600\n"},
		// Shares imp-a's program name, which no file on disk has yet.
		{FileName: "imp-b.ini", Content: "[program:imp-a]\ncommand=sleep 3600\n"},
		// The folder already has worker.ini.
		{FileName: "worker.ini", Content: "[program:imp-c]\ncommand=sleep 3600\n"},
		// Two files in the batch named alike.
		{FileName: "twin.ini", Content: "[program:imp-d]\ncommand=sleep 3600\n"},
		{FileName: "Twin.ini", Content: "[program:imp-e]\ncommand=sleep 3600\n"},
		// Not loaded by the *.ini include setting.
		{FileName: "imp-f.conf", Content: "[program:imp-f]\ncommand=sleep 3600\n"},
		{FileName: "binary.ini", Content: "[program:imp-g]\x00\x01"},
		{FileName: "ok.ini", Content: "[program:imp-ok]\ncommand=sleep 3600\n"},
	}
	got := cs.CheckImport(items)
	if len(got) != len(items) {
		t.Fatalf("%d results for %d items", len(got), len(items))
	}
	want := map[int]string{
		0: "[program:imp-a] already exists in imp-b.ini",
		1: "[program:imp-a] already exists in imp-a.ini",
		2: "worker.ini already exists in",
		3: "Another file you’re importing is also named twin.ini",
		4: "Another file you’re importing is also named Twin.ini",
		5: "does not match the include setting",
		6: "isn’t text",
	}
	for i, text := range want {
		if got[i].SaveErrors == 0 || !hasProblem(got[i], text) {
			t.Errorf("%s: SaveErrors %d, problems %v, want %q", items[i].FileName, got[i].SaveErrors, fileProblems(got[i]), text)
		}
	}
	if v := got[7]; v.SaveErrors != 0 {
		t.Errorf("ok.ini: SaveErrors %d, problems %v", v.SaveErrors, fileProblems(v))
	}
	if got[2].FileProblems[0].Fix != "Choose another file name, e.g. worker-2.ini." {
		t.Errorf("worker.ini fix = %+v", got[2].FileProblems)
	}
}

func TestImportAddsAndStarts(t *testing.T) {
	cs, svc, rt, inst := configFixture(t)
	items := []ImportItem{
		{FileName: "imp-on.ini", Content: "[program:imp-on]\ncommand=sleep 3600\n"},
		// autostart=false: only Start after adding runs it.
		{FileName: "imp-off.ini", Content: "[program:imp-off]\ncommand=sleep 3600\nautostart=false\n"},
	}
	res, err := cs.Import(items, true)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range res {
		if r.Error != "" || r.StartError != "" || !r.Started || !r.Written || r.Path != confFile(inst, strings.TrimSuffix(items[i].FileName, ".ini")) {
			t.Errorf("%s: %+v", items[i].FileName, r)
		}
		if mustRead(t, r.Path) != items[i].Content {
			t.Errorf("%s: wrong content on disk", r.Path)
		}
		if !rt.watch.watched(r.Path) {
			t.Errorf("%s is not watched", r.Path)
		}
	}
	if !slices.Equal(res[0].Result.Added, []string{"imp-on"}) || !slices.Equal(res[1].Result.Added, []string{"imp-off"}) {
		t.Errorf("added: %v, %v", res[0].Result.Added, res[1].Result.Added)
	}
	waitRunning(t, svc, "imp-on:imp-on")
	waitRunning(t, svc, "imp-off:imp-off")
}

func TestImportRefusesABatchWithProblems(t *testing.T) {
	cs, _, _, _ := configFixture(t)
	items := []ImportItem{
		{FileName: "imp-good.ini", Content: "[program:imp-good]\ncommand=sleep 3600\n"},
		{FileName: "imp-bad.ini", Content: "[program:imp-bad]\nnumprocs=2\n"},
	}
	if _, err := cs.Import(items, false); !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "imp-bad.ini") {
		t.Fatalf("err = %v, want ErrInvalidConfig naming imp-bad.ini", err)
	}
	for _, it := range items {
		if _, err := os.Stat(filepath.Join(cs.IncludeDir(), it.FileName)); err == nil {
			t.Errorf("%s was written", it.FileName)
		}
	}
	if _, err := cs.Import(nil, false); err == nil {
		t.Error("an empty import succeeded")
	}
}

func TestStartFileLoadsAFileAddedElsewhere(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	waitRunning(t, svc, "sleeper:sleeper")

	// Copied into the include folder by hand: supervisord doesn't know it.
	inst.WriteProgram(t, supervisortest.Program{Name: "copied", Command: "sleep 3600", Extra: map[string]string{"autostart": "false"}})
	path := confFile(inst, "copied")
	waitOverview(t, svc, 5*time.Second, "copied listed as not loaded", func(ov OverviewView) bool {
		for _, c := range ov.Configs {
			if c.Path == path {
				return len(c.Processes) == 0 && slices.Equal(c.NotLoaded, []NotLoadedView{{Program: "copied", Group: "copied"}})
			}
		}
		return false
	})

	if err := svc.StopFile(path); err == nil || !strings.Contains(err.Error(), "Load it first") {
		t.Errorf("StopFile on a file supervisord hasn't loaded: err = %v", err)
	}
	if err := svc.StartFile(path); err != nil {
		t.Fatalf("StartFile: %v", err)
	}
	waitRunning(t, svc, "copied:copied")
	waitOverview(t, svc, 5*time.Second, "nothing not loaded", func(ov OverviewView) bool {
		for _, c := range ov.Configs {
			if len(c.NotLoaded) > 0 {
				return false
			}
		}
		return true
	})
}

func TestNotLoadedLists(t *testing.T) {
	f := config.ConfigFile{Programs: []config.Program{{Name: "a", Group: "a"}, {Name: "b", Group: "g"}, {Name: "c", Group: "g"}}}
	got := notLoaded(f, map[string]bool{"a": true}, true)
	want := []NotLoadedView{{Program: "b", Group: "g"}, {Program: "c", Group: "g"}}
	if !slices.Equal(got, want) {
		t.Errorf("notLoaded = %v, want %v", got, want)
	}
	if got := notLoaded(f, nil, false); len(got) != 0 {
		t.Errorf("unreachable: %v, want none", got)
	}
	if got := notLoaded(config.ConfigFile{Programs: f.Programs, Err: errors.New("bad")}, nil, true); len(got) != 0 {
		t.Errorf("unreadable file: %v, want none", got)
	}
}

// A file with a running program that changed on disk and a program nobody
// loaded: Start loads only the new one, the changed one keeps running as
// it was (Apply changes is for that), and Stop and Restart leave the new
// one out instead of failing.
func TestFileActionsOnAPartlyLoadedFile(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	cs := NewConfigService(rt)
	path := confFile(inst, "pair")
	if _, err := cs.Create("pair.ini", "[program:pair-old]\ncommand=sleep 3600\n"); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, svc, "pair-old:pair-old")
	oldPID := pidIn(t, svc, "pair-old:pair-old")

	edited := "[program:pair-old]\ncommand=sleep 3601\n\n[program:pair-new]\ncommand=sleep 3600\nautostart=false\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	waitOverview(t, svc, 5*time.Second, "pair-new not loaded", func(ov OverviewView) bool {
		for _, c := range ov.Configs {
			if c.Path == path {
				return slices.Equal(c.NotLoaded, []NotLoadedView{{Program: "pair-new", Group: "pair-new"}})
			}
		}
		return false
	})

	if err := svc.StopFile(path); err != nil {
		t.Fatalf("StopFile with a program not loaded: %v", err)
	}
	if err := svc.StartFile(path); err != nil {
		t.Fatalf("StartFile: %v", err)
	}
	waitRunning(t, svc, "pair-new:pair-new")
	waitRunning(t, svc, "pair-old:pair-old")
	if got := mustRead(t, path); got != edited {
		t.Errorf("file changed: %q", got)
	}
	// pair-old was stopped and started, not re-added: its edit still waits.
	if cs := changesOf(rt); !slices.Contains(cs.Changed, path) {
		t.Errorf("changes = %+v, want %s still changed", cs, path)
	}
	if pidIn(t, svc, "pair-old:pair-old") == oldPID {
		t.Error("pair-old wasn't restarted by Stop then Start")
	}
}

func TestLoadFileAddsOnlyWhatIsNotLoaded(t *testing.T) {
	inst := supervisortest.Start(t, supervisortest.Sleeper)
	rt, _ := startRuntime(t, confOf(inst), t.TempDir(), 100*time.Millisecond)
	svc := NewServerService(rt)
	cs := NewConfigService(rt)
	waitRunning(t, svc, "sleeper:sleeper")
	inst.WriteProgram(t, supervisortest.Program{Name: "late", Command: "sleep 3600"})
	path := confFile(inst, "late")
	res, err := cs.LoadFile(path)
	if err != nil || !slices.Equal(res.Added, []string{"late"}) {
		t.Fatalf("LoadFile = %+v, %v", res, err)
	}
	waitRunning(t, svc, "late:late")
	if res, err := cs.LoadFile(path); err != nil || len(res.Added)+len(res.Changed) != 0 {
		t.Errorf("second LoadFile = %+v, %v; want nothing to do", res, err)
	}
}
