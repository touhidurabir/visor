package settings

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain points go-keyring at its in-memory mock before any test runs,
// so no test in this package can reach the real login Keychain.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
