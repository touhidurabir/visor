//go:build !server

package main

import "github.com/touhidurabir/visor/internal/settings"

// newKeychain is the login Keychain.
func newKeychain() settings.Keychain { return settings.SystemKeychain{} }
