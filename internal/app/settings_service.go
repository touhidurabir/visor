package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/platform"
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
	// HasPassword says a password is saved in the Keychain (or keyring)
	// for the saved username and this config.
	HasPassword bool
	// PasswordThisSession says that password is kept only until Visor
	// quits: no keyring was running when it was saved (Linux).
	PasswordThisSession bool
	// LoadError is set when settings.json could not be read at launch, so
	// the defaults are in use.
	LoadError string
	// Warning is set when a save succeeded but something after it did
	// not: the Keychain refused the password, or older issues could not
	// be cleared.
	Warning string
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
	// read-modify-write of the settings. It also guards the fields below.
	mu      sync.Mutex
	loadErr error
	// The Keychain is read once per account and again after a change, not
	// on every view: each read runs /usr/bin/security and may prompt.
	pwKnown   bool
	pwAccount string
	pwSaved   bool
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
		if !s.pwKnown || s.pwAccount != path {
			pw, err := settings.Password(s.keychain, path)
			s.pwKnown, s.pwAccount, s.pwSaved = true, path, err == nil && pw != ""
		}
		v.HasPassword = s.pwSaved
		if so, ok := s.keychain.(interface {
			SessionOnly(service, account string) bool
		}); ok && v.HasPassword {
			v.PasswordThisSession = so.SessionOnly(settings.KeychainService, path)
		}
	}
	return v
}

// withWarning returns the view after a save that succeeded, noting what
// went wrong after it (err may be nil).
func (s *SettingsService) withWarning(err error) SettingsView {
	v := s.view()
	if err != nil {
		v.Warning = err.Error()
	}
	return v
}

// Save saves everything but the connection (config path and username,
// which SaveConnection changes), the issue retention (ApplyRetention's,
// R27) and the appearance (SetAppearance's, so a page save built from an
// older view never puts back an older theme), and puts it into use: the poll interval and crash-loop thresholds
// apply straight away. Out-of-range values are clamped; the returned view
// shows what was saved.
func (s *SettingsService) Save(in settings.Settings) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.rt.Settings()
	// A lower retention saved here would delete history at the next
	// launch without the person confirming it.
	in.ConfigPath, in.Username, in.RetentionDays = cur.ConfigPath, cur.Username, cur.RetentionDays
	in.Appearance = cur.Appearance
	in.Onboarded = cur.Onboarded // only FinishSetup changes it
	return s.saveAndTell(in)
}

// FinishSetup records that first-run setup (Doctor) is done, with the
// notification master switch the person left on or off.
func (s *SettingsService) FinishSetup(notify bool) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.rt.Settings()
	next.Onboarded, next.Notify.Enabled = true, notify
	return s.saveAndTell(next)
}

// SetAppearance saves the colour theme ("system", "light" or "dark";
// anything else means system) and nothing else, puts it into the window
// chrome and tells every window, so the menu-bar popover follows.
func (s *SettingsService) SetAppearance(appearance string) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.rt.Settings()
	next.Appearance = appearance
	return s.saveAndTell(next)
}

// saveAndTell saves next (clamped), puts it into use and tells every
// window. Caller holds mu.
func (s *SettingsService) saveAndTell(next settings.Settings) (SettingsView, error) {
	next = next.Clamp()
	if err := settings.Save(s.stateDir, next); err != nil {
		return s.view(), err
	}
	s.loadErr = nil
	v := s.withWarning(s.rt.ApplySettings(next, false))
	setNativeAppearance(next.Appearance)
	s.rt.opts.Emit(EventSettings, v)
	return v, nil
}

// ApplyRetention saves how many days of closed issues and process history
// to keep (clamped) and puts it into use. A lower value deletes what is
// now older straight away, and that cannot be undone, so the page asks
// first; a higher one deletes nothing. Once the file is written the save
// has happened: a failure to clear older issues comes back as the view's
// Warning, not as an error.
func (s *SettingsService) ApplyRetention(days int) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.rt.Settings()
	old := next.RetentionDays
	next.RetentionDays = days
	next = next.Clamp()
	if err := settings.Save(s.stateDir, next); err != nil {
		return s.view(), err
	}
	s.loadErr = nil
	err := s.rt.ApplySettings(next, false)
	if err == nil && next.RetentionDays < old {
		err = s.rt.compactIssues(next)
	}
	return s.withWarning(err), nil
}

