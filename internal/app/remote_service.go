package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/crypto/ssh"

	"github.com/touhidurabir/visor/internal/doctor"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/settings"
)

// RemoteService manages saved servers and their tabs: the form's Test, Save
// and Connect, the Remote servers page, and the tab bar's open and close.
type RemoteService struct {
	hub      *Hub
	stateDir string
	keychain settings.Keychain
	hk       remote.HostKeys
	prefs    func() settings.Settings
	// probe runs Test; tests replace it.
	probe func(ctx context.Context, s settings.Server, sec remote.Secrets, sudo string, hk remote.HostKeys) (ReportView, ssh.PublicKey)

	mu      sync.Mutex // serialises servers.json changes
	pending map[string]pendingKey
	reports map[string]ReportView // the latest report per server id
	// unsaved are servers connected without saving: their tab and secrets
	// (in memory) last until the tab closes or Visor quits.
	unsaved map[string]unsavedServer
}

type unsavedServer struct {
	server settings.Server
	k      *settings.MemoryKeychain
}

// NewRemoteService returns the service over hub.
func NewRemoteService(hub *Hub, stateDir string, k settings.Keychain, hk remote.HostKeys, prefs func() settings.Settings) *RemoteService {
	pruneServerState(stateDir)
	return &RemoteService{
		hub: hub, stateDir: stateDir, keychain: k, hk: hk, prefs: prefs, probe: probeServer,
		pending: map[string]pendingKey{}, reports: map[string]ReportView{}, unsaved: map[string]unsavedServer{},
	}
}

// pruneServerState removes the history of servers that aren't saved: one
// connected without saving whose tab was still open when Visor quit.
func pruneServerState(stateDir string) {
	list, err := settings.LoadServers(stateDir)
	if err != nil {
		return // unreadable servers.json: never delete what it may name
	}
	entries, _ := os.ReadDir(filepath.Join(stateDir, "servers"))
	for _, e := range entries {
		id := e.Name()
		if e.IsDir() && settings.ValidServerID(id) && !slices.ContainsFunc(list, func(x settings.Server) bool { return x.ID == id }) {
			_ = settings.RemoveServerState(stateDir, id)
		}
	}
}

// SavedServerView is a saved server for the list and the form: which
// secrets are saved (never the secrets) and whether its tab is open.
type SavedServerView struct {
	Server           settings.Server
	HasPassword      bool
	HasKeyPassphrase bool
	HasSudoPassword  bool
	Open             bool
	// Conn is an open tab's connection: ConnConnected, ConnReconnecting or
	// ConnFailed ("" when not open).
	Conn string
	// Unsaved: connected without saving; it is gone when its tab closes.
	Unsaved bool
}

// SecretInput changes one secret: Keep leaves the saved one as it is,
// otherwise Value replaces it ("" removes it).
type SecretInput struct {
	Keep  bool
	Value string
}

// ServerInput is the form: the server and what to do with each secret.
type ServerInput struct {
	Server        settings.Server
	Password      SecretInput
	KeyPassphrase SecretInput
	SudoPassword  SecretInput
}

// LimitsView is the server limit for the tab bar.
type LimitsView struct {
	MaxServers int
	Open       int
}

// List returns the saved servers, and those connected without saving, by name.
func (s *RemoteService) List() ([]SavedServerView, error) {
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	for _, u := range s.unsaved {
		list = append(list, u.server)
	}
	s.mu.Unlock()
	open := s.hub.Open()
	out := make([]SavedServerView, 0, len(list))
	for _, sv := range list {
		v := SavedServerView{Server: sv, Open: slices.Contains(open, sv.ID)}
		k, unsaved := s.keychainFor(sv.ID)
		if unsaved && !v.Open {
			continue // still connecting, or failing: not a server yet
		}
		v.Unsaved = unsaved
		v.HasPassword = has(k, sv.ID, settings.SecretPassword)
		v.HasKeyPassphrase = has(k, sv.ID, settings.SecretKeyPassphrase)
		v.HasSudoPassword = has(k, sv.ID, settings.SecretSudo)
		if t, err := s.hub.Tab(sv.ID); err == nil && v.Open {
			v.Conn = t.RT.hostView().Conn
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b SavedServerView) int {
		switch {
		case a.Server.Name < b.Server.Name:
			return -1
		case a.Server.Name > b.Server.Name:
			return 1
		}
		return 0
	})
	return out, nil
}

