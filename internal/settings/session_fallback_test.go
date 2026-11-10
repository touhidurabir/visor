package settings

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// A session without a keyring: go-keyring's mock answers every call with
// the error the Secret Service gives when nothing listens on the bus.
func noKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInitWithError(errors.New("dial unix /run/user/1000/bus: connect: no such file or directory"))
	t.Cleanup(keyring.MockInit)
}

func TestNoKeyringKeepsThePasswordForThisSession(t *testing.T) {
	noKeyring(t)
	k := &SessionFallback{K: SystemKeychain{}}
	const conf = "/etc/supervisor/supervisord.conf"

	err := SetPassword(k, conf, "s3cret")
	if !errors.Is(err, ErrNoKeyring) {
		t.Fatalf("SetPassword = %v, want ErrNoKeyring", err)
	}
	if !strings.HasPrefix(err.Error(), "no keyring is running") || strings.Contains(err.Error(), "cannot save the supervisord password in the") {
		t.Errorf("message = %q", err)
	}
	// The connection still gets it, until Visor quits.
	if pw, err := Password(k, conf); err != nil || pw != "s3cret" {
		t.Errorf("Password = %q, %v", pw, err)
	}
	if !k.SessionOnly(KeychainService, conf) {
		t.Error("SessionOnly = false for a password the keyring didn't take")
	}
	// Removing it works although the keyring is still down.
	if err := SetPassword(k, conf, ""); err != nil {
		t.Errorf("remove = %v", err)
	}
	if k.SessionOnly(KeychainService, conf) {
		t.Error("still kept after removing")
	}
	// Reading with nothing kept says why: the keyring isn't there.
	if _, err := Password(k, conf); err == nil {
		t.Error("Password with no keyring and nothing kept = nil error")
	}
}

func TestSessionFallbackWithAKeyringSavesThere(t *testing.T) {
	keyring.MockInit()
	k := &SessionFallback{K: SystemKeychain{}}
	const conf = "/home/ana/supervisord.conf"
	if err := SetPassword(k, conf, "s3cret"); err != nil {
		t.Fatal(err)
	}
	if k.SessionOnly(KeychainService, conf) {
		t.Error("a password the keyring took is not session-only")
	}
	if pw, err := keyring.Get(KeychainService, conf); err != nil || pw != "s3cret" {
		t.Errorf("keyring has %q, %v", pw, err)
	}
	if err := SetPassword(k, conf, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(KeychainService, conf); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("after remove: %v", err)
	}
	if err := SetPassword(k, conf, ""); err != nil {
		t.Errorf("removing twice: %v", err)
	}
}

func TestSessionFallbackMovesToTheKeyringWhenItComesBack(t *testing.T) {
	noKeyring(t)
	k := &SessionFallback{K: SystemKeychain{}}
	const conf = "/srv/supervisord.conf"
	_ = SetPassword(k, conf, "first")
	keyring.MockInit() // the keyring is unlocked now
	if err := SetPassword(k, conf, "second"); err != nil {
		t.Fatal(err)
	}
	if k.SessionOnly(KeychainService, conf) {
		t.Error("the session copy outlived a save the keyring took")
	}
	if pw, _ := Password(k, conf); pw != "second" {
		t.Errorf("Password = %q", pw)
	}
}
