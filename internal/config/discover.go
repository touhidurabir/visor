package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// Discovery is what Visor needs from a supervisord.conf to connect and to
// find program configs.
type Discovery struct {
	ConfigPath         string
	Transport          supervisor.Transport // nil if the config defines no socket or port
	Username, Password string
	IncludeGlobs       []string // absolute
	PidFile            string   // [supervisord] pidfile=, absolute; "" if unset
	LogFile            string   // [supervisord] logfile=, absolute; "" if unset (or syslog)
	NoDaemon           bool     // [supervisord] nodaemon=true: it runs in the foreground
	// SocketChown is [unix_http_server] chown= ("root:supervisor"), "" if
	// unset: Doctor's fix for a socket owned by root depends on it.
	SocketChown string
	// FS is where the files are read from: nil means this machine's disk.
	FS FS
}

// DefaultPaths are the usual supervisord.conf locations on this OS, in
// search order (PathsFor).
var DefaultPaths = PathsFor(runtime.GOOS)

// PathsFor is where supervisord.conf usually is on goos, in search order.
// macOS: Homebrew's (Apple Silicon, then Intel), then the system ones.
// Linux: the distro packages' (Debian and Ubuntu, then Fedora), then
// Homebrew on Linux and a hand-made /usr/local one.
func PathsFor(goos string) []string {
	if goos == "darwin" {
		return []string{
			"/opt/homebrew/etc/supervisord.conf",
			"/usr/local/etc/supervisord.conf",
			"/etc/supervisord.conf",
			"/etc/supervisor/supervisord.conf",
		}
	}
	return []string{
		"/etc/supervisor/supervisord.conf",
		"/etc/supervisord.conf",
		"/home/linuxbrew/.linuxbrew/etc/supervisord.conf",
		"/usr/local/etc/supervisord.conf",
	}
}

// ErrNoConfig means none of the candidate paths could be read.
var ErrNoConfig = errors.New("no supervisord config found")

// ErrNotSupervisord means a file was read but has none of the sections a
// supervisord.conf has (e.g. someone picked the wrong .ini).
var ErrNotSupervisord = errors.New("not a supervisord config")

// mainSections are the sections that mark a file as a supervisord.conf.
// [supervisord] alone isn't required: it may come from an included file.
var mainSections = []string{"supervisord", "unix_http_server", "inet_http_server", "supervisorctl", "include"}

// Discover reads the first readable path in paths. A file that exists but
// doesn't parse is an error, not a reason to try the next path.
func Discover(paths []string) (Discovery, error) { return DiscoverFS(OS, paths) }

// DiscoverFS is Discover on another file system (a server's). Paths are
// absolute there.
func DiscoverFS(fsys FS, paths []string) (Discovery, error) {
	for _, p := range paths {
		data, err := fsys.ReadFile(p)
		if err != nil {
			continue
		}
		d, err := fromConfig(p, string(data))
		if err == nil && fsys != OS {
			d.FS = fsys
		}
		return d, err
	}
	return Discovery{}, fmt.Errorf("%w (looked in %s)", ErrNoConfig, strings.Join(paths, ", "))
}

// Resolve is Discover for a user-chosen path: a non-empty override is the
// only path tried; otherwise DefaultPaths are searched.
func Resolve(override string) (Discovery, error) {
	if override != "" {
		return Discover([]string{override})
	}
	return Discover(DefaultPaths)
}

func fromConfig(path, src string) (Discovery, error) {
	f, err := ParseINI(strings.NewReader(src), path)
	if err != nil {
		return Discovery{}, err
	}
	if !hasAnySection(f, mainSections) {
		return Discovery{}, fmt.Errorf("%s: %w (it has no [supervisord], [unix_http_server], [inet_http_server], [supervisorctl] or [include] section)", path, ErrNotSupervisord)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Discovery{}, err
	}
	here := filepath.Dir(abs)
	vars := map[string]string{"here": here}
	get := func(section, key string) string {
		s, ok := f.Section(section)
		if !ok {
			return ""
		}
		v, _ := s.Get(key)
		return Expand(v, vars)
	}
	absolute := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(here, p)
	}

	d := Discovery{
		ConfigPath:  path,
		Username:    get("supervisorctl", "username"),
		Password:    get("supervisorctl", "password"),
		SocketChown: strings.TrimSpace(get("unix_http_server", "chown")),
	}
	if p := get("supervisord", "pidfile"); p != "" {
		d.PidFile = absolute(p)
	}
	if p := get("supervisord", "logfile"); p != "" && !strings.EqualFold(p, "syslog") {
		d.LogFile = absolute(p)
	}
	switch strings.ToLower(strings.TrimSpace(get("supervisord", "nodaemon"))) {
	case "true", "yes", "on", "1":
		d.NoDaemon = true
	}
	for _, g := range strings.Fields(get("include", "files")) {
		d.IncludeGlobs = append(d.IncludeGlobs, absolute(g))
	}

	switch url := get("supervisorctl", "serverurl"); {
	case strings.HasPrefix(url, "unix://"):
		d.Transport = supervisor.UnixTransport{Path: absolute(strings.TrimPrefix(url, "unix://"))}
	case strings.HasPrefix(url, "http://"):
		d.Transport = supervisor.TCPTransport{Addr: strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/")}
	case get("unix_http_server", "file") != "":
		d.Transport = supervisor.UnixTransport{Path: absolute(get("unix_http_server", "file"))}
	case get("inet_http_server", "port") != "":
		d.Transport = supervisor.TCPTransport{Addr: inetAddr(get("inet_http_server", "port"))}
	}
	return d, nil
}

// inetAddr turns an inet_http_server port value (":9001", "*:9001", "9001",
// "host:9001") into a dialable address; a missing or wildcard host means
// loopback.
func inetAddr(port string) string {
	host, p, found := strings.Cut(port, ":")
	if !found {
		host, p = "", port
	}
	if host == "" || host == "*" {
		host = "127.0.0.1"
	}
	return host + ":" + p
}

func hasAnySection(f File, names []string) bool {
	for _, n := range names {
		if _, ok := f.Section(n); ok {
			return true
		}
	}
	return false
}
