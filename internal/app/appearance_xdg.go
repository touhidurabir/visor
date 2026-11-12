package app

import "github.com/touhidurabir/visor/internal/settings"

// preferDark is whether GTK should draw dark (the title bar, and the
// prefers-color-scheme the web page sees): the choice in Settings, or for
// "system" the desktop's own setting.
func preferDark(appearance string, systemDark bool) bool {
	switch appearance {
	case settings.AppearanceLight:
		return false
	case settings.AppearanceDark:
		return true
	}
	return systemDark
}

// portalDark reads the freedesktop appearance portal's color-scheme value:
// 1 prefers dark, 2 prefers light, 0 no preference (light).
func portalDark(v any) bool {
	for {
		switch x := v.(type) {
		case uint32:
			return x == 1
		case interface{ Value() any }: // a dbus.Variant, possibly nested (Read wraps it twice)
			v = x.Value()
		default:
			return false
		}
	}
}
