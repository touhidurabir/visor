package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/doctor"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/settings"
)

// DoctorView is what the Doctor screen shows: the five checks, the
// situation the first failing one leads to, and what first-run setup needs.
type DoctorView struct {
	Steps      []doctor.Step
	Situation  string // doctor.Situation
	Version    string
	Processes  int
	Configs    int
	ConfigPath string
	IncludeDir string
	// IncludeMissing: IncludeDir doesn't exist yet.
	IncludeMissing bool
	Overridden     bool     // VISOR_CONFIG sets the config path
	Looked         []string // where Visor looked for the config
	Moved          bool     // false: running from the disk image or a quarantine copy
	User           string   // this Mac's user, for the commands Doctor suggests
	Onboarded      bool
	Notify         bool // notifications are on (the Ready toggle starts from it)
	// StartCommand is the command that starts supervisord ("" when it isn't
	// stopped, isn't installed, or has no socket to reach). CanStart: Visor
	// runs it itself (Start supervisord); false for a config that keeps
	// supervisord in the foreground (Foreground), which launchd should run.
	StartCommand string
	CanStart     bool
	Foreground   bool
	// StartFailure says why the last Start supervisord didn't work.
	StartFailure *StartFailure
	// Linux is the Linux advice (nil on macOS).
	Linux *LinuxAdvice
}

// StartFailure is what Visor knows when supervisord didn't start: the
// command, what it printed, and the end of supervisord's own log.
type StartFailure struct {
	Command string
	Output  string
	Error   string
	LogPath string
	Log     string
}

// DoctorService runs Doctor's checks (internal/doctor) against this Mac,
// saves a config the person points it to, and finishes first-run setup.
// It never runs sudo and writes nothing but Visor's own settings.
type DoctorService struct {
	rt       *Runtime
	settings *SettingsService
	login    *LoginItemService // nil: launch at login is left alone

	find      func() (path, version string, err error) // supervisord on the login PATH
	moved     func() bool
	brewAt    func(prefix string) string // a Homebrew prefix's brew, or ""
	start     func(context.Context, doctor.StartPlan, []string) (string, error)
	startWait time.Duration    // how long a started supervisord gets to answer
	host      func() hostFacts // the Linux facts behind LinuxAdvice
}

// NewDoctorService returns a DoctorService. login may be nil.
func NewDoctorService(rt *Runtime, s *SettingsService, login *LoginItemService) *DoctorService {
	return &DoctorService{
		rt: rt, settings: s, login: login,
		find: doctor.FindIn(platform.LoginPATH), moved: platform.Moved,
		brewAt: func(prefix string) string {
			p := filepath.Join(prefix, "bin", "brew")
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return p
			}
			return ""
		},
		start:     doctor.RunStart,
		startWait: 10 * time.Second,
		host:      realHostFacts,
	}
}

// pingTimeout keeps Check again quick when nothing answers.
const pingTimeout = 3 * time.Second

// Run runs the checks now.
func (s *DoctorService) Run() DoctorView {
	v, _, _ := s.check()
	return v
}

// check runs the checks and also returns how Start supervisord would start
// it (nil when it can't) and supervisord's log file.
func (s *DoctorService) check() (DoctorView, *doctor.StartPlan, string) {
	cur := s.rt.Settings()
	env := s.settings.envPath
	var (
		found config.Discovery
		bin   string
	)
	r := doctor.Run(doctor.Probe{
		FindSupervisord: func() (string, string, error) {
			path, version, err := s.find()
			bin = path
			return path, version, err
		},
		Resolve: func() (config.Discovery, []string, error) {
			path := cur.ConfigPath
			if env != "" {
				path = env
			}
			if path != "" {
				d, err := config.Resolve(path)
				found = d
				return d, []string{path}, err
			}
			d, err := config.Discover(config.DefaultPaths)
			found = d
			return d, append([]string(nil), config.DefaultPaths...), err
		},
		Ping: func(d config.Discovery) (int, error) {
			user, pass, err := s.credentials(cur, d)
			if err != nil {
				return 0, fmt.Errorf("%w: %w", doctor.ErrCredentials, err)
			}
			d.Username, d.Password = user, pass
			return doctor.Ping(pingTimeout)(d)
		},
		Writable: platform.DirWritable,
		Exists: func(dir string) bool {
			fi, err := os.Stat(dir)
			return err == nil && fi.IsDir()
		},
		Inventory: func(d config.Discovery) (int, error) {
			inv, err := config.LoadInventory(d)
			return len(inv.Files), err
		},
		Override:    func() (string, bool) { return env, env != "" },
		BundleMoved: s.moved,
	})
	v := DoctorView{
		Steps: r.Steps, Situation: string(r.Situation), Version: r.Version,
		Processes: r.Processes, Configs: r.Configs, ConfigPath: r.ConfigPath, IncludeDir: r.IncludeDir, IncludeMissing: r.IncludeMissing,
		Overridden: r.Overridden, Looked: r.Looked, Moved: r.Moved,
		Onboarded: cur.Onboarded, Notify: cur.Notify.Enabled,
	}
	if u, err := user.Current(); err == nil {
		v.User = u.Username
	}
	var family platform.Family
	if platform.Current() == platform.Linux && s.host != nil {
		h := s.host()
		family = h.Distro.Family
		v.Linux = linuxAdvice(h, r, found, v.User)
	}
	// Only a stopped supervisord that Visor found and could then reach.
	if r.Situation != doctor.Down || bin == "" || found.Transport == nil {
		return v, nil, found.LogFile
	}
	// A system service is started with systemctl, by the person: show it.
	if v.Linux != nil {
		if plan, ok := doctor.PlanSystemStart(found, family); ok {
			v.StartCommand = plan.Command
			return v, nil, found.LogFile
		}
	}
	plan, ok := doctor.PlanStart(found, bin, s.brewAt)
	v.StartCommand, v.CanStart, v.Foreground = plan.Command, ok, found.NoDaemon
	if !ok {
		return v, nil, found.LogFile
	}
	return v, &plan, found.LogFile
}

