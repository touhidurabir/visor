package app

import (
	"testing"

	"github.com/touhidurabir/visor/internal/platform"
)

func TestSystemServiceDescribesCurrentOS(t *testing.T) {
	got := NewSystemService().Platform()
	if want := platform.Describe(platform.Current()); got != want {
		t.Errorf("Platform() = %+v, want %+v", got, want)
	}
}