func has(k settings.Keychain, id, kind string) bool {
	v, err := settings.ServerSecret(k, id, kind)
	return err == nil && v != ""
}

// keychainFor is where a server's secrets are: in memory for one connected
// without saving (unsaved true), else the Keychain or keyring.
func (s *RemoteService) keychainFor(id string) (settings.Keychain, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.unsaved[id]; ok {
		return u.k, true
	}
	return s.keychain, false
}

// find returns a saved server, or one connected without saving.
func (s *RemoteService) find(id string) (settings.Server, error) {
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return settings.Server{}, err
	}
	if i := slices.IndexFunc(list, func(x settings.Server) bool { return x.ID == id }); i >= 0 {
		return list[i], nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.unsaved[id]; ok {
		return u.server, nil
	}
	return settings.Server{}, errors.New("that server isn't saved any more")
}

// Limits reports how many servers may be open and how many are.
func (s *RemoteService) Limits() LimitsView {
	return LimitsView{MaxServers: s.prefs().Clamp().MaxServers, Open: len(s.hub.Open())}
}

// Tabs returns the open server tabs in order (for a window that reloads).
func (s *RemoteService) Tabs() ([]settings.Server, error) {
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return nil, err
	}
	var out []settings.Server
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.hub.Open() {
		if i := slices.IndexFunc(list, func(x settings.Server) bool { return x.ID == id }); i >= 0 {
			out = append(out, list[i])
		} else if u, ok := s.unsaved[id]; ok {
			out = append(out, u.server)
		}
	}
	return out, nil
}

// Save validates and stores a server (a new one gets an id) and its
// secrets. An open tab keeps its connection until it is reopened.
func (s *RemoteService) Save(in ServerInput) (settings.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return settings.Server{}, err
	}
	sv := in.Server.Normalize()
	if sv.ID == "" {
		sv.ID = settings.NewServerID()
	} else if !settings.ValidServerID(sv.ID) {
		return settings.Server{}, fmt.Errorf("bad server id %q", sv.ID)
	}
	if err := sv.Validate(list); err != nil {
		return settings.Server{}, err
	}
	if sv.KeyPath != "" {
		if _, err := remote.ParseKeyFile(sv.KeyPath); err != nil {
			return settings.Server{}, &settings.FieldError{Field: "keyPath", Msg: "Visor can't read that key file: " + err.Error()}
		}
	}
	u, wasUnsaved := s.unsaved[sv.ID]
	var noKeyring error
	for kind, si := range map[string]SecretInput{
		settings.SecretPassword: in.Password, settings.SecretKeyPassphrase: in.KeyPassphrase, settings.SecretSudo: in.SudoPassword,
	} {
		if si.Keep && wasUnsaved {
			// Saving a server connected without saving: its secrets move
			// from memory to the Keychain or keyring.
			si.Value, _ = settings.ServerSecret(u.k, sv.ID, kind)
		} else if si.Keep {
			continue
		}
		err := settings.SetServerSecret(s.keychain, sv.ID, kind, si.Value)
		switch {
		case errors.Is(err, settings.ErrNoKeyring):
			noKeyring = err // kept until Visor quits; said after saving
		case err != nil:
			return settings.Server{}, err
		}
	}
	if i := slices.IndexFunc(list, func(x settings.Server) bool { return x.ID == sv.ID }); i >= 0 {
		list[i] = sv // the form carries the mutes too (its Notifications fold)
	} else {
		list = append(list, sv)
	}
	if err := settings.SaveServers(s.stateDir, list); err != nil {
		return settings.Server{}, err
	}
	delete(s.unsaved, sv.ID)
	// An open tab takes the new name, tag and Read only at once.
	if t, err := s.hub.Tab(sv.ID); err == nil && t.RT.Remote() {
		t.RT.SetServer(sv)
		s.hub.TabsChanged() // the popover's sections follow the new tag and Read only
	}
	if noKeyring != nil {
		return sv, fmt.Errorf("%s was saved, but %w", sv.Name, noKeyring)
	}
	return sv, nil
}

