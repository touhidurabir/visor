package doctor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/platform"
)

// What Doctor tells a Linux user: the commands depend on the distro
// family, and supervisord there usually runs as root, as a system service.
// Doctor shows these commands; it never runs them.

// Packages is how a distro family installs and runs supervisord.
type Packages struct {
	Install string // the command that installs supervisord
	Service string // its systemd unit
	// TrayInstall installs GNOME's AppIndicator extension, which gives it a
	// tray ("" when Doctor doesn't know the package manager).
	TrayInstall string
}

// PackagesFor is the family's install commands and service name.
func PackagesFor(f platform.Family) Packages {
	const tray = "gnome-shell-extension-appindicator"
	add := map[platform.Family]string{
		platform.FamilyDebian: "sudo apt install",
		platform.FamilyFedora: "sudo dnf install",
		platform.FamilyArch:   "sudo pacman -S",
	}[f]
	service := "supervisord"
	if f == platform.FamilyDebian {
		service = "supervisor"
	}
	if add == "" {
		return Packages{Install: "pip install supervisor (or your package manager's supervisor package)", Service: service}
	}
	return Packages{Install: add + " supervisor", Service: service, TrayInstall: add + " " + tray}
}

// SystemConfig reports whether conf belongs to the system (under /etc), so
// a system service runs it and only root can start it.
func SystemConfig(conf string) bool {
	return strings.HasPrefix(filepath.Clean(conf), "/etc/")
}

// PlanSystemStart is the start for a stopped supervisord whose config is
// the system's: a systemctl command, shown and never run (it needs sudo).
// ok is false for any other config, which PlanStart handles.
func PlanSystemStart(d config.Discovery, f platform.Family) (StartPlan, bool) {
	if !SystemConfig(d.ConfigPath) {
		return StartPlan{}, false
	}
	return StartPlan{Command: "sudo systemctl enable --now " + PackagesFor(f).Service}, true
}

// SocketFacts are what the root-socket fix depends on.
type SocketFacts struct {
	User    string
	Service string // systemd unit, from the family
	// Chown is [unix_http_server] chown= ("root:supervisor"), "" if unset.
	Chown string
	// Member: the user is in Chown's group (in /etc/group). Active: this
	// session has it (os.Getgroups), which needs a new login after joining.
	Member, Active bool
}

// RootFix is the way out of a socket owned by root on Linux.
type RootFix struct {
	// Variant: "setup" (the config gives the socket to no group), "join"
	// (it does, but you aren't in that group), "relogin" (you are, but this
	// session started before you joined).
	Variant string
	Group   string
	Steps   []FixStep
}

// FixStep is one thing to do: what and why, and the command or config
// lines to copy ("" when there is nothing to copy).
type FixStep struct {
	Text, Command string
}

// supervisorGroup is the group the setup fix gives the socket to.
const supervisorGroup = "supervisor"

// GroupFix lets the user in through a group: a socket supervisord keeps
// for root, chmod 0770 and chown root:<group>, with the user in the group.
func GroupFix(s SocketFacts) RootFix {
	user := s.User
	if user == "" {
		user = "$USER"
	}
	group := chownGroup(s.Chown)
	restart := FixStep{Text: "Restart supervisord so the socket is made again with them.", Command: "sudo systemctl restart " + s.Service}
	relogin := FixStep{Text: "Log out and back in (or restart), so your session has the new group. Then open Visor again."}
	join := FixStep{Text: "Add yourself to that group.", Command: fmt.Sprintf("sudo usermod -aG %s %s", group, user)}
	switch {
	case group == "" || group == "root":
		return RootFix{Variant: "setup", Group: supervisorGroup, Steps: []FixStep{
			{Text: "In supervisord.conf, under [unix_http_server], let a group use the socket:", Command: "chmod=0770\nchown=root:" + supervisorGroup},
			{Text: "Create the group and add yourself to it.", Command: fmt.Sprintf("sudo groupadd -f %s && sudo usermod -aG %s %s", supervisorGroup, supervisorGroup, user)},
			restart,
			relogin,
		}}
	case !s.Member:
		return RootFix{Variant: "join", Group: group, Steps: []FixStep{join, relogin}}
	case !s.Active:
		return RootFix{Variant: "relogin", Group: group, Steps: []FixStep{relogin}}
	}
	// In the group and the session has it: the socket's mode must be what
	// stops it (chmod= left at 0700).
	return RootFix{Variant: "setup", Group: group, Steps: []FixStep{
		{Text: "In supervisord.conf, under [unix_http_server], let the group use the socket:", Command: "chmod=0770"},
		restart,
	}}
}

// chownGroup is the group of a chown= value ("user:group" or "user").
func chownGroup(chown string) string {
	_, g, ok := strings.Cut(strings.TrimSpace(chown), ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(g)
}

// WritableFix lets the supervisor group write programs into a config
// folder root owns.
func WritableFix(dir string) []string {
	if dir == "" {
		return nil
	}
	q := shellQuote(dir)
	return []string{"sudo chgrp " + supervisorGroup + " " + q + " && sudo chmod g+w " + q}
}

// Warning is a Linux note beside the checks: something that works, but
// not as well as it should.
type Warning struct {
	ID, Title, Detail, Command string
}

// LinuxWarnings are the notes for this desktop: no tray host (GNOME needs
// the AppIndicator extension), and an AppImage run from a temporary folder.
func LinuxWarnings(trayHost bool, desktop string, appImage string, p Packages) []Warning {
	var w []Warning
	if !trayHost {
		t := Warning{ID: "tray", Title: "No tray to show Visor's icon in",
			Detail: "Your desktop has no tray (StatusNotifier host), so Visor's icon and its menu don't show. Visor still runs: open it again from your apps."}
		if strings.Contains(strings.ToUpper(desktop), "GNOME") {
			t.Detail = "GNOME shows tray icons only with the AppIndicator extension. Install it, turn it on in Extensions, then open Visor again."
			t.Command = p.TrayInstall
		}
		w = append(w, t)
	}
	if appImage != "" && !appImageMoved(appImage) {
		w = append(w, Warning{ID: "appimage", Title: "Visor runs from a temporary folder",
			Detail: "This AppImage is in " + filepath.Dir(appImage) + ", which is emptied when the computer restarts, so launch at login stays off. Move it somewhere that stays, such as ~/Applications."})
	}
	return w
}

// appImageMoved: an AppImage outside the temporary folders.
func appImageMoved(p string) bool {
	for _, d := range []string{"/tmp/", "/var/tmp/", "/dev/shm/"} {
		if strings.HasPrefix(filepath.Clean(p)+"/", d) {
			return false
		}
	}
	return true
}
