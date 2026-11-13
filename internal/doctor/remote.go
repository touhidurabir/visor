package doctor

import (
	"fmt"
	"slices"
	"strings"
)

// RemoteFacts are what a connection attempt found out about a server.
// Empty fields were not reached (an earlier step failed).
type RemoteFacts struct {
	Server  string // the server's name in Visor
	Login   string // user@host:port
	HostKey string // SHA256 fingerprint ("" when the SSH step failed)
	// SSHError is why the SSH login failed ("" when it worked).
	SSHError string
	// HostKeyChanged: the key differs from the one on file (refused).
	HostKeyChanged bool
	KnownHostsFix  string // the command to remove the old key

	// Bridge: the helper started (python found, sudo accepted when on).
	BridgeError string
	BridgeUser  string   // who the helper runs as (root with sudo)
	Groups      []string // the SSH user's groups (when the helper ran without sudo)
	Python      string

	Sudo bool // Use sudo is on
	// SudoResult: "accepted", "nopasswd", "refused", "needs-password",
	// "not-allowed", "needs-tty", "unconfirmed" or "" (sudo off or not reached).
	SudoResult string

	ConfigPath  string
	ConfigError string
	Chown       string // [unix_http_server] chown=

	Socket       string // unix:/path or tcp:host:port
	Via          string // forward, bridge, sudo ("" when not reached)
	SocketError  string
	SocketDenied bool
	SocketOwner  string
	SocketGroup  string
	SocketMode   string // 0700
	Service      string // systemd unit: supervisor (Debian) or supervisord

	IncludeDir      string
	Files           int
	FilesUnreadable int

	Version      string
	VersionError string
}

// Report row statuses.
const (
	RowOK      = "ok"
	RowWarning = "warning"
	RowError   = "error"
	RowInfo    = "info"
	RowSkipped = "skipped"
)

// ReportRow is one line of a server's capability report.
type ReportRow struct {
	ID     string
	Title  string
	Status string
	Detail string
	Fix    []FixStep
}

// Blocked reports whether a report stops the tab from working (no
// connection to supervisord), so the tab opens on the report.
func Blocked(rows []ReportRow) bool {
	for _, r := range rows {
		if r.Status == RowError {
			return true
		}
	}
	return false
}

