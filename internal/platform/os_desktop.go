//go:build !server

package platform

// override: the desktop app always reports the OS it runs on.
func override() (OS, bool) { return "", false }
