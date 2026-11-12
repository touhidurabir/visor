package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeFM struct {
	shown []string
	err   error
}

func (f *fakeFM) ShowItems(uris []string) error {
	f.shown = append(f.shown, uris...)
	return f.err
}

// testXDG records the commands it would run instead of running them.
func testXDG(fm *fakeFM, editor string) (xdg, *[][]string) {
	var ran [][]string
	return xdg{
		fm:         fm,
		run:        func(c *exec.Cmd) error { ran = append(ran, c.Args); return nil },
		textEditor: func() string { return editor },
	}, &ran
}

func writeFile(t *testing.T, p string, mode os.FileMode) string {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestXDGRevealUsesTheFileManagerThenXDGOpen(t *testing.T) {
	dir := t.TempDir()
	log := writeFile(t, filepath.Join(dir, "my app.log"), 0o644)
	fm := &fakeFM{}
	x, ran := testXDG(fm, "")
	if err := x.reveal(log); err != nil {
		t.Fatal(err)
	}
	if want := []string{"file://" + filepath.ToSlash(dir) + "/my%20app.log"}; !slices.Equal(fm.shown, want) {
		t.Errorf("ShowItems(%q), want %q", fm.shown, want)
	}
	if len(*ran) != 0 {
		t.Errorf("ran %q although the file manager answered", *ran)
	}

	// No D-Bus file manager: open the folder that holds the file.
	fm.err = errors.New("org.freedesktop.DBus.Error.ServiceUnknown")
	if err := x.reveal(log); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"xdg-open", dir}}; !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Errorf("fallback ran %q, want %q", *ran, want)
	}
	if err := x.reveal("relative.log"); !errors.Is(err, ErrNotAbsolute) {
		t.Errorf("relative: %v", err)
	}
}

func TestXDGOpenNeverLaunches(t *testing.T) {
	dir := t.TempDir()
	conf := writeFile(t, filepath.Join(dir, "web.ini"), 0o644)
	entry := writeFile(t, filepath.Join(dir, "evil.desktop"), 0o644)
	image := writeFile(t, filepath.Join(dir, "Tool.AppImage"), 0o644)
	script := writeFile(t, filepath.Join(dir, "run.sh"), 0o755)
	fm := &fakeFM{}
	x, ran := testXDG(fm, "")
	for _, p := range []string{conf, dir} {
		if err := x.open(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if want := [][]string{{"xdg-open", conf}, {"xdg-open", dir}}; !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Errorf("ran %q, want %q", *ran, want)
	}
	// A desktop entry or an AppImage is shown, not opened.
	for _, p := range []string{entry, image} {
		if err := x.open(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if len(*ran) != 2 || len(fm.shown) != 2 {
		t.Errorf("launchers: ran %q, shown %q", *ran, fm.shown)
	}
	if err := x.open(script); !errors.Is(err, ErrExecutable) {
		t.Errorf("executable: %v", err)
	}
}

func TestXDGOpenTextAlwaysUsesTheTextEditor(t *testing.T) {
	dir := t.TempDir()
	editor := "/usr/share/applications/org.gnome.TextEditor.desktop"
	fm := &fakeFM{}
	x, ran := testXDG(fm, editor)
	var want [][]string
	for _, c := range []struct {
		name string
		mode os.FileMode
	}{{"app.log", 0o644}, {"x.jar", 0o644}, {"evil.desktop", 0o644}, {"exec.log", 0o755}} {
		p := writeFile(t, filepath.Join(dir, c.name), c.mode)
		if err := x.openText(p); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		want = append(want, []string{"gio", "launch", editor, p})
	}
	if !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Errorf("ran %q, want %q", *ran, want)
	}
	if err := x.openText(dir); !errors.Is(err, ErrNotFile) {
		t.Errorf("folder: %v", err)
	}
	// No text editor set: the file is shown instead, never opened by type.
	none, ran2 := testXDG(fm, "")
	if err := none.openText(filepath.Join(dir, "x.jar")); err != nil {
		t.Fatal(err)
	}
	if len(*ran2) != 0 || len(fm.shown) != 1 {
		t.Errorf("no editor: ran %q, shown %q", *ran2, fm.shown)
	}
}

func TestFindDesktopFile(t *testing.T) {
	have := map[string]bool{
		"/home/ana/.local/share/applications/code.desktop":           true,
		"/usr/share/applications/org.gnome.TextEditor.desktop":       true,
		"/usr/local/share/applications/org.gnome.TextEditor.desktop": false,
	}
	exists := func(p string) bool { return have[p] }
	dirs := dataDirs("", "", "/home/ana")
	if want := []string{"/home/ana/.local/share", "/usr/local/share", "/usr/share"}; !slices.Equal(dirs, want) {
		t.Errorf("dataDirs = %q, want %q", dirs, want)
	}
	if got := findDesktopFile("org.gnome.TextEditor.desktop", dirs, exists); got != "/usr/share/applications/org.gnome.TextEditor.desktop" {
		t.Errorf("got %q", got)
	}
	if got := findDesktopFile("code.desktop", dirs, exists); got != "/home/ana/.local/share/applications/code.desktop" {
		t.Errorf("home first: %q", got)
	}
	for _, bad := range []string{"", "code", "../../etc/x.desktop", "a/b.desktop", ".desktop"} {
		if got := findDesktopFile(bad, dirs, func(string) bool { return true }); got != "" {
			t.Errorf("%q gave %q", bad, got)
		}
	}
	if got := dataDirs("/srv/data", "/a:relative:/b", "/home/ana"); !slices.Equal(got, []string{"/srv/data", "/a", "/b"}) {
		t.Errorf("custom dirs = %q", got)
	}
}

// The launched app keeps the launcher's output open; runDetached must not
// wait for it, as waiting on a pipe would.
func TestRunDetachedDoesNotWaitForTheLaunchedApp(t *testing.T) {
	start := time.Now()
	if err := runDetached(exec.Command("sh", "-c", "sleep 3 & exit 0")); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v: it waited for the launched app", d)
	}
	err := runDetached(exec.Command("sh", "-c", "echo 'no such app' >&2; exit 3", "/tmp/x.log"))
	if err == nil || !strings.Contains(err.Error(), "no such app") {
		t.Errorf("failure = %v, want the launcher's message", err)
	}
}