// Delete closes the server's tab and removes it, its secrets and its
// history (issues, activity) from this computer.
func (s *RemoteService) Delete(id string) error {
	if !settings.ValidServerID(id) {
		return fmt.Errorf("bad server id %q", id)
	}
	_ = s.hub.CloseServer(id)
	s.forget(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return err
	}
	list = slices.DeleteFunc(list, func(x settings.Server) bool { return x.ID == id })
	if err := settings.SaveServers(s.stateDir, list); err != nil {
		return err
	}
	if err := settings.DeleteServerSecrets(s.keychain, id); err != nil {
		return err
	}
	return settings.RemoveServerState(s.stateDir, id)
}

// secretsFor picks the form's secrets, falling back to the saved ones.
func (s *RemoteService) secretsFor(in ServerInput) (remote.Secrets, string, error) {
	sv := in.Server.Normalize()
	saved := settings.Server{ID: sv.ID, SudoPassword: settings.SudoOwn}
	var sec remote.Secrets
	var sudo string
	if settings.ValidServerID(sv.ID) {
		var err error
		k, _ := s.keychainFor(sv.ID)
		if sec, sudo, err = ServerSecrets(k, saved); err != nil {
			return sec, "", err
		}
	}
	if !in.Password.Keep {
		sec.Password = in.Password.Value
	}
	if !in.KeyPassphrase.Keep {
		sec.KeyPassphrase = in.KeyPassphrase.Value
	}
	if !in.SudoPassword.Keep {
		sudo = in.SudoPassword.Value
	}
	switch sv.SudoPassword {
	case settings.SudoSameAsSSH:
		sudo = sec.Password
	case settings.SudoNone:
		sudo = ""
	}
	return sec, sudo, nil
}

// Test connects with the form as it is (nothing saved) and returns the
// capability report. A host key Visor hasn't seen comes back in HostKey
// for the user to check and Trust; Test again then goes through.
func (s *RemoteService) Test(in ServerInput) (ReportView, error) {
	sv := in.Server.Normalize()
	if sv.Host == "" {
		return ReportView{}, &settings.FieldError{Field: "host", Msg: "Enter the server's host name or address."}
	}
	sec, sudo, err := s.secretsFor(in)
	if err != nil {
		return ReportView{}, err
	}
	rep, key := s.probe(context.Background(), sv, sec, sudo, s.hk)
	s.remember(rep, key)
	if settings.ValidServerID(sv.ID) {
		s.mu.Lock()
		s.reports[sv.ID] = rep
		s.mu.Unlock()
	}
	return rep, nil
}

func (s *RemoteService) remember(rep ReportView, key ssh.PublicKey) {
	if rep.HostKey == nil || rep.HostKey.Status != "unknown" || key == nil {
		return
	}
	host, port, err := splitHostPort(rep.HostKey.Host)
	if err != nil {
		return
	}
	p := 22
	_, _ = fmt.Sscanf(port, "%d", &p)
	s.mu.Lock()
	s.pending[rep.HostKey.Host+" "+rep.HostKey.Fingerprint] = pendingKey{host: host, port: p, key: key}
	s.mu.Unlock()
}

