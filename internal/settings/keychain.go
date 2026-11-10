package settings

import (
	"errors"
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"

	"github.com/touhidurabir/visor/internal/platform"
)

// KeychainService is the Keychain service that supervisord passwords are
// saved under, one entry per config file (the account is the config path).
// It is the app's bundle ID. Renaming it orphans every saved password:
// users would have to enter them again.
const KeychainService = "dev.getvisor.visor"

// Keychain stores secrets. SystemKeychain is the macOS Keychain or, on
// Linux, the Secret Service (GNOME Keyring, KWallet); tests use
// keyring.MockInit or a fake, never the real one.
type Keychain interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// SystemKeychain is the login Keychain, through go-keyring (which runs
// /usr/bin/security), or on Linux the Secret Service over D-Bus.
type SystemKeychain struct{}

func (SystemKeychain) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (SystemKeychain) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}

func (SystemKeychain) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

// store is where passwords go, as the OS calls it: "Keychain" or "keyring".
func store() string { return platform.Describe(platform.Current()).SecretStore }

// ErrNoKeyring: on Linux no Secret Service answered (no GNOME Keyring or
// KWallet running, or a session without one), so a password can't be
// saved. SessionFallback still keeps it until Visor quits.
var ErrNoKeyring = errors.New("no keyring is running (GNOME Keyring or KWallet), so the password can't be saved. Visor uses it until you quit")

// Password returns the password saved for configPath, or "" if there is
// none.
func Password(k Keychain, configPath string) (string, error) {
	pw, err := k.Get(KeychainService, configPath)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("cannot read the supervisord password from the %s: %w", store(), err)
	}
	return pw, nil
}

// SetPassword saves password for configPath; an empty password removes the
// saved one.
func SetPassword(k Keychain, configPath, password string) error {
	if configPath == "" {
		return errors.New("cannot save the password: Visor has not found a supervisord config to save it for")
	}
	if password == "" {
		if err := k.Delete(KeychainService, configPath); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("cannot remove the supervisord password from the %s: %w", store(), err)
		}
		return nil
	}
	if err := k.Set(KeychainService, configPath, password); errors.Is(err, ErrNoKeyring) {
		return err
	} else if err != nil {
		return fmt.Errorf("cannot save the supervisord password in the %s: %w", store(), err)
	}
	return nil
}

// SessionFallback is the Linux secret store: the Secret Service, and when
// none answers, this run's memory. Saving then reports ErrNoKeyring, so the
// person knows, but the connection keeps its password until Visor quits.
type SessionFallback struct {
	K   Keychain
	mem MemoryKeychain
}

// keyringDown reports an error that means no Secret Service answered,
// rather than a missing or refused entry.
func keyringDown(err error) bool {
	return err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, keyring.ErrSetDataTooBig)
}

// Get prefers a password kept for this session (saved while the keyring
// was down) over the keyring's.
func (f *SessionFallback) Get(service, account string) (string, error) {
	if pw, err := f.mem.Get(service, account); err == nil {
		return pw, nil
	}
	return f.K.Get(service, account)
}

func (f *SessionFallback) Set(service, account, secret string) error {
	err := f.K.Set(service, account, secret)
	if keyringDown(err) {
		_ = f.mem.Set(service, account, secret)
		return ErrNoKeyring
	}
	if err == nil {
		_ = f.mem.Delete(service, account) // the keyring has it now
	}
	return err
}

// Delete forgets the session's copy and the keyring's. With the keyring
// down there is nothing more to remove, so that is not an error.
func (f *SessionFallback) Delete(service, account string) error {
	memErr := f.mem.Delete(service, account)
	err := f.K.Delete(service, account)
	switch {
	case keyringDown(err):
		if memErr == nil {
			return nil
		}
		return keyring.ErrNotFound
	case errors.Is(err, keyring.ErrNotFound) && memErr == nil:
		return nil
	}
	return err
}

// SessionOnly says the password for account is kept only until Visor
// quits, because the keyring was down when it was saved.
func (f *SessionFallback) SessionOnly(service, account string) bool {
	_, err := f.mem.Get(service, account)
	return err == nil
}

// MemoryKeychain keeps secrets in memory only. It is for the server-mode
// build the UI tests use, which must never touch the login Keychain.
type MemoryKeychain struct {
	mu      sync.Mutex
	secrets map[[2]string]string
}

func (m *MemoryKeychain) Get(service, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	secret, ok := m.secrets[[2]string{service, account}]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return secret, nil
}

func (m *MemoryKeychain) Set(service, account, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.secrets == nil {
		m.secrets = map[[2]string]string{}
	}
	m.secrets[[2]string{service, account}] = secret
	return nil
}

// Delete reports keyring.ErrNotFound for a missing entry, like the system
// Keychain.
func (m *MemoryKeychain) Delete(service, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [2]string{service, account}
	if _, ok := m.secrets[k]; !ok {
		return keyring.ErrNotFound
	}
	delete(m.secrets, k)
	return nil
}
