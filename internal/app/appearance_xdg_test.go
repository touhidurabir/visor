package app

import (
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/touhidurabir/visor/internal/settings"
)

func TestPreferDark(t *testing.T) {
	for _, c := range []struct {
		appearance string
		system     bool
		want       bool
	}{
		{settings.AppearanceLight, true, false},
		{settings.AppearanceDark, false, true},
		{settings.AppearanceSystem, true, true},
		{settings.AppearanceSystem, false, false},
		{"", true, true},
	} {
		if got := preferDark(c.appearance, c.system); got != c.want {
			t.Errorf("preferDark(%q, %v) = %v", c.appearance, c.system, got)
		}
	}
}

func TestPortalDark(t *testing.T) {
	if !portalDark(dbus.MakeVariant(uint32(1))) {
		t.Error("1 is prefer-dark")
	}
	if portalDark(dbus.MakeVariant(uint32(2))) || portalDark(dbus.MakeVariant(uint32(0))) {
		t.Error("2 and 0 are light")
	}
	// The older Read call wraps the value in a second variant.
	if !portalDark(dbus.MakeVariant(dbus.MakeVariant(uint32(1)))) {
		t.Error("nested variant")
	}
	if portalDark(dbus.MakeVariant("dark")) {
		t.Error("an unexpected type is light")
	}
}
