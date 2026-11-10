package app

import (
	"os"
	"os/user"
	"slices"
	"strconv"

	"github.com/godbus/dbus/v5"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/doctor"
	"github.com/touhidurabir/visor/internal/platform"
)

// LinuxAdvice is what Doctor shows on Linux beside the checks: the commands
// for this distro, the fix for a socket root keeps, and warnings. Doctor
// shows the commands; it never runs them. Nil on macOS.
type LinuxAdvice struct {
	Distro  string // os-release NAME ("Ubuntu"), "" when unknown
	Family  string // platform.Family
	Install string // installs supervisord
	Service string // its systemd unit
	// SystemService: the config is the system's (under /etc), so systemd
	// runs supervisord and StartCommand is a systemctl one to copy.
	SystemService bool
	// RootFix is the group fix when the socket isn't open to this user.
	RootFix *doctor.RootFix
	// WritableFix lets a group you're in write a config folder root owns
	// (when it isn't writable), joining the group first when needed.
	WritableFix []doctor.FixStep
	Warnings    []doctor.Warning
}

// hostFacts are the Linux facts Doctor's advice depends on.
type hostFacts struct {
	Distro   platform.Distro
	TrayHost bool   // a StatusNotifier host is on the session bus
	Desktop  string // $XDG_CURRENT_DESKTOP
	AppImage string // $APPIMAGE
	// InGroup says whether the user is in group (in /etc/group), and
	// whether this session already has it.
	InGroup func(group string) (member, active bool)
}

func realHostFacts() hostFacts {
	if h, ok := testHostFacts(); ok {
		return h
	}
	return hostFacts{
		Distro:   platform.ReadDistro(),
		TrayHost: trayHostRunning(),
		Desktop:  os.Getenv("XDG_CURRENT_DESKTOP"),
		AppImage: platform.AppImage(),
		InGroup:  inGroup,
	}
}

// trayHostRunning: something owns org.kde.StatusNotifierWatcher, the tray
// host Wails' Linux tray registers with (GNOME's AppIndicator extension,
// Plasma, …).
func trayHostRunning() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var owned bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owned)
	return err == nil && owned
}

func inGroup(group string) (member, active bool) {
	g, err := user.LookupGroup(group)
	if err != nil {
		return false, false
	}
	if u, err := user.Current(); err == nil {
		if ids, err := u.GroupIds(); err == nil {
			member = slices.Contains(ids, g.Gid)
		}
	}
	// The session has it as a supplementary group (after a new login), or
	// as its primary group (started with `sg` or `newgrp`).
	gid, _ := strconv.Atoi(g.Gid)
	if os.Getegid() == gid || os.Getgid() == gid {
		active = true
	} else if gids, err := os.Getgroups(); err == nil {
		active = slices.Contains(gids, gid)
	}
	return member || active, active
}

// linuxAdvice builds the advice for a report on Linux.
func linuxAdvice(h hostFacts, r doctor.Report, d config.Discovery, userName string) *LinuxAdvice {
	pk := doctor.PackagesFor(h.Distro.Family)
	a := &LinuxAdvice{
		Distro: h.Distro.Name, Family: string(h.Distro.Family), Install: pk.Install, Service: pk.Service,
		SystemService: doctor.SystemConfig(d.ConfigPath),
		Warnings:      doctor.LinuxWarnings(h.TrayHost, h.Desktop, h.AppImage, pk),
	}
	switch r.Situation {
	case doctor.Denied:
		f := doctor.SocketFacts{User: userName, Service: pk.Service, Chown: d.SocketChown}
		if g := doctor.ChownGroup(d.SocketChown); g != "" && h.InGroup != nil {
			f.Member, f.Active = h.InGroup(g)
		}
		fix := doctor.GroupFix(f)
		a.RootFix = &fix
	case doctor.NotWritable:
		if !r.IncludeMissing {
			f := doctor.FolderFacts{Dir: r.IncludeDir, User: userName, Group: doctor.ChownGroup(d.SocketChown)}
			group := f.Group
			if group == "" || group == "root" {
				group = "supervisor"
			}
			if h.InGroup != nil {
				f.Member, f.Active = h.InGroup(group)
			}
			a.WritableFix = doctor.WritableFix(f)
		}
	}
	return a
}
