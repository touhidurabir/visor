package config

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/touhidurabir/visor/internal/supervisor"
)

func td(name string) string { return filepath.Join("testdata", name) }

func TestDiscoverHomebrewDefault(t *testing.T) {
	t.Parallel()
	d, err := Discover([]string{td("missing.conf"), td("homebrew.conf")})
	if err != nil {
		t.Fatal(err)
	}
	if d.ConfigPath != td("homebrew.conf") {
		t.Errorf("ConfigPath = %q", d.ConfigPath)
	}
	if got, want := d.Transport, (supervisor.UnixTransport{Path: "/opt/test/var/run/supervisor.sock"}); got != want {
		t.Errorf("Transport = %#v, want %#v", got, want)
	}
	if want := []string{"/opt/test/etc/supervisor.d/*.ini"}; !reflect.DeepEqual(d.IncludeGlobs, want) {
		t.Errorf("IncludeGlobs = %v", d.IncludeGlobs)
	}
	if d.PidFile != "/opt/test/var/run/supervisord.pid" {
		t.Errorf("PidFile = %q", d.PidFile)
	}
}

func TestDiscoverTransport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file string
		want supervisor.Transport
	}{
		{"onlyfile.conf", supervisor.UnixTransport{Path: "/tmp/only-file.sock"}},
		{"inet.conf", supervisor.TCPTransport{Addr: "127.0.0.1:9001"}},
		{"inet_nohost.conf", supervisor.TCPTransport{Addr: "127.0.0.1:9001"}},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			d, err := Discover([]string{td(tc.file)})
			if err != nil {
				t.Fatal(err)
			}
			if d.Transport != tc.want {
				t.Errorf("Transport = %#v, want %#v", d.Transport, tc.want)
			}
		})
	}
}

func TestDiscoverServerURLWins(t *testing.T) {
	t.Parallel()
	d, err := Discover([]string{td("socketfile.conf")})
	if err != nil {
		t.Fatal(err)
	}
	if u, ok := d.Transport.(supervisor.UnixTransport); !ok || u.Path != "/tmp/from-url.sock" {
		t.Errorf("Transport = %#v", d.Transport)
	}
}

func TestDiscoverRelativeIncludeAndHere(t *testing.T) {
	t.Parallel()
	d, err := Discover([]string{td("relative.conf")})
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("testdata")
	want := []string{filepath.Join(abs, "conf.d/*.ini"), filepath.Join(abs, "more/*.conf")}
	if !reflect.DeepEqual(d.IncludeGlobs, want) {
		t.Errorf("IncludeGlobs = %v, want %v", d.IncludeGlobs, want)
	}
	if d.PidFile != filepath.Join(abs, "run/supervisord.pid") {
		t.Errorf("PidFile = %q", d.PidFile)
	}
}

func TestDiscoverAuth(t *testing.T) {
	t.Parallel()
	d, err := Discover([]string{td("auth.conf")})
	if err != nil {
		t.Fatal(err)
	}
	if d.Username != "admin" || d.Password != "s3cret" {
		t.Errorf("auth = %q/%q", d.Username, d.Password)
	}
}

func TestDiscoverNone(t *testing.T) {
	t.Parallel()
	_, err := Discover([]string{td("missing.conf"), td("also-missing.conf")})
	if !errors.Is(err, ErrNoConfig) {
		t.Errorf("err = %v, want ErrNoConfig", err)
	}
}

func TestDiscoverNoServerConfigured(t *testing.T) {
	t.Parallel()
	// No socket or port: Discover still succeeds so the doctor can explain.
	d, err := Discover([]string{td("relative.conf")})
	if err != nil {
		t.Fatal(err)
	}
	if d.Transport != nil {
		t.Errorf("Transport = %#v, want nil", d.Transport)
	}
}

func TestResolveOverride(t *testing.T) {
	t.Parallel()
	d, err := Resolve(td("inet.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if d.ConfigPath != td("inet.conf") {
		t.Errorf("ConfigPath = %q", d.ConfigPath)
	}
	// An override that doesn't exist must not fall back to DefaultPaths.
	if _, err := Resolve(td("nope.conf")); !errors.Is(err, ErrNoConfig) {
		t.Errorf("err = %v, want ErrNoConfig", err)
	}
}
