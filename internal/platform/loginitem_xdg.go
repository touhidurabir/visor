package platform

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// XDGAutostart is a LoginItem made of a desktop entry in
// $XDG_CONFIG_HOME/autostart (default ~/.config/autostart), which GNOME,
// KDE and the other desktops start at login.
type XDGAutostart struct {
	exe, file string
	installed bool
	moved     func() bool
}

// NewXDGAutostart starts exe (the installed binary or the AppImage) at
// login. configHome is $XDG_CONFIG_HOME; installed says exe is an installed
// copy (Installed); moved says it sits somewhere it can stay (Moved).
func NewXDGAutostart(exe, configHome string, installed bool, moved func() bool) *XDGAutostart {
	return &XDGAutostart{exe: exe, file: filepath.Join(configHome, "autostart", loginLabel+".desktop"), installed: installed, moved: moved}
}

// xdgConfigHome is $XDG_CONFIG_HOME, or ~/.config when it isn't set to an
// absolute path (the spec says to ignore a relative one).
func xdgConfigHome(env, home string) string {
	if filepath.IsAbs(env) {
		return env
	}
	return filepath.Join(home, ".config")
}

func (x *XDGAutostart) Status() LoginItemStatus {
	if !x.installed {
		return LoginUnsupported
	}
	// An entry for another copy (an older AppImage, a dev build) would start
	// that copy: it doesn't count as on for this one.
	if data, err := os.ReadFile(x.file); err == nil && autostartProgram(data) == x.exe {
		return LoginEnabled
	}
	if !x.moved() {
		return LoginNotMoved
	}
	return LoginDisabled
}

// Refresh rewrites this copy's entry when it predates today's (no
// LoginArg), so a launch at login stays quiet. Another copy's entry, or
// none, is left alone.
func (x *XDGAutostart) Refresh() error {
	data, err := os.ReadFile(x.file)
	if err != nil || autostartProgram(data) != x.exe || bytes.Equal(data, autostartEntry(x.exe)) {
		return nil
	}
	return x.Set(true)
}

func (x *XDGAutostart) Set(on bool) error {
	if !on {
		if err := os.Remove(x.file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if !x.installed {
		return ErrLoginUnsupported
	}
	if !x.moved() {
		return ErrNotMoved
	}
	if err := os.MkdirAll(filepath.Dir(x.file), 0o755); err != nil {
		return err
	}
	// Written whole or not at all: a temp file beside it, then a rename.
	tmp, err := os.CreateTemp(filepath.Dir(x.file), "."+loginLabel+"-*.desktop")
	if err != nil {
		return err
	}
	_, err = tmp.Write(autostartEntry(x.exe))
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), x.file)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// autostartEntry is the desktop entry that starts exe at login.
func autostartEntry(exe string) []byte {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Name=Visor\n")
	b.WriteString("Comment=Manage supervisord from the tray\n")
	b.WriteString("Exec=" + quoteExecArg(exe) + " " + LoginArg + "\n")
	b.WriteString("Icon=" + loginLabel + "\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("X-GNOME-Autostart-enabled=true\n")
	return []byte(b.String())
}

// quoteExecArg quotes one Exec argument as the Desktop Entry spec asks:
// inside double quotes, `"`, "`", "$" and "\" take a backslash, and "%"
// (a field code) is doubled. Then the whole value is escaped as a string
// value, where "\" itself is written "\\".
func quoteExecArg(s string) string {
	var q strings.Builder
	q.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '`', '$', '\\':
			q.WriteByte('\\')
			q.WriteRune(r)
		case '%':
			q.WriteString("%%")
		default:
			q.WriteRune(r)
		}
	}
	q.WriteByte('"')
	return strings.ReplaceAll(q.String(), `\`, `\\`)
}

// autostartProgram is the program an autostart entry runs (the first Exec
// argument, unquoted), or "" when it has none.
func autostartProgram(entry []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(entry))
	inEntry := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "Exec" {
			return firstExecArg(strings.ReplaceAll(strings.TrimSpace(v), `\\`, `\`))
		}
	}
	return ""
}

// firstExecArg reads the first argument of an Exec value whose string
// escapes are already undone: quoted (with backslash escapes) or bare.
func firstExecArg(v string) string {
	if !strings.HasPrefix(v, `"`) {
		arg, _, _ := strings.Cut(v, " ")
		return strings.ReplaceAll(arg, "%%", "%")
	}
	var out strings.Builder
	esc := false
	for _, r := range v[1:] {
		switch {
		case esc:
			out.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == '"':
			return strings.ReplaceAll(out.String(), "%%", "%")
		default:
			out.WriteRune(r)
		}
	}
	return "" // no closing quote
}
