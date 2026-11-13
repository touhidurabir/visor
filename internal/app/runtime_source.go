package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/settings"
)

// LocalID is the id of this computer's tab.
const LocalID = "local"

var (
	// ErrRemoteReadOnly: configs on servers can be read but not changed in this version.
	ErrRemoteReadOnly = errors.New("configs on servers are read only in this version of Visor")
	// ErrServerReadOnly: the server is set to Read only, so nothing can be changed on it.
	ErrServerReadOnly = errors.New("this server is set to Read only: turn it off in the server's settings to make changes")
)

// RemoteSource makes a Runtime manage supervisord on a server.
type RemoteSource struct {
	Server settings.Server
	// Connect opens the SSH connection, discovers supervisord.conf on the
	// server (configPath, or the usual Linux paths when "") and picks how to
	// reach supervisord. ConnectServer is the real one; tests may fake it.
	Connect func(ctx context.Context, configPath string) (*RemoteSession, error)
}

// RemoteSession is one connection to a server.
type RemoteSession struct {
	// Discovery reads files through the server (FS set) and its Transport
	// reaches supervisord over SSH.
	Discovery config.Discovery
	Via       string // remote.ViaForward, ViaBridge or ViaSudo
	Address   string // user@host:port, for display
	// Lost is closed when the SSH connection drops.
	Lost  <-chan struct{}
	Close func()
	// Tail reads the end of a file on the server (supervisord's own log).
	Tail func(path string, max int) ([]byte, error)
}

// ServerSecrets reads a server's secrets from the Keychain or keyring.
func ServerSecrets(k settings.Keychain, s settings.Server) (remote.Secrets, string, error) {
	if k == nil {
		return remote.Secrets{}, "", nil
	}
	var sec remote.Secrets
	var err error
	if sec.Password, err = settings.ServerSecret(k, s.ID, settings.SecretPassword); err != nil {
		return sec, "", err
	}
	if sec.KeyPassphrase, err = settings.ServerSecret(k, s.ID, settings.SecretKeyPassphrase); err != nil {
		return sec, "", err
	}
	sudo := ""
	switch s.SudoPassword {
	case settings.SudoSameAsSSH:
		sudo = sec.Password
	case settings.SudoOwn:
		if sudo, err = settings.ServerSecret(k, s.ID, settings.SecretSudo); err != nil {
			return sec, "", err
		}
	}
	return sec, sudo, nil
}

// ServerTarget is where to connect for s, with ~/.ssh/config applied.
func ServerTarget(ctx context.Context, s settings.Server) (remote.Target, error) {
	t := remote.Target{Host: s.Host, Port: s.Port, User: s.User, KeyPath: s.KeyPath}
	if s.Port == 22 {
		t.Port = 0 // let ~/.ssh/config choose the port when the form kept the default
	}
	if s.Jump != "" {
		t.Jump = nil
		jumps, err := remote.ParseJump(s.Jump)
		if err != nil {
			return t, err
		}
		t.Jump = jumps
	}
	return resolveTarget(ctx, t)
}

// resolveTarget applies ~/.ssh/config; tests replace it so they never read
// the real one.
var resolveTarget = remote.Resolve

// ConnectServer dials s and reaches its supervisord: the real
// RemoteSource.Connect. hk holds Visor's own known_hosts.
func ConnectServer(s settings.Server, k settings.Keychain, hk remote.HostKeys) func(context.Context, string) (*RemoteSession, error) {
	return func(ctx context.Context, configPath string) (*RemoteSession, error) {
		c, sess, err := dialServer(ctx, s, k, hk)
		if err != nil {
			return nil, err
		}
		files := sess.files
		closeAll := func() {
			files.Close()
			_ = c.Close()
		}
		paths := config.PathsFor("linux")
		if configPath != "" {
			paths = []string{configPath}
		}
		fsys := remote.BridgeFS{T: files}
		d, err := config.DiscoverFS(fsys, paths)
		if err != nil {
			closeAll()
			return nil, err
		}
		addr, err := remote.AddrOf(d.Transport)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("%s: %w", d.ConfigPath, err)
		}
		a, err := remote.ReachWith(ctx, c, addr, files)
		if err != nil {
			closeAll()
			return nil, err
		}
		d.Transport = a.Transport
		return &RemoteSession{
			Discovery: d,
			Via:       a.Via,
			Address:   sess.address,
			Lost:      c.Closed(),
			Close:     closeAll,
			Tail:      fsys.Tail,
		}, nil
	}
}

type dialed struct {
	files   *remote.BridgeTransport
	address string
}

// dialServer opens the SSH connection and prepares (without starting) the
// bridge with the server's sudo settings.
func dialServer(ctx context.Context, s settings.Server, k settings.Keychain, hk remote.HostKeys) (*remote.Conn, dialed, error) {
	sec, sudo, err := ServerSecrets(k, s)
	if err != nil {
		return nil, dialed{}, err
	}
	t, err := ServerTarget(ctx, s)
	if err != nil {
		return nil, dialed{}, err
	}
	c, err := remote.Dial(ctx, t, sec, hk)
	if err != nil {
		return nil, dialed{}, err
	}
	opts := remote.BridgeOptions{Sudo: s.UseSudo, SudoPassword: sudo}
	return c, dialed{
		files:   &remote.BridgeTransport{Conn: c, Options: opts},
		address: t.User + "@" + t.Addr(),
	}, nil
}

// HostView is which machine a tab shows.
type HostView struct {
	ID       string
	Name     string
	Remote   bool
	Tag      string
	Color    string
	ReadOnly bool
	Sudo     bool
	Via      string // how supervisord is reached on a server
	Address  string // user@host:port on a server
}

// remoteServer returns the server this Runtime manages, if any.
func (r *Runtime) remoteServer() (settings.Server, bool) {
	if r.opts.Remote == nil {
		return settings.Server{}, false
	}
	return r.opts.Remote.Server, true
}

// ID is the tab id: LocalID or the server's id.
func (r *Runtime) ID() string {
	if s, ok := r.remoteServer(); ok {
		return s.ID
	}
	return LocalID
}

// Remote reports whether this Runtime manages a server.
func (r *Runtime) Remote() bool { return r.opts.Remote != nil }

// CheckConfigWrite refuses config changes on servers.
func (r *Runtime) CheckConfigWrite() error {
	if r.Remote() {
		return ErrRemoteReadOnly
	}
	return nil
}

// CheckAction refuses every action on a server set to Read only.
func (r *Runtime) CheckAction() error {
	if s, ok := r.remoteServer(); ok && s.ReadOnly {
		return ErrServerReadOnly
	}
	return nil
}

// files is how this Runtime reads supervisord's files.
func (r *Runtime) files() config.FS {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.d.Files()
}

func (r *Runtime) hostView() HostView {
	s, ok := r.remoteServer()
	if !ok {
		return HostView{ID: LocalID, Name: platform.Describe(platform.Current()).Local}
	}
	h := HostView{ID: s.ID, Name: s.Name, Remote: true, Tag: s.Tag, Color: s.Color, ReadOnly: s.ReadOnly, Sudo: s.UseSudo}
	r.mu.Lock()
	if r.session != nil {
		h.Via, h.Address = r.session.Via, r.session.Address
	}
	r.mu.Unlock()
	return h
}

// ConnErr is why the current connection failed (nil when it works or before
// it was tried).
func (r *Runtime) ConnErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connErr
}
