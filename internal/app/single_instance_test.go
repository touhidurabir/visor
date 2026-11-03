package app

import "testing"

func TestSingleInstanceOnlyForThePackagedApp(t *testing.T) {
	if o := SingleInstance("", NewShell()); o != nil {
		t.Errorf("a dev or server build (no bundle) got %+v; it must be able to run beside the app", o)
	}
	o := SingleInstance("/Applications/Visor.app", NewShell())
	if o == nil || o.UniqueID != "dev.getvisor.visor" || o.OnSecondInstanceLaunch == nil {
		t.Fatalf("packaged app = %+v", o)
	}
}

func TestSecondLaunchOpensTheWindowUnlessAtLogin(t *testing.T) {
	if !secondLaunchShowsWindow([]string{"/Applications/Visor.app/Contents/MacOS/visor"}) {
		t.Error("opening Visor again should bring its window forward")
	}
	if secondLaunchShowsWindow([]string{"/Applications/Visor.app/Contents/MacOS/visor", LoginArg}) {
		t.Error("launchd starting the login item again should stay quiet")
	}
}
