package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestAutostart(t *testing.T, exe string, installed, moved bool) (*XDGAutostart, string) {
	t.Helper()
	cfg := t.TempDir() // a temp XDG_CONFIG_HOME
	return NewXDGAutostart(exe, cfg, installed, func() bool { return moved }), filepath.Join(cfg, "autostart", "dev.getvisor.visor.desktop")
}

func TestXDGAutostartWritesAndRemovesItsEntry(t *testing.T) {
	exe := "/home/ana/Applications/Visor.AppImage"
	x, file := newTestAutostart(t, exe, true, true)
	if st := x.Status(); st != LoginDisabled {
		t.Fatalf("before = %s", st)
	}
	if err := x.Set(true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := "[Desktop Entry]\nType=Application\nName=Visor\nComment=Manage supervisord from the tray\n" +
		"Exec=\"/home/ana/Applications/Visor.AppImage\" --login\nIcon=dev.getvisor.visor\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	if string(data) != want {
		t.Errorf("entry =\n%s\nwant\n%s", data, want)
	}
	if st := x.Status(); st != LoginEnabled {
		t.Errorf("after on = %s", st)
	}
	// Nothing but the entry is left in the folder (the temp file was renamed).
	if ents, _ := os.ReadDir(filepath.Dir(file)); len(ents) != 1 {
		t.Errorf("autostart holds %d files", len(ents))
	}
	if err := x.Set(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("entry still there: %v", err)
	}
	if err := x.Set(false); err != nil {
		t.Errorf("turning off twice: %v", err)
	}
}

func TestXDGAutostartAnotherCopyReadsAsOff(t *testing.T) {
	x, file := newTestAutostart(t, "/usr/bin/visor", true, true)
	other := NewXDGAutostart("/home/ana/Applications/Visor.AppImage", filepath.Dir(filepath.Dir(file)), true, func() bool { return true })
	if err := other.Set(true); err != nil {
		t.Fatal(err)
	}
	if st := x.Status(); st != LoginDisabled {
		t.Errorf("another copy's entry = %s, want disabled", st)
	}
	if err := x.Refresh(); err != nil {
		t.Fatal(err)
	}
	if got := autostartProgram(mustRead(t, file)); got != "/home/ana/Applications/Visor.AppImage" {
		t.Errorf("Refresh rewrote another copy's entry: %q", got)
	}
}

func TestXDGAutostartRefusals(t *testing.T) {
	dev, _ := newTestAutostart(t, "/home/ana/visor/bin/visor", false, true)
	if st := dev.Status(); st != LoginUnsupported {
		t.Errorf("dev build = %s", st)
	}
	if err := dev.Set(true); !errors.Is(err, ErrLoginUnsupported) {
		t.Errorf("dev build on = %v", err)
	}
	tmp, file := newTestAutostart(t, "/tmp/Visor.AppImage", true, false)
	if st := tmp.Status(); st != LoginNotMoved {
		t.Errorf("AppImage in /tmp = %s", st)
	}
	if err := tmp.Set(true); !errors.Is(err, ErrNotMoved) {
		t.Errorf("AppImage in /tmp on = %v", err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("a refused Set wrote the entry")
	}
}

func TestXDGAutostartRefreshAddsLoginArg(t *testing.T) {
	exe := "/usr/bin/visor"
	x, file := newTestAutostart(t, exe, true, true)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	old := "[Desktop Entry]\nType=Application\nName=Visor\nExec=/usr/bin/visor\n"
	if err := os.WriteFile(file, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := x.Status(); st != LoginEnabled {
		t.Fatalf("an unquoted Exec for this copy = %s", st)
	}
	if err := x.Refresh(); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, file)); !strings.Contains(got, `Exec="/usr/bin/visor" --login`) {
		t.Errorf("after Refresh:\n%s", got)
	}
}

func TestExecQuotingRoundTrips(t *testing.T) {
	for _, exe := range []string{
		"/usr/bin/visor",
		"/home/ana/My Apps/Visor.AppImage",
		`/home/ana/we"ird/$HOME/back\slash/100%/visor`,
		"/home/ana/`tick`/visor",
	} {
		entry := autostartEntry(exe)
		if got := autostartProgram(entry); got != exe {
			t.Errorf("round trip of %q = %q\n%s", exe, got, entry)
		}
	}
	if got := quoteExecArg(`/a b/c"d$e\f%g`); got != `"/a b/c\\"d\\$e\\\\f%%g"` {
		t.Errorf("quoteExecArg = %s", got)
	}
}

func TestAutostartProgramReadsOnlyTheDesktopEntryGroup(t *testing.T) {
	entry := "# comment\n[Desktop Action other]\nExec=/bin/other\n[Desktop Entry]\nName=Visor\nExec = \"/usr/bin/visor\" --login\n"
	if got := autostartProgram([]byte(entry)); got != "/usr/bin/visor" {
		t.Errorf("got %q", got)
	}
	if got := autostartProgram([]byte("[Desktop Entry]\nExec=\"/unterminated\n")); got != "" {
		t.Errorf("unterminated quote = %q", got)
	}
}

func TestXDGConfigHome(t *testing.T) {
	if got := xdgConfigHome("", "/home/ana"); got != "/home/ana/.config" {
		t.Errorf("unset = %q", got)
	}
	if got := xdgConfigHome("relative/cfg", "/home/ana"); got != "/home/ana/.config" {
		t.Errorf("relative = %q", got)
	}
	if got := xdgConfigHome("/srv/cfg", "/home/ana"); got != "/srv/cfg" {
		t.Errorf("absolute = %q", got)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
