// Package doctor runs the setup checks behind the first-launch screen and
// the Doctor page: is supervisord installed, is its config found, is it
// running, can Visor write programs, and what does it run. Every check goes
// through a Probe, so the same report can later describe a remote server.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// Status is one step's result.
type Status string

const (
	OK   Status = "ok"
	Bad  Status = "bad"
	Wait Status = "wait" // a step it depends on failed
)

// Situation is what the screen leads with: the first failing step decides.
type Situation string

const (
	Ready       Situation = "ready"
	NoSup       Situation = "nosup"       // supervisord isn't installed
	NoConf      Situation = "noconf"      // no readable supervisord.conf
	Down        Situation = "down"        // nothing to connect to
	Denied      Situation = "denied"      // the socket exists but isn't open to this user
	Auth        Situation = "auth"        // supervisord turned down the username and password
	NotWritable Situation = "notwritable" // can't add or change programs
)

// Step is one line of the checklist.
type Step struct {
	ID, Title, Detail string
	Status            Status
}

// Report is the whole checklist.
type Report struct {
	Steps      []Step
	Situation  Situation
	Version    string
	Processes  int
	Configs    int
	ConfigPath string
	IncludeDir string // where new programs go (the first [include] glob's folder)
	// IncludeMissing: IncludeDir doesn't exist yet (create it, don't chown it).
	IncludeMissing bool
	Overridden     bool     // the config path comes from VISOR_CONFIG
	Looked         []string // where Visor looked for the config
	Moved          bool     // false: Visor runs from the disk image or a quarantine copy
}

// ErrCredentials wraps a Ping error that came before supervisord was asked:
// the saved password couldn't be read (the Keychain refused, say).
var ErrCredentials = errors.New("couldn't read the saved password")

// Probe is how the checks see one target. Nil Override, BundleMoved and
// Exists mean "no override", "moved" and "exists".
type Probe struct {
	FindSupervisord func() (path, version string, err error)
	Resolve         func() (config.Discovery, []string, error)
	Ping            func(config.Discovery) (processes int, err error)
	Writable        func(dir string) bool
	Exists          func(dir string) bool
	Inventory       func(config.Discovery) (configs int, err error)
	Override        func() (path string, ok bool)
	BundleMoved     func() bool
}

const waitDetail = "Waiting for the step above"

