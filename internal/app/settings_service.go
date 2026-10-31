package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/settings"
)

// SettingsView is the Settings page: the saved settings (clamped, so the
// page shows what is in use) and what Visor found from them.
type SettingsView struct {
	Settings settings.Settings
	// EnvConfigPath is VISOR_CONFIG when set: it overrides
	// Settings.ConfigPath and the page cannot change it.
	EnvConfigPath string
	// ConfigPath is the config in use: the override, the saved path, or
	// the one auto-detected ("" if none was found).
	ConfigPath string
	// Socket is supervisord's address from that config: a socket path or
	// an http:// URL ("" if the config has none).
	Socket string
	// HasPassword says a password is saved in the Keychain for the saved
	// username and this config.
	HasPassword bool
	// LoadError is set when settings.json could not be read at launch, so
	// the defaults are in use.
	LoadError string
}

// ConnectionInput is what the Connection section saves. ConfigPath "" means
// auto-detect. The Keychain password changes only when SetPassword is true
// (then "" removes it).
type ConnectionInput struct {
	ConfigPath  string
	Username    string
	Password    string
	SetPassword bool
}

// SettingsService loads and saves Visor's settings for the frontend and
// puts them into use in the Runtime.
type SettingsService struct {
	rt       *Runtime
	stateDir string
	envPath  string
	keychain settings.Keychain

	// mu serialises saves, so two saves never interleave their
	// read-modify-write of the settings.
	mu      sync.Mutex
	loadErr error
}

// NewSettingsService returns a service that saves to stateDir, with envPath
// the VISOR_CONFIG override ("" if unset) and loadErr why settings.json
// could not be read at launch (nil if it could).
func NewSettingsService(rt *Runtime, stateDir, envPath string, k settings.Keychain, loadErr error) *SettingsService {
	return &SettingsService{rt: rt, stateDir: stateDir, envPath: envPath, keychain: k, loadErr: loadErr}
}

// Get returns the settings in use.
func (s *SettingsService) Get() SettingsView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view()
}

// view builds the page's view. Caller holds mu.
func (s *SettingsService) view() SettingsView {
	cur := s.rt.Settings()
	path, socket := s.rt.Connection()
	v := SettingsView{Settings: cur, EnvConfigPath: s.envPath, ConfigPath: path, Socket: socket}
	if s.loadErr != nil {
		v.LoadError = s.loadErr.Error()
	}
	if cur.Username != "" && path != "" && s.keychain != nil {
		pw, err := settings.Password(s.keychain, path)
		v.HasPassword = err == nil && pw != ""
	}
	return v
}

// Save saves everything but the connection (config path and username,
// which SaveConnection changes) and puts it into use: the poll interval,
// crash-loop thresholds and retention apply straight away. Out-of-range
// values are clamped; the returned view shows what was saved.
func (s *SettingsService) Save(in settings.Settings) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.rt.Settings()
	in.ConfigPath, in.Username = cur.ConfigPath, cur.Username
	in = in.Clamp()
	if err := settings.Save(s.stateDir, in); err != nil {
		return s.view(), err
	}
	s.loadErr = nil
	if err := s.rt.ApplySettings(in, false); err != nil {
		return s.view(), err
	}
	return s.view(), nil
}

// SaveConnection checks and saves the config path and credentials, then
// reconnects with them. A config path that does not exist, or is not a
// supervisord config Visor can read, is refused and nothing is saved.
func (s *SettingsService) SaveConnection(in ConnectionInput) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.rt.Settings()
	next.ConfigPath, next.Username = strings.TrimSpace(in.ConfigPath), strings.TrimSpace(in.Username)
	if s.envPath != "" {
		next.ConfigPath = s.rt.Settings().ConfigPath // VISOR_CONFIG wins; keep the saved one
	}
	if s.envPath == "" {
		if err := checkConfigPath(next.ConfigPath); err != nil {
			return s.view(), err
		}
	}
	if in.SetPassword {
		if in.Password != "" && next.Username == "" {
			return s.view(), errors.New("enter the username that goes with the password")
		}
		if err := s.savePassword(next, in.Password); err != nil {
			return s.view(), err
		}
	}
	if err := settings.Save(s.stateDir, next); err != nil {
		return s.view(), err
	}
	s.loadErr = nil
	if err := s.rt.ApplySettings(next, true); err != nil {
		return s.view(), err
	}
	return s.view(), nil
}

// savePassword stores password in the Keychain for the config that next
// connects to.
func (s *SettingsService) savePassword(next settings.Settings, password string) error {
	if s.keychain == nil {
		return errors.New("cannot save the password: the Keychain is not available")
	}
	path := s.envPath
	if path == "" {
		path = next.ConfigPath
	}
	if path == "" { // auto-detect: the password belongs to the config found
		d, err := config.Resolve("")
		if err != nil {
			return fmt.Errorf("cannot save the password: %w. Choose the config file first", err)
		}
		path = d.ConfigPath
	}
	return settings.SetPassword(s.keychain, path, password)
}

// checkConfigPath refuses a chosen config that is missing, a folder, or
// not readable as a supervisord config. "" (auto-detect) is always fine.
func checkConfigPath(path string) error {
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s does not exist: choose your supervisord.conf, or use auto-detect", path)
	case err != nil:
		return fmt.Errorf("cannot open %s: %w", path, err)
	case fi.IsDir():
		return fmt.Errorf("%s is a folder: choose the supervisord.conf file inside it", path)
	}
	if _, err := config.Resolve(path); err != nil {
		return fmt.Errorf("cannot use %s: %w", path, err)
	}
	return nil
}

// ChooseConfig shows the open-file dialog for picking supervisord.conf and
// returns the chosen path, or "" when the person cancels. It does not save.
func (s *SettingsService) ChooseConfig() (string, error) {
	a := application.Get()
	if a == nil {
		return "", errors.New("cannot show the file dialog: Visor is not running as an app")
	}
	path, _ := s.rt.Connection()
	d := a.Dialog.OpenFile().
		SetTitle("Choose supervisord.conf").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		ShowHiddenFiles(true).
		AttachToWindow(a.Window.Current())
	if path != "" {
		d.SetDirectory(filepath.Dir(path))
	}
	path, err := d.PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("cannot show the file dialog: %w", err)
	}
	return path, nil
}