// Trust records the host key of host (host:port as the prompt showed it)
// with this fingerprint, one Test or Open just showed, in Visor's own
// known_hosts. ~/.ssh/known_hosts is never changed.
func (s *RemoteService) Trust(host, fingerprint string) error {
	s.mu.Lock()
	pk, ok := s.pending[host+" "+fingerprint]
	delete(s.pending, host+" "+fingerprint)
	s.mu.Unlock()
	if !ok {
		return errors.New("that host key is no longer waiting to be trusted: test the connection again")
	}
	return s.hk.Trust(pk.host, pk.port, pk.key)
}

// OpenView is the result of opening a server's tab. HostKey or Report is
// set when the tab could not connect and the user has something to do.
type OpenView struct {
	Overview *OverviewView
	HostKey  *HostKeyView
	Report   *ReportView
}

// Open opens a saved server's tab. A host key that needs the user, or a
// connection that fails, comes back as a report instead of a tab: no tab
// stays open on a server Visor can't reach.
func (s *RemoteService) Open(id string) (OpenView, error) {
	sv, err := s.find(id)
	if err != nil {
		return OpenView{}, err
	}
	k, _ := s.keychainFor(id)
	return s.open(sv, k)
}

// ConnectOnce connects to the form's server without saving it: its secrets
// stay in memory, and the server is forgotten when its tab closes. One that
// can't connect leaves nothing behind.
func (s *RemoteService) ConnectOnce(in ServerInput) (OpenView, error) {
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return OpenView{}, err
	}
	sv := in.Server.Normalize()
	sv.ID = settings.NewServerID()
	if sv.Name == "" {
		sv.Name = sv.Host
	}
	if err := sv.Validate(list); err != nil {
		return OpenView{}, err
	}
	if sv.KeyPath != "" {
		if _, err := remote.ParseKeyFile(sv.KeyPath); err != nil {
			return OpenView{}, &settings.FieldError{Field: "keyPath", Msg: "Visor can't read that key file: " + err.Error()}
		}
	}
	k := &settings.MemoryKeychain{}
	for kind, si := range map[string]SecretInput{
		settings.SecretPassword: in.Password, settings.SecretKeyPassphrase: in.KeyPassphrase, settings.SecretSudo: in.SudoPassword,
	} {
		if err := settings.SetServerSecret(k, sv.ID, kind, si.Value); err != nil {
			return OpenView{}, err
		}
	}
	s.mu.Lock()
	s.unsaved[sv.ID] = unsavedServer{server: sv, k: k}
	s.mu.Unlock()
	res, err := s.open(sv, k)
	if err != nil || res.Overview == nil {
		s.forget(sv.ID)
	}
	return res, err
}

// forget drops a server connected without saving, and its history.
func (s *RemoteService) forget(id string) {
	s.mu.Lock()
	_, ok := s.unsaved[id]
	if ok {
		delete(s.unsaved, id)
		delete(s.reports, id)
	}
	s.mu.Unlock()
	if ok {
		_ = settings.RemoveServerState(s.stateDir, id)
	}
}

