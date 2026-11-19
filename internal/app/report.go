package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/doctor"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/settings"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// probeTimeout bounds a whole Test.
const probeTimeout = 45 * time.Second

// HostKeyView is a host key Visor asks about: one it hasn't seen (Status
// "unknown", trust it with RemoteService.Trust) or one that changed
// ("changed", refused).
type HostKeyView struct {
	Status      string // "unknown" or "changed"
	Host        string // host:port as dialled
	Type        string // ssh-ed25519, ecdsa-sha2-nistp256, …
	Fingerprint string
	Old         []string // changed: the fingerprints on file
	File        string   // changed: where the old key is
	Line        int
	Fix         string // changed: the command that removes the old key
}

// ReportView is a server's capability report.
type ReportView struct {
	Rows    []doctor.ReportRow
	Blocked bool // something stops the tab from working
	// Credentials: a password or key was refused. The report is never
	// re-checked on its own then: each try would count against the lockout.
	Credentials bool
	HostKey     *HostKeyView // set when the host key needs the user
	AtMs        int64
}

// pendingKey is a host key shown to the user, kept so Trust writes the very
// key they saw (by fingerprint), not whatever the server says later.
type pendingKey struct {
	host string
	port int
	key  ssh.PublicKey
}

func hostKeyView(err error) (*HostKeyView, ssh.PublicKey) {
	var unknown *remote.UnknownHostError
	var changed *remote.HostKeyChangedError
	switch {
	case errors.As(err, &unknown):
		return &HostKeyView{Status: "unknown", Host: unknown.Host, Type: unknown.Key.Type(), Fingerprint: unknown.Fingerprint}, unknown.Key
	case errors.As(err, &changed):
		v := &HostKeyView{Status: "changed", Host: changed.Host, Fingerprint: changed.New, Old: changed.Old, File: changed.File, Line: changed.Line}
		host := changed.Host
		if h, p, splitErr := splitHostPort(host); splitErr == nil && p == "22" {
			host = h
		} else if splitErr == nil {
			host = "[" + h + "]:" + p
		}
		v.Fix = fmt.Sprintf("ssh-keygen -R '%s' -f '%s'", host, changed.File)
		return v, nil
	}
	return nil, nil
}

func splitHostPort(hp string) (string, string, error) {
	i := strings.LastIndex(hp, ":")
	if i < 0 {
		return hp, "", errors.New("no port")
	}
	return strings.Trim(hp[:i], "[]"), hp[i+1:], nil
}

// sshErrorText words a failed login for the report.
func sshErrorText(err error, t remote.Target) string {
	switch {
	case errors.Is(err, remote.ErrAuthFailed):
		return "The server refused the login for " + t.User + ". Check the user, password or key."
	case errors.Is(err, remote.ErrPassphraseNeeded):
		return "The key file is locked with a passphrase. Enter it, or add the key to your SSH agent (ssh-add --apple-use-keychain on a Mac)."
	case errors.Is(err, remote.ErrPassphraseWrong):
		return "The passphrase didn't open the key file."
	case errors.Is(err, remote.ErrNoAuth):
		return "There's nothing to log in with: enter a password, choose a key file, or start your SSH agent."
	case errors.Is(err, remote.ErrProxyCommand):
		return err.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return "No answer from " + t.Addr() + " in time. Check the host and port, and that the server is up."
	}
	msg := err.Error()
	if strings.Contains(msg, "connection refused") {
		return "Nothing answers SSH on " + t.Addr() + "."
	}
	if strings.Contains(msg, "no such host") {
		return "The host name " + t.Host + " can't be found."
	}
	return "Can't connect to " + t.Addr() + ": " + msg
}

func sudoResult(err error, b *remote.Bridge) string {
	switch {
	case err == nil && b != nil && b.Prompted():
		return "accepted"
	case err == nil:
		return "nopasswd"
	case errors.Is(err, remote.ErrSudoRefused):
		return "refused"
	case errors.Is(err, remote.ErrSudoPasswordNeeded):
		return "needs-password"
	case errors.Is(err, remote.ErrSudoNotAllowed):
		return "not-allowed"
	case errors.Is(err, remote.ErrSudoNeedsTTY):
		return "needs-tty"
	case errors.Is(err, remote.ErrSudoUnconfirmed):
		return "unconfirmed"
	}
	return ""
}

// serviceFor guesses the systemd unit from where the config is.
func serviceFor(configPath string) string {
	if strings.HasPrefix(configPath, "/etc/supervisor/") {
		return "supervisor"
	}
	return "supervisord"
}