// Run runs the five checks in order. A failed step marks the steps that
// need it as waiting: the config needs supervisord; running and writable
// need the config; processes need running. supervisord missing from the
// login PATH still passes when the config's server answers.
func Run(p Probe) Report {
	r := Report{Moved: true}
	if p.BundleMoved != nil {
		r.Moved = p.BundleMoved()
	}
	if p.Override != nil {
		var path string
		if path, r.Overridden = p.Override(); r.Overridden {
			r.ConfigPath = path // shown even when the file is missing
		}
	}
	steps := []Step{
		{ID: "installed", Title: "supervisord installed"},
		{ID: "config", Title: "Config file found"},
		{ID: "running", Title: "supervisord running"},
		{ID: "writable", Title: "Config folder writable"},
		{ID: "processes", Title: "Processes found"},
	}
	set := func(i int, st Status, detail string) { steps[i].Status, steps[i].Detail = st, detail }
	wait := func(from ...int) {
		for _, i := range from {
			set(i, Wait, waitDetail)
		}
	}
	fail := func(s Situation) {
		if r.Situation == "" {
			r.Situation = s
		}
	}

	d, looked, resolveErr := p.Resolve()
	// Ping at most once: step 1 may need the answer, step 3 always does.
	var (
		pinged  bool
		pingN   int
		pingErr error
	)
	ping := func() (int, error) {
		if !pinged {
			pinged = true
			pingN, pingErr = p.Ping(d)
		}
		return pingN, pingErr
	}
	answers := func() bool {
		if d.Transport == nil {
			return false
		}
		_, err := ping()
		return err == nil
	}

	path, version, err := p.FindSupervisord()
	switch {
	case err == nil:
		r.Version = version
		set(0, OK, installedDetail(path, version))
	case resolveErr == nil && answers():
		// Installed where the login PATH doesn't reach (a virtualenv, say):
		// a server that answers proves it.
		set(0, OK, "Not on your PATH, but it's running")
	default:
		set(0, Bad, "Not on your PATH or in Homebrew")
		wait(1, 2, 3, 4)
		r.Steps, r.Situation = steps, NoSup
		return r
	}

	err = resolveErr
	r.Looked = looked
	if err != nil {
		detail := "Not in any of the usual places"
		if !errors.Is(err, config.ErrNoConfig) {
			detail = err.Error()
		}
		set(1, Bad, detail)
		wait(2, 3, 4)
		r.Steps, r.Situation = steps, NoConf
		return r
	}
	r.ConfigPath = d.ConfigPath
	set(1, OK, d.ConfigPath)

	running := false
	if d.Transport == nil {
		set(2, Bad, "supervisord.conf has no [unix_http_server] or [inet_http_server]")
		fail(Down)
	} else {
		n, err := ping()
		switch {
		case err == nil:
			running, r.Processes = true, n
			set(2, OK, "Connected through "+where(d.Transport))
		case errors.Is(err, os.ErrPermission):
			set(2, Bad, where(d.Transport)+" isn't open to your user (it's usually owned by root)")
			fail(Denied)
		case errors.Is(err, ErrCredentials):
			set(2, Bad, err.Error())
			fail(Auth)
		case errors.Is(err, supervisor.ErrUnauthorized):
			set(2, Bad, "supervisord turned down the username and password")
			fail(Auth)
		default:
			set(2, Bad, "Nothing is listening on "+where(d.Transport))
			fail(Down)
		}
	}

	r.IncludeDir = includeDir(d)
	switch dir := r.IncludeDir; {
	case dir == "":
		set(3, Bad, "supervisord.conf has no [include] files=")
		fail(NotWritable)
	case p.Exists != nil && !p.Exists(dir):
		r.IncludeMissing = true
		set(3, Bad, dir+" doesn't exist yet")
		fail(NotWritable)
	case !p.Writable(dir):
		set(3, Bad, dir+" isn't writable for you")
		fail(NotWritable)
	default:
		set(3, OK, dir)
	}

	if !running {
		wait(4)
	} else if n, err := p.Inventory(d); err != nil {
		set(4, Bad, err.Error())
	} else {
		r.Configs = n
		steps[4].Title = fmt.Sprintf("%d %s in %d %s", r.Processes, plural(r.Processes, "process", "processes"), n, plural(n, "config", "configs"))
		set(4, OK, "")
	}

	fail(Ready)
	r.Steps = steps
	return r
}

func installedDetail(path, version string) string {
	if version == "" {
		return path
	}
	return path + ", version " + version
}

// where names the socket or port supervisord listens on.
func where(t supervisor.Transport) string {
	switch t := t.(type) {
	case supervisor.UnixTransport:
		return filepath.Base(t.Path)
	case supervisor.TCPTransport:
		return t.Addr
	}
	return t.URL()
}

// includeDir is the folder of the first [include] glob, where new programs
// go.
func includeDir(d config.Discovery) string {
	if len(d.IncludeGlobs) == 0 {
		return ""
	}
	return filepath.Dir(d.IncludeGlobs[0])
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// FindIn returns a FindSupervisord that searches path() (the login shell's
// PATH for a GUI app) and asks the binary for its version.
func FindIn(path func() []string) func() (string, string, error) {
	return func() (string, string, error) {
		bin, err := platform.LookPath("supervisord", path())
		if err != nil {
			return "", "", err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "--version").Output()
		if err != nil {
			return bin, "", nil // installed; the version is a nicety
		}
		return bin, strings.TrimSpace(string(out)), nil
	}
}

// Ping returns a Probe.Ping that connects with a short timeout and counts
// processes.
func Ping(timeout time.Duration) func(config.Discovery) (int, error) {
	return func(d config.Discovery) (int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		opts := []supervisor.Option{supervisor.WithTimeout(timeout)}
		if d.Username != "" {
			opts = append(opts, supervisor.WithBasicAuth(d.Username, d.Password))
		}
		procs, err := supervisor.New(d.Transport, opts...).GetAllProcessInfo(ctx)
		if err != nil {
			return 0, err
		}
		return len(procs), nil
	}
}
