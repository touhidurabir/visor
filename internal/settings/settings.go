// Package settings keeps Visor's preferences: settings.json in the state
// folder, and the supervisord password in the macOS Keychain.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fileName is the settings file inside the state folder.
const fileName = "settings.json"

// Limits that Clamp enforces.
const (
	MinPollInterval     = time.Second
	MaxPollInterval     = 10 * time.Second
	MinRetentionDays    = 7
	MaxRetentionDays    = 3650
	MinCrashLoopRestart = 2
	MaxCrashLoopRestart = 100
	MinCrashLoopWindow  = time.Minute
	MaxCrashLoopWindow  = 24 * time.Hour
)

// Settings are Visor's preferences. Durations are stored in JSON as
// nanoseconds, like time.Duration.
type Settings struct {
	// ConfigPath is the supervisord.conf to use; "" auto-discovers.
	// VISOR_CONFIG overrides it.
	ConfigPath string `json:"configPath"`
	// Username, when set, replaces the [supervisorctl] username in the
	// config; its password is in the Keychain (see Password).
	Username      string         `json:"username"`
	PollInterval  time.Duration  `json:"pollInterval"`
	Notify        NotifyPrefs    `json:"notify"`
	CrashLoop     CrashLoopRules `json:"crashLoop"`
	RetentionDays int            `json:"retentionDays"`
	Onboarded     bool           `json:"onboarded"`
	// Appearance is the colour theme: AppearanceSystem, Light or Dark.
	Appearance string `json:"appearance"`
	// MenuBarCount shows the unread count beside the menu-bar icon.
	MenuBarCount bool `json:"menuBarCount"`
}

// Appearance values.
const (
	AppearanceSystem = "system"
	AppearanceLight  = "light"
	AppearanceDark   = "dark"
)

// NotifyPrefs are which macOS notifications Visor sends. Enabled is the
// master switch.
type NotifyPrefs struct {
	Enabled        bool `json:"enabled"`
	Fatal          bool `json:"fatal"`
	UnexpectedExit bool `json:"unexpectedExit"`
	CrashLoop      bool `json:"crashLoop"`
	Unreachable    bool `json:"unreachable"`
}

// CrashLoopRules: Restarts restarts within Window open a crash-loop issue.
type CrashLoopRules struct {
	Restarts int           `json:"restarts"`
	Window   time.Duration `json:"window"`
}

// Defaults are the settings Visor starts with.
func Defaults() Settings {
	return Settings{
		PollInterval:  2 * time.Second,
		Notify:        NotifyPrefs{Enabled: true, Fatal: true, UnexpectedExit: true, CrashLoop: true, Unreachable: true},
		CrashLoop:     CrashLoopRules{Restarts: 5, Window: 10 * time.Minute},
		RetentionDays: 90,
		Appearance:    AppearanceSystem,
		MenuBarCount:  true,
	}
}

// Clamp returns s with every value moved into its allowed range and the
// text fields trimmed.
func (s Settings) Clamp() Settings {
	s.ConfigPath = strings.TrimSpace(s.ConfigPath)
	s.Username = strings.TrimSpace(s.Username)
	switch s.Appearance {
	case AppearanceLight, AppearanceDark:
	default:
		s.Appearance = AppearanceSystem
	}
	s.PollInterval = min(max(s.PollInterval, MinPollInterval), MaxPollInterval)
	s.RetentionDays = min(max(s.RetentionDays, MinRetentionDays), MaxRetentionDays)
	s.CrashLoop.Restarts = min(max(s.CrashLoop.Restarts, MinCrashLoopRestart), MaxCrashLoopRestart)
	s.CrashLoop.Window = min(max(s.CrashLoop.Window, MinCrashLoopWindow), MaxCrashLoopWindow)
	return s
}

// Retention is RetentionDays as a duration.
func (s Settings) Retention() time.Duration {
	return time.Duration(s.RetentionDays) * 24 * time.Hour
}

// Load reads dir/settings.json. A missing file gives the defaults, a key
// missing from the file keeps its default, and out-of-range values are
// clamped. A file that cannot be read or parsed gives the defaults and an
// error.
func Load(dir string) (Settings, error) {
	b, err := os.ReadFile(filepath.Join(dir, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("cannot read the settings: %w", err)
	}
	s := Defaults()
	if err := json.Unmarshal(b, &s); err != nil {
		return Defaults(), fmt.Errorf("cannot read the settings in %s: %w; Visor uses the defaults until you save them again", filepath.Join(dir, fileName), err)
	}
	return s.Clamp(), nil
}

// Save clamps s and writes it to dir/settings.json atomically (a temp file
// renamed over it), mode 0600, creating dir (0700) if needed.
func Save(dir string, s Settings) error {
	b, err := json.MarshalIndent(s.Clamp(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot save the settings: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "settings-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot save the settings: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // gone after a rename; cleans up after a failure
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot save the settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot save the settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, fileName)); err != nil {
		return fmt.Errorf("cannot save the settings: %w", err)
	}
	return nil
}

// Credentials picks the username and password for supervisord. A username
// saved in Visor's settings wins, with its saved password (possibly empty);
// otherwise the config's [supervisorctl] credentials are used. A saved
// password without a saved username is ignored.
func Credentials(savedUser, savedPass, configUser, configPass string) (user, pass string) {
	if savedUser != "" {
		return savedUser, savedPass
	}
	return configUser, configPass
}
