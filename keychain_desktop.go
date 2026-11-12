//go:build !server

package main

import (
	"runtime"

	"github.com/touhidurabir/visor/internal/settings"
)

// newKeychain is the login Keychain, or on Linux the Secret Service with
// this run's memory behind it for a session that has no keyring.
func newKeychain() settings.Keychain {
	if runtime.GOOS == "darwin" {
		return settings.SystemKeychain{}
	}
	return &settings.SessionFallback{K: settings.SystemKeychain{}}
}
