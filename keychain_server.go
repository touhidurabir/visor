//go:build server

package main

import "github.com/touhidurabir/visor/internal/settings"

// newKeychain keeps secrets in memory. Server mode exists for UI tests and
// must never write the login Keychain.
func newKeychain() settings.Keychain { return &settings.MemoryKeychain{} }
