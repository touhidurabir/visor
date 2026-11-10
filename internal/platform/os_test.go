package platform

import "testing"

func TestFromGOOS(t *testing.T) {
	for goos, want := range map[string]OS{"darwin": Darwin, "linux": Linux, "freebsd": Linux} {
		if got := fromGOOS(goos); got != want {
			t.Errorf("fromGOOS(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestParseOS(t *testing.T) {
	for in, want := range map[string]bool{"darwin": true, "linux": true, "": false, "windows": false, "Darwin": false} {
		if _, ok := parseOS(in); ok != want {
			t.Errorf("parseOS(%q) ok = %v, want %v", in, ok, want)
		}
	}
}

func TestDescribe(t *testing.T) {
	mac, linux := Describe(Darwin), Describe(Linux)
	if mac.Mod != "⌘" || mac.SecretStore != "Keychain" || mac.FileManager != "Finder" || mac.Tray != "menu bar" || !mac.SystemSettings {
		t.Errorf("darwin = %+v", mac)
	}
	if linux.Mod != "Ctrl" || linux.SecretStore != "keyring" || linux.FileManager != "file manager" || linux.Tray != "tray" || linux.SystemSettings {
		t.Errorf("linux = %+v", linux)
	}
}