// StartSupervisord starts a stopped supervisord as this user (never sudo)
// and runs the checks again once it answers. When it doesn't start, the
// view's StartFailure says what the command printed and shows the end of
// supervisord's log.
func (s *DoctorService) StartSupervisord() (DoctorView, error) {
	v, plan, logPath := s.check()
	if plan == nil {
		return v, nil // nothing to start
	}
	mark := doctor.LogSize(logPath) // show only what this start writes
	out, err := s.start(context.Background(), *plan, platform.LoginPATH())
	if err == nil {
		for deadline := time.Now().Add(s.startWait); ; time.Sleep(300 * time.Millisecond) {
			if v, _, _ = s.check(); v.Situation != string(doctor.Down) || !time.Now().Before(deadline) {
				break
			}
		}
	}
	if err == nil && v.Situation != string(doctor.Down) {
		_ = s.rt.Reconnect() // the dashboard picks it up now, not at the next poll
		return v, nil
	}
	f := &StartFailure{Command: plan.Command, Output: out, LogPath: logPath, Log: doctor.LogSince(logPath, mark, 20)}
	if err != nil {
		f.Error = err.Error()
	} else {
		f.Error = fmt.Sprintf("the command finished, but supervisord didn't answer within %s", s.startWait.Round(time.Second))
	}
	v.StartFailure = f
	return v, nil
}

// credentials are the username and password Visor connects with: the ones
// saved in Settings (password from the Keychain), else the config's. A
// Keychain that refuses is an error, not an empty password.
func (s *DoctorService) credentials(cur settings.Settings, d config.Discovery) (string, string, error) {
	pass := ""
	if cur.Username != "" && s.settings.keychain != nil {
		var err error
		if pass, err = settings.Password(s.settings.keychain, d.ConfigPath); err != nil {
			return "", "", err
		}
	}
	user, pass := settings.Credentials(cur.Username, pass, d.Username, d.Password)
	return user, pass, nil
}

// UseConfig saves path as the config Visor uses and runs the checks again.
// A missing file, a folder, or a file that isn't a supervisord.conf is
// refused and nothing is saved. With VISOR_CONFIG set it refuses, because
// the saved path would be ignored.
func (s *DoctorService) UseConfig(path string) (DoctorView, error) {
	path = strings.TrimSpace(path)
	switch {
	case s.settings.envPath != "":
		return s.Run(), fmt.Errorf("VISOR_CONFIG is set to %s, so Visor uses that file. Unset it to choose another", s.settings.envPath)
	case path == "":
		return s.Run(), errors.New("enter the path to your supervisord.conf, or choose it")
	}
	cur := s.rt.Settings()
	if _, err := s.settings.SaveConnection(ConnectionInput{ConfigPath: path, Username: cur.Username}); err != nil {
		return s.Run(), err
	}
	return s.Run(), nil
}

// Finish records that first-run setup is done, with the notification
// switch as the person left it, and turns launch at login on when asked.
// Setup counts as done even when launch at login can't turn on; the
// returned warning says so.
func (s *DoctorService) Finish(launchAtLogin, notify bool) (warning string, err error) {
	if _, err := s.settings.FinishSetup(notify); err != nil {
		return "", err
	}
	if launchAtLogin && s.login != nil {
		if _, err := s.login.SetLaunchAtLogin(true); err != nil {
			return fmt.Sprintf("Visor won't open at login yet: %v. You can turn it on later in Settings.", err), nil
		}
	}
	return "", nil
}
