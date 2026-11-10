package platform

import (
	"os"
	"testing"
)

// The .deb ships build/linux/apparmor/dev.getvisor.visor; it must be the
// profile Visor itself would suggest for /usr/bin/visor.
func TestPackagedAppArmorProfile(t *testing.T) {
	got, err := os.ReadFile("../../build/linux/apparmor/dev.getvisor.visor")
	if err != nil {
		t.Fatal(err)
	}
	if want := AppArmorProfile(AppArmorProfileName, "/usr/bin/visor"); string(got) != want {
		t.Errorf("build/linux/apparmor/dev.getvisor.visor =\n%s\nwant\n%s", got, want)
	}
}
