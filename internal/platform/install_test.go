package platform

import "testing"

func TestInstalledOnLinux(t *testing.T) {
	home := "/home/ana"
	cases := []struct {
		exe, appImage string
		want          bool
	}{
		{"/tmp/.mount_VisorXYZ/usr/bin/visor", "/home/ana/Applications/Visor.AppImage", true},
		{"/usr/bin/visor", "", true},
		{"/usr/local/bin/visor", "", true},
		{"/opt/visor/visor", "", true},
		{"/home/ana/.local/bin/visor", "", true},
		{"/home/ana/visor/bin/visor", "", false}, // a dev build
		{"/usr/binary/visor", "", false},
		{"/optional/visor", "", false},
	}
	for _, c := range cases {
		if got := installedOnLinux(c.exe, c.appImage, home); got != c.want {
			t.Errorf("installedOnLinux(%q, %q) = %v, want %v", c.exe, c.appImage, got, c.want)
		}
	}
	if installedOnLinux("/home/ana/.local/bin/visor", "", "") {
		t.Error("with no home, ~/.local/bin can't count")
	}
}

func TestAppImageMoved(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	if !appImageMoved("", no) {
		t.Error("not an AppImage: counts as moved")
	}
	for _, p := range []string{"/tmp/Visor.AppImage", "/tmp/dl/Visor.AppImage", "/var/tmp/Visor.AppImage", "/dev/shm/Visor.AppImage"} {
		if appImageMoved(p, yes) {
			t.Errorf("%s: a temporary folder must not count as moved", p)
		}
	}
	if !appImageMoved("/home/ana/Applications/Visor.AppImage", yes) {
		t.Error("an executable AppImage in home is moved")
	}
	if appImageMoved("/home/ana/Applications/Visor.AppImage", no) {
		t.Error("an AppImage that lost its execute bit can't start at login")
	}
	if !appImageMoved("/tmpfiles/Visor.AppImage", yes) {
		t.Error("/tmpfiles is not /tmp")
	}
}

func TestAppImageOnlyFromItsOwnMount(t *testing.T) {
	const img, mount = "/home/ana/Applications/Visor.AppImage", "/tmp/.mount_VisorAb12"
	if got := appImageOf(img, mount, mount+"/usr/bin/visor"); got != img {
		t.Errorf("Visor in its AppImage = %q", got)
	}
	// A .deb copy started from another AppImage inherits its variables.
	if got := appImageOf("/home/ana/Apps/Editor.AppImage", "/tmp/.mount_EditorX", "/usr/bin/visor"); got != "" {
		t.Errorf("inherited APPIMAGE = %q, want none", got)
	}
	if got := appImageOf(img, "", mount+"/usr/bin/visor"); got != "" {
		t.Errorf("no APPDIR = %q", got)
	}
}
