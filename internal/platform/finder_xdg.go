package platform

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// The Linux side of Reveal, Open and OpenText. It shows files through the
// file manager's D-Bus interface (org.freedesktop.FileManager1, which
// Nautilus, Dolphin, Nemo and Thunar implement) and opens them with
// xdg-open. Like macOS's open(1), nothing here may run a program.

// fileManager is the D-Bus file manager, behind an interface for tests.
type fileManager interface {
	ShowItems(uris []string) error
}

type dbusFileManager struct{}

// ShowItems opens a file manager window with the items selected.
func (dbusFileManager) ShowItems(uris []string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Object("org.freedesktop.FileManager1", "/org/freedesktop/FileManager1").
		CallWithContext(ctx, "org.freedesktop.FileManager1.ShowItems", 0, uris, "").Err
}

// xdg runs Reveal, Open and OpenText on Linux.
type xdg struct {
	fm  fileManager
	run func(*exec.Cmd) error
	// textEditor is the desktop file of the default app for text/plain, or
	// "" when there is none.
	textEditor func() string
}

var linuxFiles = xdg{fm: dbusFileManager{}, run: runDetached, textEditor: defaultTextEditor}

// runDetached is run for launchers whose child outlives them: gio launch
// and xdg-open start the editor or file manager, which inherits their
// output. Through a pipe, waiting for the output would wait until that app
// quits; a temp file has no end to wait for.
func runDetached(cmd *exec.Cmd) error {
	out, err := os.CreateTemp("", "visor-open-*.log")
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
		_ = os.Remove(out.Name())
	}()
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		msg, _ := os.ReadFile(out.Name())
		if m := strings.TrimSpace(string(msg)); m != "" {
			return fmt.Errorf("open %s: %s", cmd.Args[len(cmd.Args)-1], m)
		}
		return fmt.Errorf("open %s: %w", cmd.Args[len(cmd.Args)-1], err)
	}
	return nil
}

func fileURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// XDGOpenCmd opens path with its default app (a folder in the file
// manager): xdg-open <path>.
func XDGOpenCmd(path string) *exec.Cmd { return exec.Command("xdg-open", path) }

// GioLaunchCmd opens path with the app of a desktop file, whatever the
// file's type: gio launch <desktop file> <path>.
func GioLaunchCmd(desktopFile, path string) *exec.Cmd {
	return exec.Command("gio", "launch", desktopFile, path)
}

// reveal selects path in a file manager window. Without the D-Bus file
// manager it opens the folder that holds it.
func (x xdg) reveal(path string) error {
	if err := check(path, false); err != nil {
		return err
	}
	if err := x.fm.ShowItems([]string{fileURI(path)}); err == nil {
		return nil
	}
	dir := path
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		dir = filepath.Dir(path)
	}
	return x.run(XDGOpenCmd(dir))
}

// open opens path with its default app; something xdg-open would run or
// install (a desktop entry, an AppImage, a package) is revealed instead.
func (x xdg) open(path string) error {
	if err := check(path, true); err != nil {
		return err
	}
	if linuxLaunches(path) {
		return x.reveal(path)
	}
	return x.run(XDGOpenCmd(path))
}

// openText opens a file in the default text editor, whatever its
// extension: the file's own type never picks the app, so a log named
// x.jar or x.desktop can't be run. With no text editor set, it is revealed.
func (x xdg) openText(path string) error {
	if err := check(path, false); err != nil {
		return err
	}
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%s %w", path, ErrNotFile)
	}
	editor := x.textEditor()
	if editor == "" {
		return x.reveal(path)
	}
	return x.run(GioLaunchCmd(editor, path))
}

// linuxLaunchExts are files xdg-open runs, installs or mounts.
var linuxLaunchExts = []string{
	".desktop", ".appimage", ".jar", ".deb", ".rpm", ".snap", ".flatpak", ".flatpakref", ".flatpakrepo",
	".run", ".bin", ".exe", ".msi", ".bat", ".cmd", ".com", ".iso", ".img",
}

func linuxLaunches(path string) bool {
	return slices.Contains(linuxLaunchExts, strings.ToLower(filepath.Ext(path)))
}

// defaultTextEditor finds the desktop file of the default app for
// text/plain (xdg-mime query default text/plain) in the XDG data dirs.
func defaultTextEditor() string {
	out, err := exec.Command("xdg-mime", "query", "default", "text/plain").Output()
	if err != nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	return findDesktopFile(strings.TrimSpace(string(out)), dataDirs(os.Getenv("XDG_DATA_HOME"), os.Getenv("XDG_DATA_DIRS"), home), fileExists)
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// dataDirs are the XDG data dirs in search order: $XDG_DATA_HOME (or
// ~/.local/share), then $XDG_DATA_DIRS (or /usr/local/share:/usr/share).
func dataDirs(dataHome, dataDirsEnv, home string) []string {
	if !filepath.IsAbs(dataHome) {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dirs := []string{dataHome}
	if dataDirsEnv == "" {
		dataDirsEnv = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(dataDirsEnv, ":") {
		if filepath.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// findDesktopFile is the first <dir>/applications/<id> that exists. An id
// is one file name ending in .desktop; anything else is refused.
func findDesktopFile(id string, dirs []string, exists func(string) bool) string {
	if !strings.HasSuffix(id, ".desktop") || strings.ContainsAny(id, "/\\") || strings.HasPrefix(id, ".") {
		return ""
	}
	for _, d := range dirs {
		if p := filepath.Join(d, "applications", id); exists(p) {
			return p
		}
	}
	return ""
}
