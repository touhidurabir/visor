package platform

import "runtime"

// OS is the desktop Visor runs on, which decides its wording (⌘ or Ctrl,
// Keychain or keyring, Finder or the file manager) and its integrations.
type OS string

const (
	Darwin OS = "darwin"
	Linux  OS = "linux"
)

// Current is the OS this build runs on. Anything that isn't macOS gets the
// Linux wording. The server build (UI tests) honours VISOR_PLATFORM, so one
// test machine can check both sets of words.
func Current() OS {
	if os, ok := override(); ok {
		return os
	}
	return fromGOOS(runtime.GOOS)
}

func fromGOOS(goos string) OS {
	if goos == "darwin" {
		return Darwin
	}
	return Linux
}

func parseOS(s string) (OS, bool) {
	switch OS(s) {
	case Darwin, Linux:
		return OS(s), true
	}
	return "", false
}

// Info is what the frontend needs to word things for this OS.
type Info struct {
	OS OS `json:"os"`
	// Mod is the shortcut modifier as menus show it: "⌘" or "Ctrl".
	Mod string `json:"mod"`
	// SecretStore is where saved passwords go: "Keychain" or "keyring".
	SecretStore string `json:"secretStore"`
	// FileManager names the file browser: "Finder" or "file manager".
	FileManager string `json:"fileManager"`
	// Tray names where Visor's icon lives: "menu bar" or "tray".
	Tray string `json:"tray"`
	// SystemSettings: the OS has settings pages Visor can open (macOS's
	// Notifications and Login Items).
	SystemSettings bool `json:"systemSettings"`
	// Local names this computer's tab: "This Mac" or "This computer".
	Local string `json:"local"`
}

// Describe gives the wording for os.
func Describe(os OS) Info {
	if os == Darwin {
		return Info{OS: Darwin, Mod: "⌘", SecretStore: "Keychain", FileManager: "Finder", Tray: "menu bar", SystemSettings: true, Local: "This Mac"}
	}
	return Info{OS: Linux, Mod: "Ctrl", SecretStore: "keyring", FileManager: "file manager", Tray: "tray", Local: "This computer"}
}
