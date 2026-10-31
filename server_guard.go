//go:build server

package main

import (
	"fmt"
	"os"
)

// guardEnvironment refuses to start a server-mode build that could reach the
// real supervisord or the real state folder: without VISOR_CONFIG Visor
// auto-detects the system config, and without VISOR_STATE_DIR it writes
// ~/Library/Application Support/Visor. Server mode exists for UI tests.
func guardEnvironment() {
	if os.Getenv("VISOR_CONFIG") == "" || os.Getenv("VISOR_STATE_DIR") == "" {
		fmt.Fprintln(os.Stderr, "visor (server build): refusing to start without VISOR_CONFIG and VISOR_STATE_DIR.\nThe server build is for UI tests; without them it would drive your real supervisord and write your real state folder.")
		os.Exit(2)
	}
}
