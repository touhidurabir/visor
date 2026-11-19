package settings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestLoadDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state") // does not exist yet
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Defaults()
	if got != want {
		t.Errorf("Load of a missing file = %+v, want %+v", got, want)
	}
	if want.PollInterval != 2*time.Second || want.RetentionDays != 90 ||
		want.CrashLoop.Restarts != 5 || want.CrashLoop.Window != 10*time.Minute {
		t.Errorf("defaults = %+v", want)
	}
	n := want.Notify
	if !n.Enabled || !n.Fatal || !n.UnexpectedExit || !n.CrashLoop || !n.Unreachable {
		t.Errorf("notify defaults = %+v, want all on", n)
	}
	if want.ConfigPath != "" || want.Username != "" || want.Onboarded {
		t.Errorf("defaults = %+v, want no config path, no username, not onboarded", want)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load created %s: %v", dir, err)
	}
}

func TestLoadKeepsDefaultsForMissingKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"retentionDays":30}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Defaults()
	want.RetentionDays = 30
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadCorruptFileReturnsDefaultsAndError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err == nil {
		t.Error("Load of a corrupt file: want error")
	}
	if got != Defaults() {
		t.Errorf("got %+v, want defaults", got)
	}
}

func TestClamp(t *testing.T) {
	cases := []struct {
		name string
		in   func(*Settings)
		want func(*Settings)
	}{
		{"poll too fast", func(s *Settings) { s.PollInterval = 100 * time.Millisecond }, func(s *Settings) { s.PollInterval = time.Second }},
		{"poll zero", func(s *Settings) { s.PollInterval = 0 }, func(s *Settings) { s.PollInterval = time.Second }},
		{"poll too slow", func(s *Settings) { s.PollInterval = time.Minute }, func(s *Settings) { s.PollInterval = 10 * time.Second }},
		{"poll in range", func(s *Settings) { s.PollInterval = 5 * time.Second }, func(s *Settings) { s.PollInterval = 5 * time.Second }},
		{"retention too short", func(s *Settings) { s.RetentionDays = 1 }, func(s *Settings) { s.RetentionDays = 7 }},
		{"retention negative", func(s *Settings) { s.RetentionDays = -5 }, func(s *Settings) { s.RetentionDays = 7 }},
		{"retention too long", func(s *Settings) { s.RetentionDays = 100000 }, func(s *Settings) { s.RetentionDays = 3650 }},
		{"restarts too few", func(s *Settings) { s.CrashLoop.Restarts = 0 }, func(s *Settings) { s.CrashLoop.Restarts = 2 }},
		{"restarts too many", func(s *Settings) { s.CrashLoop.Restarts = 1000 }, func(s *Settings) { s.CrashLoop.Restarts = 100 }},
		{"window too short", func(s *Settings) { s.CrashLoop.Window = time.Second }, func(s *Settings) { s.CrashLoop.Window = time.Minute }},
		{"window too long", func(s *Settings) { s.CrashLoop.Window = 48 * time.Hour }, func(s *Settings) { s.CrashLoop.Window = 24 * time.Hour }},
		{"config path trimmed", func(s *Settings) { s.ConfigPath = "  /etc/supervisord.conf \n" }, func(s *Settings) { s.ConfigPath = "/etc/supervisord.conf" }},
		{"appearance unknown", func(s *Settings) { s.Appearance = "sepia" }, func(s *Settings) { s.Appearance = AppearanceSystem }},
		{"appearance empty", func(s *Settings) { s.Appearance = "" }, func(s *Settings) { s.Appearance = AppearanceSystem }},
		{"appearance dark kept", func(s *Settings) { s.Appearance = AppearanceDark }, func(s *Settings) { s.Appearance = AppearanceDark }},
		{"appearance light kept", func(s *Settings) { s.Appearance = AppearanceLight }, func(s *Settings) { s.Appearance = AppearanceLight }},
		{"username trimmed", func(s *Settings) { s.Username = " admin " }, func(s *Settings) { s.Username = "admin" }},
	}
	for _, c := range cases {
		in, want := Defaults(), Defaults()
		c.in(&in)
		c.want(&want)
		if got := in.Clamp(); got != want {
			t.Errorf("%s: Clamp = %+v, want %+v", c.name, got, want)
		}
	}

	// Load clamps what it reads.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"pollInterval":1,"retentionDays":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.PollInterval != time.Second || got.RetentionDays != 7 {
		t.Errorf("Load did not clamp: %+v", got)
	}
}

func TestSaveAtomicAndMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := Defaults()
	s.RetentionDays = 30
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if m := fi.Mode().Perm(); m != 0o600 {
		t.Errorf("settings.json mode = %o, want 600", m)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m := di.Mode().Perm(); m != 0o700 {
		t.Errorf("state dir mode = %o, want 700", m)
	}
	// Saving again replaces the file; no temp file is left behind.
	s.RetentionDays = 60
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != fileName {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("state dir holds %v, want only %s", names, fileName)
	}

	// A failed save (the target is a folder) leaves no temp file either.
	bad := t.TempDir()
	if err := os.Mkdir(filepath.Join(bad, fileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(bad, s); err == nil {
		t.Error("Save over a folder: want error")
	}
	entries, _ = os.ReadDir(bad)
	if len(entries) != 1 {
		t.Errorf("failed save left %d entries, want 1", len(entries))
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := Settings{
		ConfigPath:    "/opt/example/supervisord.conf",
		Username:      "admin",
		PollInterval:  3 * time.Second,
		Notify:        NotifyPrefs{Enabled: true, Fatal: false, UnexpectedExit: true, CrashLoop: false, Unreachable: true},
		CrashLoop:     CrashLoopRules{Restarts: 8, Window: 15 * time.Minute},
		RetentionDays: 30,
		Onboarded:     true,
		Appearance:    AppearanceDark,
		MaxServers:    5,
		Mutes:         ActivityMutes{Files: true},
		ConfigRecheck: 2 * time.Minute,
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
	// Save clamps too: what is on disk is what Load returns.
	in.PollInterval = time.Hour
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(dir); got.PollInterval != 10*time.Second {
		t.Errorf("saved poll interval = %v, want clamped to 10s", got.PollInterval)
	}
}

func TestCredentials(t *testing.T) {
	cases := []struct {
		name                   string
		savedUser, savedPass   string
		configUser, configPass string
		wantUser, wantPass     string
	}{
		{"nothing anywhere", "", "", "", "", "", ""},
		{"config only", "", "", "cfg", "cfgpw", "cfg", "cfgpw"},
		{"settings override config", "me", "mypw", "cfg", "cfgpw", "me", "mypw"},
		{"settings without config", "me", "mypw", "", "", "me", "mypw"},
		{"settings username, no saved password", "me", "", "cfg", "cfgpw", "me", ""},
		{"password without a username is ignored", "", "mypw", "cfg", "cfgpw", "cfg", "cfgpw"},
	}
	for _, c := range cases {
		u, p := Credentials(c.savedUser, c.savedPass, c.configUser, c.configPass)
		if u != c.wantUser || p != c.wantPass {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, u, p, c.wantUser, c.wantPass)
		}
	}
}

func TestKeychainWithMock(t *testing.T) {
	keyring.MockInit() // never the real Keychain
	k := SystemKeychain{}
	const conf = "/opt/example/supervisord.conf"

	if pw, err := Password(k, conf); err != nil || pw != "" {
		t.Errorf("Password before saving = %q, %v; want empty, nil", pw, err)
	}
	if err := SetPassword(k, conf, "s3cret"); err != nil {
		t.Fatal(err)
	}
	if pw, err := Password(k, conf); err != nil || pw != "s3cret" {
		t.Errorf("Password = %q, %v; want s3cret", pw, err)
	}
	// The entry lives under the app's service, with the config path as the account.
	if pw, err := keyring.Get(KeychainService, conf); err != nil || pw != "s3cret" {
		t.Errorf("keyring.Get(%q, %q) = %q, %v", KeychainService, conf, pw, err)
	}
	if pw, _ := Password(k, "/other.conf"); pw != "" {
		t.Errorf("another config's password = %q, want empty", pw)
	}
	// An empty password removes the entry; removing it twice is fine.
	for range 2 {
		if err := SetPassword(k, conf, ""); err != nil {
			t.Fatal(err)
		}
	}
	if pw, err := Password(k, conf); err != nil || pw != "" {
		t.Errorf("Password after removing = %q, %v; want empty, nil", pw, err)
	}
	if err := SetPassword(k, "", "x"); err == nil {
		t.Error("SetPassword without a config path: want error")
	}
}

func TestKeychainErrors(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)
	if _, err := Password(SystemKeychain{}, "/a.conf"); err == nil {
		t.Error("Password with a failing keychain: want error")
	}
	if err := SetPassword(SystemKeychain{}, "/a.conf", "x"); err == nil {
		t.Error("SetPassword with a failing keychain: want error")
	}
}

func TestAppearanceDefaultsToSystem(t *testing.T) {
	if got := Defaults().Appearance; got != AppearanceSystem {
		t.Errorf("default appearance = %q, want %q", got, AppearanceSystem)
	}
	// A settings file from before the field existed keeps the default.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"retentionDays":30}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Appearance != AppearanceSystem {
		t.Errorf("appearance = %q, want %q", got.Appearance, AppearanceSystem)
	}
}

func TestMenuBarCountIsOnUnlessTurnedOff(t *testing.T) {
	if !Defaults().MenuBarCount {
		t.Fatal("the unread count beside the menu-bar icon should be on by default")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"onboarded":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(dir); !got.MenuBarCount {
		t.Error("a settings.json from before the switch turned the count off")
	}
	s := Defaults()
	s.MenuBarCount = false
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(dir); got.MenuBarCount {
		t.Error("turning it off didn't stick")
	}
}

func TestSearchWholeLogIsOffUntilTurnedOn(t *testing.T) {
	if Defaults().SearchWholeLog {
		t.Fatal("searching the whole log file should be off by default")
	}
	dir := t.TempDir()
	s := Defaults()
	s.SearchWholeLog = true
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(dir); !got.SearchWholeLog {
		t.Error("turning it on didn't stick")
	}
}

func TestConfigRecheckDefaultsToAMinuteAndIsClamped(t *testing.T) {
	if got := Defaults().ConfigRecheck; got != time.Minute {
		t.Fatalf("default = %v, want 1m", got)
	}
	for in, want := range map[time.Duration]time.Duration{0: MinConfigRecheck, 30 * time.Second: 30 * time.Second, time.Hour: MaxConfigRecheck} {
		s := Defaults()
		s.ConfigRecheck = in
		if got := s.Clamp().ConfigRecheck; got != want {
			t.Errorf("Clamp(%v) = %v, want %v", in, got, want)
		}
	}
}
