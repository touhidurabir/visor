//go:build server

package platform

import (
	"runtime"
	"testing"
)

func TestCurrentHonoursVisorPlatformInServerBuild(t *testing.T) {
	t.Setenv("VISOR_PLATFORM", "darwin")
	if got := Current(); got != Darwin {
		t.Errorf("Current() = %q with VISOR_PLATFORM=darwin", got)
	}
	t.Setenv("VISOR_PLATFORM", "linux")
	if got := Current(); got != Linux {
		t.Errorf("Current() = %q with VISOR_PLATFORM=linux", got)
	}
	t.Setenv("VISOR_PLATFORM", "windows")
	if got := Current(); got != fromGOOS(runtime.GOOS) {
		t.Errorf("Current() = %q with an unknown VISOR_PLATFORM", got)
	}
}
