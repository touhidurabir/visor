//go:build !server

package app

// testHostFacts: the desktop app reads the real host.
func testHostFacts() (hostFacts, bool) { return hostFacts{}, false }
