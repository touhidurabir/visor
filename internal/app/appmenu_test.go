package app

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestAppMenuHasNoWebReload(t *testing.T) {
	for _, sec := range appMenuSpec() {
		for _, it := range sec.Items {
			if it.Role == application.Reload || it.Role == application.ForceReload {
				t.Errorf("%s menu reloads the web view", sec.Title)
			}
			if strings.Contains(it.Label, "Learn More") {
				t.Errorf("%s menu has %q", sec.Title, it.Label)
			}
			if strings.HasPrefix(it.Action, "open:") && !strings.HasPrefix(it.Action, "open:https://github.com/touhidurabir/visor") {
				t.Errorf("%q opens %s", it.Label, it.Action)
			}
		}
	}
}

func TestAppMenuSections(t *testing.T) {
	var got []string
	for _, sec := range appMenuSpec() {
		got = append(got, sec.Title)
	}
	if strings.Join(got, ",") != "Visor,File,Edit,View,Supervisor,Window,Help" {
		t.Fatalf("sections = %v", got)
	}
}

func TestAppMenuAccelerators(t *testing.T) {
	want := map[string]string{
		"Preferences…":    "CmdOrCtrl+,",
		"New Config…":     "CmdOrCtrl+N",
		"Dashboard":       "CmdOrCtrl+1",
		"Issues":          "CmdOrCtrl+2",
		"Find":            "CmdOrCtrl+F",
		"Reload Config":   "CmdOrCtrl+R",
		"Restart All…":    "",
		"Doctor":          "",
		"Visor Help":      "",
		"Report an Issue": "",
	}
	seen := map[string]bool{}
	for _, sec := range appMenuSpec() {
		for _, it := range sec.Items {
			if acc, ok := want[it.Label]; ok {
				seen[it.Label] = true
				if it.Accel != acc {
					t.Errorf("%s: accelerator %q, want %q", it.Label, it.Accel, acc)
				}
			}
		}
	}
	for label := range want {
		if !seen[label] {
			t.Errorf("no %q item", label)
		}
	}
}

func TestAppMenuActionsRoute(t *testing.T) {
	want := map[string]string{
		"Preferences…":    "route:" + RouteSettings,
		"New Config…":     "route:#/new",
		"Dashboard":       "route:" + RouteMain,
		"Issues":          "route:" + RouteIssues,
		"Find":            "menu:find",
		"Reload Config":   "menu:reload",
		"Restart All…":    "menu:restart-all",
		"Visor Help":      "open:https://github.com/touhidurabir/visor#readme",
		"Report an Issue": "open:https://github.com/touhidurabir/visor/issues/new/choose",
	}
	doctor := 0
	for _, sec := range appMenuSpec() {
		for _, it := range sec.Items {
			if it.Label == "Doctor" {
				doctor++
				if it.Action != "route:#/doctor" {
					t.Errorf("%s → Doctor: %q", sec.Title, it.Action)
				}
			}
			if a, ok := want[it.Label]; ok && it.Action != a {
				t.Errorf("%s: action %q, want %q", it.Label, it.Action, a)
			}
		}
	}
	if doctor != 2 {
		t.Errorf("Doctor appears %d times, want 2 (Visor and Help menus)", doctor)
	}
}

func TestAppMenuDispatch(t *testing.T) {
	var shown, emitted, opened []string
	do := func(action string) {
		dispatchMenu(action,
			func(r string) { shown = append(shown, r) },
			func(a string) { emitted = append(emitted, a) },
			func(u string) { opened = append(opened, u) })
	}
	do("route:#/new")
	do("menu:find")
	do("open:https://github.com/touhidurabir/visor#readme")
	if strings.Join(shown, "|") != "#/new|" || strings.Join(emitted, "|") != "find" || len(opened) != 1 {
		t.Fatalf("shown %q, emitted %q, opened %q", shown, emitted, opened)
	}
}
