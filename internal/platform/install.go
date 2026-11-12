package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Installed reports whether this is an installed copy of Visor, which keeps
// one instance per user: the .app on macOS; on Linux an AppImage, or the
// binary a package put in /usr/bin, /usr/local/bin, /opt or ~/.local/bin.
// Dev and server builds run beside it.
func Installed() bool {
	if runtime.GOOS == "darwin" {
		return BundlePath() != ""
	}
	home, _ := os.UserHomeDir()
	return installedOnLinux(Executable(), AppImage(), home)
}

// AppImage is the AppImage file this process runs from, or "". The
// AppImage runtime sets $APPIMAGE and $APPDIR (its mount), and every child
// inherits them, so $APPIMAGE counts only when this binary is inside
// $APPDIR: a .deb copy started from another AppImage (an editor, a
// terminal) must not take that app for itself.
func AppImage() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return appImageOf(os.Getenv("APPIMAGE"), os.Getenv("APPDIR"), exe)
}

func appImageOf(appImage, appDir, exe string) string {
	if appImage == "" || appDir == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(appDir); err == nil {
		appDir = real
	}
	if !within(exe, appDir) {
		return ""
	}
	return appImage
}

// installDirs are where a Linux package or a person puts Visor's binary.
var installDirs = []string{"/usr/bin", "/usr/local/bin", "/opt"}

func installedOnLinux(exe, appImage, home string) bool {
	if appImage != "" {
		return true
	}
	dirs := installDirs
	if home != "" {
		dirs = append(dirs[:len(dirs):len(dirs)], filepath.Join(home, ".local", "bin"))
	}
	for _, d := range dirs {
		if within(exe, d) {
			return true
		}
	}
	return false
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Executable is the program to start Visor again (at login): the .app's
// binary on macOS; on Linux the AppImage file when Visor runs from one
// ($APPIMAGE, because /proc/self/exe points into its temporary mount),
// otherwise the binary itself. Symlinks are resolved.
func Executable() string {
	if a := AppImage(); a != "" && runtime.GOOS != "darwin" {
		return a
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return exe
}

// tempDirs are emptied at boot (or now and then), so nothing that has to
// start Visor later may point into them.
var tempDirs = []string{"/tmp", "/var/tmp", "/dev/shm"}

// appImageMoved is Moved on Linux: an AppImage that sits in a temporary
// folder, or has lost its execute bit, can't be started at login. Any other
// copy counts as moved.
func appImageMoved(appImage string, isExec func(string) bool) bool {
	if appImage == "" {
		return true
	}
	return !InTempDir(appImage) && isExec(appImage)
}

// InTempDir reports whether path is in a folder that is emptied at boot.
func InTempDir(path string) bool {
	for _, d := range tempDirs {
		if within(path, d) {
			return true
		}
	}
	return false
}

// AppImageMoved is appImageMoved for an AppImage file on disk (Doctor's
// warning uses it, so it agrees with launch at login).
func AppImageMoved(appImage string) bool { return appImageMoved(appImage, isExecutableFile) }

func isExecutableFile(p string) bool { return executable(p) == nil }