// RemoteReport turns facts into the report rows, in order: SSH, sudo, config,
// socket, configs readable, configs writable, version.
func RemoteReport(f RemoteFacts) []ReportRow {
	var rows []ReportRow
	skip := func(id, title string) ReportRow {
		return ReportRow{ID: id, Title: title, Status: RowSkipped, Detail: "Not checked: an earlier step failed."}
	}

	ssh := ReportRow{ID: "ssh", Title: "SSH login"}
	switch {
	case f.HostKeyChanged:
		ssh.Status = RowError
		ssh.Detail = "The server's host key changed since it was trusted. Visor refuses the connection: it may not be the same server."
		ssh.Fix = []FixStep{{Text: "If you know why the key changed (the server was rebuilt), remove the old key, then Test again to trust the new one:", Command: f.KnownHostsFix}}
	case f.SSHError != "":
		ssh.Status = RowError
		ssh.Detail = f.SSHError
	default:
		ssh.Status = RowOK
		ssh.Detail = "Logged in as " + f.Login + ". Host key " + f.HostKey + "."
	}
	rows = append(rows, ssh)
	if ssh.Status != RowOK {
		return append(rows, skip("sudo", "sudo"), skip("config", "supervisord.conf"), skip("socket", "supervisord's socket"),
			skip("read", "Configs readable"), skip("write", "Configs writable"), skip("version", "supervisord version"))
	}

	sudo := ReportRow{ID: "sudo", Title: "sudo"}
	switch {
	case !f.Sudo:
		sudo.Status, sudo.Detail = RowInfo, "Use sudo is off: Visor works as "+loginUser(f.Login)+"."
	case f.SudoResult == "accepted":
		sudo.Status, sudo.Detail = RowOK, "sudo accepted the password. Visor asks once per connection."
	case f.SudoResult == "nopasswd":
		sudo.Status, sudo.Detail = RowOK, "sudo works without a password (NOPASSWD)."
	case f.SudoResult == "refused":
		sudo.Status, sudo.Detail = RowError, "sudo didn't accept the password. Visor won't try it again until you change it, so it can't lock the account."
		sudo.Fix = []FixStep{{Text: "Check the sudo password in this server's settings (it can differ from the SSH password)."}}
	case f.SudoResult == "needs-password":
		sudo.Status, sudo.Detail = RowError, "sudo asked for a password and none is saved."
		sudo.Fix = []FixStep{{Text: "Enter the sudo password in this server's settings."}}
	case f.SudoResult == "not-allowed":
		sudo.Status, sudo.Detail = RowError, loginUser(f.Login)+" may not use sudo on this server."
		sudo.Fix = []FixStep{{Text: "Turn Use sudo off and let " + loginUser(f.Login) + " use the socket through a group (below), or ask the server's admin to allow sudo."}}
	case f.SudoResult == "needs-tty":
		sudo.Status, sudo.Detail = RowError, "sudo on this server requires a terminal (Defaults requiretty), which Visor doesn't open."
		sudo.Fix = []FixStep{{Text: "Ask the server's admin to allow sudo without a terminal for this user:", Command: "Defaults:" + loginUser(f.Login) + " !requiretty"}}
	case f.SudoResult == "unconfirmed":
		sudo.Status, sudo.Detail = RowError, "sudo was given the password but didn't answer. Visor won't send it again until you check it."
	case f.BridgeError != "":
		sudo.Status, sudo.Detail = RowError, f.BridgeError
	default:
		sudo.Status, sudo.Detail = RowOK, "sudo works."
	}
	rows = append(rows, sudo)
	if f.BridgeError != "" && (!f.Sudo || sudo.Status == RowOK) {
		rows = append(rows, ReportRow{ID: "helper", Title: "Python on the server", Status: RowError, Detail: f.BridgeError,
			Fix: []FixStep{{Text: "Visor reads the server's files with Python, which comes with supervisord. Install python3 if it was removed:", Command: "sudo apt install python3"}}})
	}
	if sudo.Status == RowError || f.BridgeError != "" {
		return append(rows, skip("config", "supervisord.conf"), skip("socket", "supervisord's socket"),
			skip("read", "Configs readable"), skip("write", "Configs writable"), skip("version", "supervisord version"))
	}

	conf := ReportRow{ID: "config", Title: "supervisord.conf"}
	if f.ConfigError != "" {
		conf.Status, conf.Detail = RowError, f.ConfigError
		conf.Fix = []FixStep{{Text: "Install supervisor on the server, or set the config's path under Advanced in this server's settings:", Command: "sudo apt install supervisor"}}
		if !f.Sudo {
			conf.Fix = append(conf.Fix, FixStep{Text: "If the file is there but only root can read it, turn on Use sudo."})
		}
		return append(rows, conf, skip("socket", "supervisord's socket"), skip("read", "Configs readable"),
			skip("write", "Configs writable"), skip("version", "supervisord version"))
	}
	conf.Status, conf.Detail = RowOK, f.ConfigPath
	rows = append(rows, conf)

	sock := ReportRow{ID: "socket", Title: "supervisord's socket"}
	switch {
	case f.SocketDenied:
		sock.Status = RowError
		sock.Detail = fmt.Sprintf("%s belongs to %s (group %s, mode %s), so %s can't open it.", strings.TrimPrefix(f.Socket, "unix:"), f.SocketOwner, f.SocketGroup, f.SocketMode, loginUser(f.Login))
		sock.Fix = append([]FixStep{{Text: "Turn on Use sudo in this server's settings, or let " + loginUser(f.Login) + " in through a group:"}},
			GroupFix(SocketFacts{
				User: loginUser(f.Login), Owner: f.SocketOwner, Service: f.Service, Chown: f.Chown,
				Member: slices.Contains(f.Groups, ChownGroup(f.Chown)), Active: slices.Contains(f.Groups, ChownGroup(f.Chown)),
			}).Steps...)
	case f.SocketError != "":
		sock.Status, sock.Detail = RowError, f.SocketError
		sock.Fix = []FixStep{{Text: "Start supervisord on the server:", Command: "sudo systemctl start " + f.Service}}
	default:
		sock.Status = RowOK
		switch f.Via {
		case "forward":
			sock.Detail = "Forwarded over SSH as " + loginUser(f.Login) + "; nothing runs on the server."
		case "bridge":
			sock.Detail = "Reached through Visor's helper (SSH forwarding is off on the server)."
		case "sudo":
			sock.Detail = "Reached through sudo."
		}
	}
	rows = append(rows, sock)

	read := ReportRow{ID: "read", Title: "Configs readable"}
	switch {
	case f.Files == 0:
		read.Status, read.Detail = RowInfo, "No config files in "+f.IncludeDir+" yet."
	case f.FilesUnreadable > 0:
		read.Status = RowWarning
		read.Detail = fmt.Sprintf("%d of %d files in %s can't be read by %s.", f.FilesUnreadable, f.Files, f.IncludeDir, f.BridgeUser)
		if !f.Sudo {
			read.Fix = []FixStep{{Text: "Turn on Use sudo to read them."}}
		}
	default:
		read.Status = RowOK
		read.Detail = fmt.Sprintf("%d %s in %s.", f.Files, plural(f.Files, "file", "files"), f.IncludeDir)
	}
	rows = append(rows, read)
	rows = append(rows, ReportRow{ID: "write", Title: "Configs writable", Status: RowInfo,
		Detail: "Editing configs on servers comes in a later version of Visor; for now they are read only."})

	ver := ReportRow{ID: "version", Title: "supervisord version"}
	switch {
	case sock.Status != RowOK:
		ver = skip("version", "supervisord version")
	case f.VersionError != "":
		ver.Status, ver.Detail = RowError, f.VersionError
	default:
		ver.Status, ver.Detail = RowOK, f.Version
	}
	return append(rows, ver)
}

func loginUser(login string) string {
	if u, _, ok := strings.Cut(login, "@"); ok && u != "" {
		return u
	}
	return "this user"
}