func (s *RemoteService) open(sv settings.Server, k settings.Keychain) (OpenView, error) {
	id := sv.ID
	if t, err := s.hub.Tab(id); err == nil && slices.Contains(s.hub.Open(), id) {
		ov := t.RT.Overview()
		return OpenView{Overview: &ov}, nil
	}
	if l := s.Limits(); l.Open >= l.MaxServers {
		return OpenView{}, &TooManyServersError{Max: l.MaxServers}
	}
	ov, err := s.hub.OpenServerWith(sv, k)
	var failed *ConnectError
	if !errors.As(err, &failed) {
		if err != nil {
			return OpenView{}, err
		}
		return OpenView{Overview: &ov}, nil
	}
	// It didn't connect (no tab opened): find out why, step by step. Only
	// then is the connection made a second time (sudo is asked once per
	// connection).
	cerr := failed.Err
	if hkv, key := hostKeyView(cerr); hkv != nil {
		rep := ReportView{HostKey: hkv, Blocked: true, AtMs: time.Now().UnixMilli()}
		if hkv.Status == "changed" {
			rep.Rows = doctor.RemoteReport(doctor.RemoteFacts{Server: sv.Name, HostKeyChanged: true, KnownHostsFix: hkv.Fix})
		}
		s.remember(rep, key)
		return OpenView{HostKey: hkv, Report: &rep}, nil
	}
	var rep ReportView
	if remote.IsCredentialError(cerr) {
		rep = credentialReport(sv, cerr) // a refused password: said without trying it again
	} else {
		sec, sudo, err := ServerSecrets(k, sv)
		if err != nil {
			return OpenView{}, err
		}
		var key ssh.PublicKey
		rep, key = s.probe(context.Background(), sv, sec, sudo, s.hk)
		s.remember(rep, key)
		if !rep.Blocked {
			// The checks pass now (it was a blip): say why it failed.
			rep.Blocked = true
			rep.Rows = append(rep.Rows, doctor.ReportRow{ID: "connect", Title: "Connecting", Status: doctor.RowError, Detail: cerr.Error()})
		}
	}
	s.mu.Lock()
	s.reports[id] = rep
	s.mu.Unlock()
	return OpenView{Report: &rep}, nil
}

// Close closes a server's tab (the server stays saved; one connected
// without saving is forgotten).
func (s *RemoteService) Close(id string) error {
	err := s.hub.CloseServer(id)
	s.forget(id)
	return err
}

// Retry makes a server tab that lost its connection try to reconnect now,
// instead of at its next try (up to a minute away).
func (s *RemoteService) Retry(id string) error {
	t, err := s.hub.Tab(id)
	if err != nil {
		return err
	}
	t.RT.RetryNow()
	return nil
}

// Report returns the latest capability report for a server (from Test,
// Open or Recheck); ok is false when there is none yet.
func (s *RemoteService) Report(id string) (ReportView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reports[id]
	return r, ok
}

// Recheck runs the report again for a server.
func (s *RemoteService) Recheck(id string) (ReportView, error) {
	sv, err := s.find(id)
	if err != nil {
		return ReportView{}, err
	}
	return s.Test(ServerInput{Server: sv, Password: SecretInput{Keep: true}, KeyPassphrase: SecretInput{Keep: true}, SudoPassword: SecretInput{Keep: true}})
}

// SetMutes saves which activity kinds don't notify for a server.
func (s *RemoteService) SetMutes(id string, m settings.ActivityMutes) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(x settings.Server) bool { return x.ID == id })
	if u, ok := s.unsaved[id]; ok && i < 0 {
		u.server.Mutes = m
		s.unsaved[id] = u
		if t, err := s.hub.Tab(id); err == nil {
			t.RT.SetServer(u.server)
		}
		return nil
	}
	if i < 0 {
		return errors.New("that server isn't saved any more")
	}
	list[i].Mutes = m
	if err := settings.SaveServers(s.stateDir, list); err != nil {
		return err
	}
	if t, err := s.hub.Tab(id); err == nil && t.RT.Remote() {
		t.RT.SetServer(list[i]) // its next notification already follows it
	}
	return nil
}

// ChooseKey shows the open-file dialog for a private key (in ~/.ssh) and
// returns its path, or "" when the person cancels.
func (s *RemoteService) ChooseKey() (string, error) {
	a := application.Get()
	if a == nil {
		return "", errors.New("cannot show the file dialog: type the key's path instead")
	}
	d := a.Dialog.OpenFile().
		SetTitle("Choose an SSH key").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		ShowHiddenFiles(true).
		AttachToWindow(a.Window.Current())
	if home, err := os.UserHomeDir(); err == nil {
		d.SetDirectory(filepath.Join(home, ".ssh"))
	}
	path, err := d.PromptForSingleSelection()
	if err != nil {
		return "", nil // cancelled
	}
	if home, herr := os.UserHomeDir(); herr == nil && strings.HasPrefix(path, home+"/") {
		path = "~" + path[len(home):]
	}
	return path, nil
}
