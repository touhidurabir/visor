package platform

import "testing"

func TestTranslocated(t *testing.T) {
	for path, want := range map[string]bool{
		"/private/var/folders/x/T/AppTranslocation/ABC-123/d/Visor.app": true,
		"/Applications/Visor.app":          false,
		"/Users/me/Applications/Visor.app": false,
		"/Users/me/Downloads/Visor.app":    false, // runs from there only after the quarantine is lifted
		"/Volumes/Visor 0.1.0/Visor.app":   false, // not translocated, but see Moved
	} {
		if got := Translocated(path); got != want {
			t.Errorf("Translocated(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestMoved(t *testing.T) {
	ro := func(string) bool { return false }
	for path, want := range map[string]bool{
		"": true, // not a bundle (dev build): nothing to move
		"/private/var/folders/x/T/AppTranslocation/ABC-123/d/Visor.app": false,
		"/Volumes/External/Apps/Visor.app":                              true, // an external drive is fine; the disk image is caught as read-only
		"/Applications/Visor.app":                                       true,
	} {
		if got := moved(path, ro); got != want {
			t.Errorf("moved(%q) = %v, want %v", path, got, want)
		}
	}
	if moved("/Applications/Visor.app", func(string) bool { return true }) {
		t.Error("a bundle on a read-only volume counts as moved")
	}
}

func TestBundleOf(t *testing.T) {
	for exe, want := range map[string]string{
		"/Applications/Visor.app/Contents/MacOS/visor": "/Applications/Visor.app",
		"/Users/me/code/visor/bin/visor":               "",
	} {
		if got := bundleOf(exe); got != want {
			t.Errorf("bundleOf(%q) = %q, want %q", exe, got, want)
		}
	}
}
