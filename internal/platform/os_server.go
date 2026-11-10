//go:build server

package platform

import "os"

// override reads VISOR_PLATFORM ("darwin" or "linux"): the server build
// exists for UI tests, which check both platforms' wording on one machine.
func override() (OS, bool) { return parseOS(os.Getenv("VISOR_PLATFORM")) }
