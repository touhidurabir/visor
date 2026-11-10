package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// Translocated reports whether path is a quarantine copy: macOS runs a
// downloaded app that was never moved from a random read-only folder
// under AppTranslocation, so anything pointing at its path breaks later.
func Translocated(path string) bool {
	return strings.Contains(path, "/AppTranslocation/")
}

// BundlePath is the .app this process runs from, or "" when it isn't in a
// bundle (a dev build).
func BundlePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return bundleOf(exe)
}

func bundleOf(exe string) string {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	app := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(app) != ".app" {
		return ""
	}
	return app
}

// Moved reports whether the running app has been moved somewhere it can
// stay. On macOS (e.g. Applications): not a quarantine copy, and not on a
// read-only volume such as the disk image it came on. On Linux: an
// AppImage outside /tmp that can still run (appImageMoved). A dev build
// counts as moved.
func Moved() bool {
	if runtime.GOOS != "darwin" {
		return appImageMoved(AppImage(), isExecutableFile)
	}
	return moved(BundlePath(), readOnly)
}

func moved(bundle string, readOnly func(string) bool) bool {
	switch {
	case bundle == "":
		return true
	case Translocated(bundle):
		return false
	}
	return !readOnly(bundle)
}

func readOnly(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	return st.Flags&0x1 != 0 // MNT_RDONLY
}