// probeServer connects to s with the given secrets and reports what works.
// It leaves nothing running: the connection is closed before it returns.
// key is the host key to trust when the report asks about one.
func probeServer(ctx context.Context, s settings.Server, sec remote.Secrets, sudoPW string, hk remote.HostKeys) (ReportView, ssh.PublicKey) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	f := doctor.RemoteFacts{Server: s.Name, Sudo: s.UseSudo}
	credentials := false
	done := func(key ssh.PublicKey, hkv *HostKeyView) (ReportView, ssh.PublicKey) {
		rows := doctor.RemoteReport(f)
		return ReportView{Rows: rows, Blocked: doctor.Blocked(rows) || hkv != nil, Credentials: credentials, HostKey: hkv, AtMs: time.Now().UnixMilli()}, key
	}
	t, err := ServerTarget(ctx, s)
	if err != nil {
		f.SSHError = err.Error()
		return done(nil, nil)
	}
	f.Login = t.User + "@" + t.Addr()
	c, err := remote.Dial(ctx, t, sec, hk)
	if err != nil {
		hkv, key := hostKeyView(err)
		switch {
		case hkv != nil && hkv.Status == "changed":
			f.HostKeyChanged, f.KnownHostsFix = true, hkv.Fix
		case hkv != nil:
			f.SSHError = "Visor hasn't seen this server's host key before. Check its fingerprint and trust it to go on."
		default:
			f.SSHError = sshErrorText(err, t)
			credentials = remote.IsCredentialError(err)
		}
		return done(key, hkv)
	}
	defer func() { _ = c.Close() }()
	f.HostKey = "connected"
	if k := c.HostKey(); k != nil {
		f.HostKey = remote.Fingerprint(k)
	}

	files := &remote.BridgeTransport{Conn: c, Options: remote.BridgeOptions{Sudo: s.UseSudo, SudoPassword: sudoPW}}
	defer files.Close()
	b, err := files.Bridge(ctx)
	if s.UseSudo {
		f.SudoResult = sudoResult(err, b)
		credentials = err != nil && remote.IsCredentialError(err)
	}
	if err != nil {
		if f.SudoResult == "" {
			f.BridgeError = err.Error()
			if errors.Is(err, remote.ErrNoPython) {
				f.BridgeError = "Neither python3 nor python is installed on the server."
			}
		}
		return done(nil, nil)
	}
	f.BridgeUser, f.Groups, f.Python = b.Hello().User, b.Hello().Groups, b.Hello().Python

	paths := config.PathsFor("linux")
	if s.ConfigPath != "" {
		paths = []string{s.ConfigPath}
	}
	d, err := config.DiscoverFS(remote.BridgeFS{T: files}, paths)
	if err != nil {
		f.ConfigError = configErrorText(err, paths)
		return done(nil, nil)
	}
	f.ConfigPath, f.Chown, f.Service = d.ConfigPath, d.SocketChown, serviceFor(d.ConfigPath)
	if len(d.IncludeGlobs) > 0 {
		f.IncludeDir = filepath.Dir(d.IncludeGlobs[0])
		fsys := remote.BridgeFS{T: files}
		f.IncludeChecked = true
		if fi, err := fsys.Stat(f.IncludeDir); err != nil {
			f.IncludeMissing = errors.Is(err, fs.ErrNotExist)
		} else if rf, ok := fi.Sys().(remote.FileInfo); ok {
			f.IncludeGroup = rf.Group
		}
		f.IncludeWritable = !f.IncludeMissing && fsys.FolderWritable(f.IncludeDir)
	}
	if inv, err := config.LoadInventory(d); err == nil {
		f.Files = len(inv.Files)
		for _, cf := range inv.Files {
			if cf.Err != nil && errors.Is(cf.Err, fs.ErrPermission) {
				f.FilesUnreadable++
			}
		}
	}
	addr, err := remote.AddrOf(d.Transport)
	if err != nil {
		f.SocketError = d.ConfigPath + " defines no socket: add an [unix_http_server] section with file= set."
		return done(nil, nil)
	}
	f.Socket = addr
	a, err := remote.ReachWith(ctx, c, addr, files)
	if err != nil {
		var denied *remote.SocketDeniedError
		switch {
		case errors.As(err, &denied):
			f.SocketDenied = true
			f.SocketOwner, f.SocketGroup, f.SocketMode = denied.Owner, denied.Group, fmt.Sprintf("%04o", denied.Mode.Perm())
		case errors.Is(err, remote.ErrSocketMissing):
			f.SocketError = "There's no socket at " + strings.TrimPrefix(addr, "unix:") + ": supervisord isn't running."
		case errors.Is(err, remote.ErrNotListening):
			f.SocketError = "supervisord isn't listening on " + strings.TrimPrefix(addr, "unix:") + " (it stopped and left the socket file behind)."
		default:
			f.SocketError = err.Error()
		}
		return done(nil, nil)
	}
	f.Via = a.Via
	opts := []supervisor.Option{supervisor.WithTimeout(10 * time.Second)}
	if d.Username != "" {
		opts = append(opts, supervisor.WithBasicAuth(d.Username, d.Password))
	}
	cl := supervisor.New(a.Transport, opts...)
	defer cl.Close()
	if v, err := cl.GetSupervisorVersion(ctx); err != nil {
		f.VersionError = "supervisord didn't answer: " + err.Error()
	} else {
		f.Version = v
	}
	return done(nil, nil)
}

func configErrorText(err error, paths []string) string {
	switch {
	case errors.Is(err, config.ErrNoConfig):
		return "No supervisord.conf found (looked in " + strings.Join(paths, ", ") + ")."
	case errors.Is(err, config.ErrNotSupervisord):
		return err.Error()
	}
	return err.Error()
}

// credentialReport is the report for a connection that failed on a password
// or key, built without connecting again (that would be another try).
func credentialReport(s settings.Server, err error) ReportView {
	f := doctor.RemoteFacts{Server: s.Name, Sudo: s.UseSudo, Login: s.User + "@" + s.Host}
	if r := sudoResult(err, nil); r != "" && r != "nopasswd" {
		f.HostKey, f.SudoResult = "connected", r
	} else {
		f.SSHError = sshErrorText(err, remote.Target{Host: s.Host, Port: s.Port, User: s.User})
	}
	rows := doctor.RemoteReport(f)
	return ReportView{Rows: rows, Blocked: true, Credentials: true, AtMs: time.Now().UnixMilli()}
}
