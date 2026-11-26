package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/touhidurabir/visor/internal/apply"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/usage"
)

// LocalID is the id of this computer's tab.
const LocalID = "local"

var (
	// ErrServerReadOnly: the server is set to Read only, so nothing can be changed on it.
	ErrServerReadOnly = errors.New("this server is set to Read only: turn it off in the server's settings to make changes")
)

// RemoteSource makes a Runtime manage supervisord on a server.
type RemoteSource struct {
	Server settings.Server
	// Connect opens the SSH connection, discovers supervisord.conf on the
	// server and picks how to reach supervisord. ConnectServer is the real
	// one; tests may fake it.
	Connect ConnectFunc
}

// ConnectFunc connects to a server as it is saved now (a reconnect after an
// edit uses the edited user, host and secrets). s.ConfigPath "" discovers
// supervisord.conf in the usual Linux places.
type ConnectFunc func(ctx context.Context, s settings.Server) (*RemoteSession, error)

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
	// Server changes and checks the server's files through the helper
	// (apply.Disk), as Helper.User (root with Use sudo).
	Server remote.BridgeFS
	Helper remote.Hello
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
		// Each hop logs in with its own key or the agent (never the
		// server's password), so it needs ~/.ssh/config applied too.
		for i := range jumps {
			if jumps[i], err = resolveTarget(ctx, jumps[i]); err != nil {
				return t, fmt.Errorf("jump host %s: %w", jumps[i].Host, err)
			}
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
func ConnectServer(k settings.Keychain, hk remote.HostKeys) ConnectFunc {
	return func(ctx context.Context, s settings.Server) (*RemoteSession, error) {
		configPath := s.ConfigPath
		c, sess, err := dialServer(ctx, s, k, hk)
		if err != nil {
			return nil, err
		}
		files := sess.files
		closeAll := func() {
			files.Close()
			_ = c.Close()
		}
		// File reads have their own timeouts: closing the connection when
		// ctx ends stops them at once (a tab closed or Visor quitting).
		stop := context.AfterFunc(ctx, closeAll)
		defer stop()
		// Start the helper first, so sudo's answer (a refused password)
		// comes back as it is, not as "no config found".
		b, err := files.Bridge(ctx)
		if err != nil {
			closeAll()
			return nil, err
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
			Server:    fsys,
			Helper:    b.Hello(),
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
	// Conn is a server's connection: ConnConnected, ConnReconnecting or
	// ConnFailed ("" on this computer, or before it connected).
	Conn      string
	ConnError string
}

// remoteServer returns the server this Runtime manages, if any, as last
// saved (SetServer).
func (r *Runtime) remoteServer() (settings.Server, bool) {
	if r.opts.Remote == nil {
		return settings.Server{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.server == nil {
		return r.opts.Remote.Server, true
	}
	return *r.server, true
}

// SetServer puts a saved server's new name, tag, colour and Read only into
// use on its open tab at once; connection settings apply when it reopens.
func (r *Runtime) SetServer(s settings.Server) {
	if r.opts.Remote == nil {
		return
	}
	r.mu.Lock()
	r.server = &s
	r.mu.Unlock()
	r.sizeMu.Lock()
	r.size = usage.Machine{} // the edit may point at another machine
	r.sizeMu.Unlock()
	r.emitOverview()
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

// CheckConfigWrite refuses config changes on a server set to Read only.
func (r *Runtime) CheckConfigWrite() error { return r.CheckAction() }

// disk is where this Runtime changes config files: this machine, or the
// server through its helper.
func (r *Runtime) disk() apply.Disk {
	if !r.Remote() {
		return apply.Local
	}
	f, _ := r.serverFS()
	return f
}

// serverFS is the connected server's files through the helper, and who the
// helper runs as. Not connected, its calls fail.
func (r *Runtime) serverFS() (remote.BridgeFS, remote.Hello) {
	r.mu.Lock()
	l := r.link
	r.mu.Unlock()
	if l == nil {
		return remote.BridgeFS{}, remote.Hello{}
	}
	s, err := l.current()
	if err != nil {
		return remote.BridgeFS{}, remote.Hello{}
	}
	return s.Server, s.Helper
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
	link := r.link
	r.mu.Unlock()
	if link != nil {
		var err error
		h.Conn, err = link.status()
		if err != nil {
			h.ConnError = err.Error()
		}
	}
	return h
}

// ConnErr is why the current connection failed (nil when it works or before
// it was tried).
func (r *Runtime) ConnErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connErr
}

// IssuesRoute is the main window's route to a tab's Issues ("" or LocalID:
// this computer's).
func IssuesRoute(server string) string {
	if server == "" || server == LocalID || !settings.ValidServerID(server) {
		return RouteIssues
	}
	return "#/s/" + server + "/issues"
}
