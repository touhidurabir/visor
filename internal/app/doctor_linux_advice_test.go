package app

import (
	"testing"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/doctor"
	"github.com/touhidurabir/visor/internal/platform"
)

func ubuntu(inGroup func(string) (bool, bool)) hostFacts {
	return hostFacts{Distro: platform.Distro{ID: "ubuntu", Name: "Ubuntu", Family: platform.FamilyDebian}, TrayHost: true, Desktop: "ubuntu:GNOME", InGroup: inGroup}
}

func TestLinuxAdviceForAStoppedSystemService(t *testing.T) {
	d := config.Discovery{ConfigPath: "/etc/supervisor/supervisord.conf"}
	a := linuxAdvice(ubuntu(nil), doctor.Report{Situation: doctor.Down}, d, "ana")
	if a.Install != "sudo apt install supervisor" || a.Service != "supervisor" || !a.SystemService || a.Distro != "Ubuntu" || a.RootFix != nil || len(a.Warnings) != 0 {
		t.Errorf("advice = %+v", a)
	}
	if mine := linuxAdvice(ubuntu(nil), doctor.Report{Situation: doctor.Down}, config.Discovery{ConfigPath: "/home/ana/supervisord.conf"}, "ana"); mine.SystemService {
		t.Error("a config in home is not a system service")
	}
}

func TestLinuxAdviceForARootSocket(t *testing.T) {
	r := doctor.Report{Situation: doctor.Denied}
	asked := ""
	member := func(g string) (bool, bool) { asked = g; return true, false }
	a := linuxAdvice(ubuntu(member), r, config.Discovery{ConfigPath: "/etc/supervisor/supervisord.conf", SocketChown: "root:supervisor"}, "ana")
	if asked != "supervisor" || a.RootFix == nil || a.RootFix.Variant != "relogin" {
		t.Errorf("in the group, not logged in again: asked %q, %+v", asked, a.RootFix)
	}
	a = linuxAdvice(ubuntu(member), r, config.Discovery{ConfigPath: "/etc/supervisor/supervisord.conf"}, "ana")
	if a.RootFix == nil || a.RootFix.Variant != "setup" {
		t.Errorf("no chown = %+v", a.RootFix)
	}
}

func TestLinuxAdviceForARootConfigFolder(t *testing.T) {
	r := doctor.Report{Situation: doctor.NotWritable, IncludeDir: "/etc/supervisor/conf.d"}
	a := linuxAdvice(ubuntu(nil), r, config.Discovery{}, "ana")
	if len(a.WritableFix) != 1 || a.WritableFix[0] != "sudo chgrp supervisor /etc/supervisor/conf.d && sudo chmod g+w /etc/supervisor/conf.d" {
		t.Errorf("fix = %q", a.WritableFix)
	}
	r.IncludeMissing = true
	if a := linuxAdvice(ubuntu(nil), r, config.Discovery{}, "ana"); a.WritableFix != nil {
		t.Errorf("a missing folder is made, not regrouped: %q", a.WritableFix)
	}
	noTray := ubuntu(nil)
	noTray.TrayHost = false
	if a := linuxAdvice(noTray, doctor.Report{Situation: doctor.Ready}, config.Discovery{}, "ana"); len(a.Warnings) != 1 || a.Warnings[0].ID != "tray" {
		t.Errorf("warnings = %+v", a.Warnings)
	}
}
