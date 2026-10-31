package settings

import (
	"errors"
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

// KeychainService is the Keychain service that supervisord passwords are
// saved under, one entry per config file (the account is the config path).
// It is the app's bundle ID. Renaming it orphans every saved password:
// users would have to enter them again.
const KeychainService = "dev.getvisor.visor"

// Keychain stores secrets. SystemKeychain is the macOS Keychain; tests use
// keyring.MockInit or a fake, never the real Keychain.
type Keychain interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// SystemKeychain is the login Keychain, through go-keyring (which runs
// /usr/bin/security).
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

// Password returns the password saved for configPath, or "" if there is
// none.
func Password(k Keychain, configPath string) (string, error) {
	pw, err := k.Get(KeychainService, configPath)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("cannot read the supervisord password from the Keychain: %w", err)
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
			return fmt.Errorf("cannot remove the supervisord password from the Keychain: %w", err)
		}
		return nil
	}
	if err := k.Set(KeychainService, configPath, password); err != nil {
		return fmt.Errorf("cannot save the supervisord password in the Keychain: %w", err)
	}
	return nil
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
