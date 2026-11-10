package doctor

import (
	"slices"
	"strings"
	"testing"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/platform"
)

func TestPackagesFor(t *testing.T) {
	cases := map[platform.Family]Packages{
		platform.FamilyDebian: {Install: "sudo apt install supervisor", Service: "supervisor", TrayInstall: "sudo apt install gnome-shell-extension-appindicator"},
		platform.FamilyFedora: {Install: "sudo dnf install supervisor", Service: "supervisord", TrayInstall: "sudo dnf install gnome-shell-extension-appindicator"},
		platform.FamilyArch:   {Install: "sudo pacman -S supervisor", Service: "supervisord", TrayInstall: "sudo pacman -S gnome-shell-extension-appindicator"},
	}
	for f, want := range cases {
		if got := PackagesFor(f); got != want {
			t.Errorf("%s = %+v", f, got)
		}
	}
	if got := PackagesFor(platform.FamilyOther); !strings.HasPrefix(got.Install, "pip install supervisor") || got.TrayInstall != "" {
		t.Errorf("other = %+v", got)
	}
}

func TestPlanSystemStart(t *testing.T) {
	for _, c := range []struct {
		conf   string
		family platform.Family
		want   string
		system bool
	}{
		{"/etc/supervisor/supervisord.conf", platform.FamilyDebian, "sudo systemctl enable --now supervisor", true},
		{"/etc/supervisord.conf", platform.FamilyFedora, "sudo systemctl enable --now supervisord", true},
		{"/home/ana/supervisor/supervisord.conf", platform.FamilyDebian, "", false},
		{"/etcetera/supervisord.conf", platform.FamilyDebian, "", false},
	} {
		p, ok := PlanSystemStart(config.Discovery{ConfigPath: c.conf}, c.family)
		if ok != c.system || p.Command != c.want || p.Name != "" {
			t.Errorf("%s: %+v, %v", c.conf, p, ok)
		}
	}
}

func commands(f RootFix) []string {
	var out []string
	for _, s := range f.Steps {
		out = append(out, s.Command)
	}
	return out
}

func TestGroupFixVariants(t *testing.T) {
	// No group on the socket: set one up, join it, restart, log in again.
	setup := GroupFix(SocketFacts{User: "ana", Service: "supervisor"})
	if setup.Variant != "setup" || setup.Group != "supervisor" || len(setup.Steps) != 4 {
		t.Fatalf("setup = %+v", setup)
	}
	if want := []string{"chmod=0770\nchown=root:supervisor", "sudo groupadd -f supervisor && sudo usermod -aG supervisor ana", "sudo systemctl restart supervisor", ""}; !slices.Equal(commands(setup), want) {
		t.Errorf("setup commands = %q", commands(setup))
	}
	if root := GroupFix(SocketFacts{User: "ana", Service: "supervisord", Chown: "root:root"}); root.Variant != "setup" {
		t.Errorf("chown to root's group = %+v", root)
	}
	// A group you're not in.
	join := GroupFix(SocketFacts{User: "ana", Service: "supervisor", Chown: "root:devops"})
	if join.Variant != "join" || join.Group != "devops" || !slices.Equal(commands(join), []string{"sudo usermod -aG devops ana", ""}) {
		t.Errorf("join = %+v", join)
	}
	// In the group, but this session started before.
	re := GroupFix(SocketFacts{User: "ana", Service: "supervisor", Chown: "root:devops", Member: true})
	if re.Variant != "relogin" || len(re.Steps) != 1 || !strings.HasPrefix(re.Steps[0].Text, "Log out and back in") {
		t.Errorf("relogin = %+v", re)
	}
	// In the group and the session has it: the mode is what's left.
	mode := GroupFix(SocketFacts{User: "ana", Service: "supervisor", Chown: "root:devops", Member: true, Active: true})
	if mode.Variant != "setup" || commands(mode)[0] != "chmod=0770" {
		t.Errorf("mode = %+v", mode)
	}
	if u := GroupFix(SocketFacts{Service: "supervisor"}); !strings.HasSuffix(commands(u)[1], "supervisor $USER") {
		t.Errorf("no user name = %q", commands(u))
	}
}

func TestWritableFix(t *testing.T) {
	if got := WritableFix("/etc/supervisor/conf.d"); !slices.Equal(got, []string{"sudo chgrp supervisor /etc/supervisor/conf.d && sudo chmod g+w /etc/supervisor/conf.d"}) {
		t.Errorf("got %q", got)
	}
	if got := WritableFix("/etc/my confs"); !strings.Contains(got[0], "'/etc/my confs'") {
		t.Errorf("quoting: %q", got)
	}
	if WritableFix("") != nil {
		t.Error("no dir, no command")
	}
}

func TestLinuxWarnings(t *testing.T) {
	deb := PackagesFor(platform.FamilyDebian)
	if w := LinuxWarnings(true, "ubuntu:GNOME", "", deb); len(w) != 0 {
		t.Errorf("all fine = %+v", w)
	}
	w := LinuxWarnings(false, "ubuntu:GNOME", "", deb)
	if len(w) != 1 || w[0].ID != "tray" || w[0].Command != "sudo apt install gnome-shell-extension-appindicator" {
		t.Errorf("GNOME without a tray = %+v", w)
	}
	if w := LinuxWarnings(false, "XFCE", "", deb); len(w) != 1 || w[0].Command != "" {
		t.Errorf("another desktop without a tray = %+v", w)
	}
	w = LinuxWarnings(true, "KDE", "/tmp/Visor-0.1.0-x86_64.AppImage", deb)
	if len(w) != 1 || w[0].ID != "appimage" || !strings.Contains(w[0].Detail, "/tmp") {
		t.Errorf("AppImage in /tmp = %+v", w)
	}
	if w := LinuxWarnings(true, "KDE", "/home/ana/Applications/Visor.AppImage", deb); len(w) != 0 {
		t.Errorf("AppImage at home = %+v", w)
	}
}
