//go:build !server

package platform

import (
	"runtime"
	"testing"
)

func TestCurrentIgnoresVisorPlatformOutsideServerBuild(t *testing.T) {
	other := "linux"
	if runtime.GOOS != "darwin" {
		other = "darwin"
	}
	t.Setenv("VISOR_PLATFORM", other)
	if got := Current(); got != fromGOOS(runtime.GOOS) {
		t.Errorf("Current() = %q: the desktop app must report the OS it runs on", got)
	}
}
