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

// secretStoreWord is how this OS names the password store in messages:
// "Keychain" on macOS, "keyring" on Linux.
func secretStoreWord() string { return platform.Describe(platform.Current()).SecretStore }
