//go:build server

package app

import "github.com/touhidurabir/visor/internal/platform"

// testHostFacts: the server build (UI tests) words Linux Doctor as on
// Ubuntu with a tray, whatever machine runs the tests, so the specs see
// the same commands on a Mac and in CI.
func testHostFacts() (hostFacts, bool) {
	return hostFacts{
		Distro:   platform.Distro{ID: "ubuntu", Name: "Ubuntu", Family: platform.FamilyDebian},
		TrayHost: true,
		Desktop:  "ubuntu:GNOME",
		InGroup:  func(string) (bool, bool) { return false, false },
	}, true
}
