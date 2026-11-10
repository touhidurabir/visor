package platform

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// LoginItemStatus is whether Visor opens when the user logs in.
type LoginItemStatus string

const (
	LoginEnabled          LoginItemStatus = "enabled"
	LoginDisabled         LoginItemStatus = "disabled"
	LoginRequiresApproval LoginItemStatus = "requires_approval" // macOS wants the user's OK in Login Items
	LoginNotMoved         LoginItemStatus = "not_moved"         // running from the disk image or a quarantine copy
	LoginUnsupported      LoginItemStatus = "unsupported"       // not an installed copy (a dev build)
)

var (
	// ErrNotMoved: launch at login would point at a copy that goes away.
	ErrNotMoved = errors.New(notMovedText())
	// ErrLoginUnsupported: this build can't open at login (a dev build).
	ErrLoginUnsupported = errors.New(loginUnsupportedText())
)

func loginUnsupportedText() string {
	if runtime.GOOS == "darwin" {
		return "launch at login needs the packaged Visor.app"
	}
	return "launch at login needs an installed Visor (the package or the AppImage)"
}

func notMovedText() string {
	if runtime.GOOS == "darwin" {
		return "move Visor to Applications first"
	}
	return "move the AppImage out of /tmp first, and keep it executable"
}

// LoginItem turns "open Visor when I log in" on and off.
type LoginItem interface {
	Status() LoginItemStatus
	Set(on bool) error
}

const loginLabel = "dev.getvisor.visor"

// LoginArg is passed by the login item, so the app can tell a launch it
// caused (e.g. macOS starting it when switched on in System Settings).
const LoginArg = "--login"

// LaunchAgent is a LoginItem made of a plist in ~/Library/LaunchAgents
// that runs the app's binary at login. It works for any signature, unlike
// SMAppService, which may refuse an unsigned app.
type LaunchAgent struct {
	exe, plist string
	moved      func() bool
}

// NewLaunchAgent runs exe (inside a .app) at login; home is the user's
// home folder; moved reports whether the app sits somewhere it can stay.
func NewLaunchAgent(exe, home string, moved func() bool) *LaunchAgent {
	return &LaunchAgent{exe: exe, plist: filepath.Join(home, "Library", "LaunchAgents", loginLabel+".plist"), moved: moved}
}

func (l *LaunchAgent) Status() LoginItemStatus {
	if bundleOf(l.exe) == "" {
		return LoginUnsupported
	}
	// A plist for another copy (a test build, an older location) would
	// start that copy: it doesn't count as on for this one.
	if data, err := os.ReadFile(l.plist); err == nil && agentProgram(data) == l.exe {
		return LoginEnabled
	}
	if !l.moved() {
		return LoginNotMoved
	}
	return LoginDisabled
}

// Refresh rewrites this copy's plist when it predates LoginArg, so a
// launch macOS causes while Visor runs stays quiet. Another copy's plist,
// or none, is left alone.
func (l *LaunchAgent) Refresh() error {
	data, err := os.ReadFile(l.plist)
	if err != nil || agentProgram(data) != l.exe || bytes.Contains(data, []byte("<string>"+LoginArg+"</string>")) {
		return nil
	}
	return l.Set(true)
}

func (l *LaunchAgent) Set(on bool) error {
	if !on {
		if err := os.Remove(l.plist); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if bundleOf(l.exe) == "" {
		return ErrLoginUnsupported
	}
	if !l.moved() {
		return ErrNotMoved
	}
	if err := os.MkdirAll(filepath.Dir(l.plist), 0o755); err != nil {
		return err
	}
	var exe bytes.Buffer
	if err := xml.EscapeText(&exe, []byte(l.exe)); err != nil {
		return err
	}
	// Not loaded now (RunAtLoad would start a second Visor); launchd reads
	// it at the next login.
	return os.WriteFile(l.plist, []byte(fmt.Sprintf(agentPlist, loginLabel, exe.String(), LoginArg)), 0o644)
}

// agentProgram is the program a LaunchAgent plist runs (the first
// ProgramArguments string), or "" when it can't be read.
func agentProgram(plist []byte) string {
	d := xml.NewDecoder(bytes.NewReader(plist))
	var lastKey string
	inArgs := false
	for {
		tok, err := d.Token()
		if err != nil {
			return ""
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			var k string
			if d.DecodeElement(&k, &start) != nil {
				return ""
			}
			lastKey = k
		case "array":
			inArgs = lastKey == "ProgramArguments"
		case "string":
			var v string
			if d.DecodeElement(&v, &start) != nil {
				return ""
			}
			if inArgs {
				return v
			}
		}
	}
}

const agentPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`

// appServiceStatus maps SMAppServiceStatus (NotRegistered 0, Enabled 1,
// RequiresApproval 2, NotFound 3).
func appServiceStatus(code int) LoginItemStatus {
	switch code {
	case 0, 3:
		return LoginDisabled
	case 1:
		return LoginEnabled
	case 2:
		return LoginRequiresApproval
	}
	return LoginUnsupported
}

// useAppService picks SMAppService over the LaunchAgent. It stays false
// until M5's signing check shows SMAppService accepts an unsigned Visor.
var useAppService = false

// DefaultLoginItem is the login item for the running app: a LaunchAgent
// on macOS, an XDG autostart entry on Linux.
func DefaultLoginItem() LoginItem {
	home, _ := os.UserHomeDir()
	if runtime.GOOS != "darwin" {
		return NewXDGAutostart(Executable(), xdgConfigHome(os.Getenv("XDG_CONFIG_HOME"), home), Installed(), Moved)
	}
	if useAppService {
		return NewAppService(Moved)
	}
	return NewLaunchAgent(Executable(), home, Moved)
}