// SaveConnection checks and saves the config path and credentials, then
// reconnects with them. A config path that does not exist, or is not a
// supervisord config Visor can read, is refused and nothing is saved.
//
// settings.json is written first, then the Keychain, so a Keychain failure
// never leaves a password for settings that were not saved. The password
// belongs to a config file: when the config changes, a saved password moves
// with it; clearing the username (or SetPassword with "") removes it. A
// problem after settings.json is written comes back as the view's Warning.
func (s *SettingsService) SaveConnection(in ConnectionInput) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.rt.Settings()
	next.ConfigPath, next.Username = strings.TrimSpace(in.ConfigPath), strings.TrimSpace(in.Username)
	if s.envPath != "" {
		next.ConfigPath = s.rt.Settings().ConfigPath // VISOR_CONFIG wins; keep the saved one
	} else if err := checkConfigPath(next.ConfigPath); err != nil {
		return s.view(), err
	}
	setting := in.SetPassword && in.Password != ""
	if setting && next.Username == "" {
		return s.view(), errors.New("enter the username that goes with the password")
	}
	oldAccount, _ := s.rt.Connection()
	newAccount, findErr := s.accountFor(next)
	if setting {
		switch {
		case s.keychain == nil:
			return s.view(), fmt.Errorf("cannot save the password: the %s is not available", platform.Describe(platform.Current()).SecretStore)
		case findErr != nil:
			return s.view(), fmt.Errorf("cannot save the password: %w. Choose the config file first", findErr)
		}
	}

	if err := settings.Save(s.stateDir, next); err != nil {
		return s.view(), err
	}
	s.loadErr = nil
	var warn error
	if s.keychain != nil {
		warn = s.updatePassword(oldAccount, newAccount, next.Username, in.Password, setting, in.SetPassword)
		s.pwKnown = false
	}
	if err := s.rt.ApplySettings(next, true); err != nil {
		warn = errors.Join(warn, err)
	}
	if warn != nil {
		warn = fmt.Errorf("saved the connection, but %w", warn)
	}
	return s.withWarning(warn), nil
}

// accountFor is the Keychain account (config path) next connects with:
// VISOR_CONFIG, the chosen config, or the auto-detected one.
func (s *SettingsService) accountFor(next settings.Settings) (string, error) {
	switch {
	case s.envPath != "":
		return s.envPath, nil
	case next.ConfigPath != "":
		return next.ConfigPath, nil
	}
	d, err := config.Resolve("")
	return d.ConfigPath, err
}

// updatePassword brings the Keychain in line with a saved connection:
// store a new password, remove it (asked to, or no username any more), or
// move a saved one from the old config's account to the new one.
func (s *SettingsService) updatePassword(oldAccount, newAccount, username, password string, setting, asked bool) error {
	k := s.keychain
	removeFrom := func(accounts ...string) error {
		var errs []error
		for _, a := range slices.Compact(accounts) {
			if a != "" {
				errs = append(errs, settings.SetPassword(k, a, ""))
			}
		}
		return errors.Join(errs...)
	}
	switch {
	case setting:
		if err := settings.SetPassword(k, newAccount, password); err != nil {
			return err
		}
		if oldAccount != newAccount {
			return removeFrom(oldAccount)
		}
		return nil
	case asked || username == "":
		return removeFrom(oldAccount, newAccount)
	case oldAccount == "" || oldAccount == newAccount:
		return nil
	}
	pw, err := settings.Password(k, oldAccount)
	if err != nil || pw == "" {
		return err
	}
	if newAccount == "" {
		return errors.New("cannot move the saved password: Visor has not found the new config. Enter the password again")
	}
	if err := settings.SetPassword(k, newAccount, pw); err != nil {
		return err
	}
	return removeFrom(oldAccount)
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

// ApplyStartupAppearance puts the saved appearance into the window chrome.
// Call it once the app is running (main does, on ApplicationStarted): the
// main thread does not run before that.
func ApplyStartupAppearance(appearance string) { setNativeAppearance(appearance) }

// unlockHint says how to let Visor read a saved password again.
func unlockHint() string {
	if platform.Current() == platform.Darwin {
		return "Allow Visor to use the Keychain item"
	}
	return "Unlock your keyring"
}
