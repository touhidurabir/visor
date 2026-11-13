package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/touhidurabir/visor/internal/doctor"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/settings"
)

// RemoteService manages saved servers and their tabs: the form's Test, Save
// and Connect, Settings → Servers, and the tab bar's open and close.
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
}

// NewRemoteService returns the service over hub.
func NewRemoteService(hub *Hub, stateDir string, k settings.Keychain, hk remote.HostKeys, prefs func() settings.Settings) *RemoteService {
	return &RemoteService{
		hub: hub, stateDir: stateDir, keychain: k, hk: hk, prefs: prefs, probe: probeServer,
		pending: map[string]pendingKey{}, reports: map[string]ReportView{},
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

// List returns the saved servers, by name.
func (s *RemoteService) List() ([]SavedServerView, error) {
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return nil, err
	}
	open := s.hub.Open()
	out := make([]SavedServerView, 0, len(list))
	for _, sv := range list {
		v := SavedServerView{Server: sv, Open: slices.Contains(open, sv.ID)}
		v.HasPassword = s.has(sv.ID, settings.SecretPassword)
		v.HasKeyPassphrase = s.has(sv.ID, settings.SecretKeyPassphrase)
		v.HasSudoPassword = s.has(sv.ID, settings.SecretSudo)
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

func (s *RemoteService) has(id, kind string) bool {
	v, err := settings.ServerSecret(s.keychain, id, kind)
	return err == nil && v != ""
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
	for _, id := range s.hub.Open() {
		if i := slices.IndexFunc(list, func(x settings.Server) bool { return x.ID == id }); i >= 0 {
			out = append(out, list[i])
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
	for kind, si := range map[string]SecretInput{
		settings.SecretPassword: in.Password, settings.SecretKeyPassphrase: in.KeyPassphrase, settings.SecretSudo: in.SudoPassword,
	} {
		if si.Keep {
			continue
		}
		if err := settings.SetServerSecret(s.keychain, sv.ID, kind, si.Value); err != nil && !errors.Is(err, settings.ErrNoKeyring) {
			return settings.Server{}, err
		}
	}
	if i := slices.IndexFunc(list, func(x settings.Server) bool { return x.ID == sv.ID }); i >= 0 {
		sv.Mutes = list[i].Mutes // kept by SetMutes, not the form
		list[i] = sv
	} else {
		list = append(list, sv)
	}
	if err := settings.SaveServers(s.stateDir, list); err != nil {
		return settings.Server{}, err
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
		if sec, sudo, err = ServerSecrets(s.keychain, saved); err != nil {
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
	s.pending[rep.HostKey.Fingerprint] = pendingKey{host: host, port: p, key: key}
	s.mu.Unlock()
}

// Trust records the host key with this fingerprint (one Test or Open just
// showed) in Visor's own known_hosts. ~/.ssh/known_hosts is never changed.
func (s *RemoteService) Trust(fingerprint string) error {
	s.mu.Lock()
	pk, ok := s.pending[fingerprint]
	delete(s.pending, fingerprint)
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
// connection that fails, comes back as a report instead of a tab.
func (s *RemoteService) Open(id string) (OpenView, error) {
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return OpenView{}, err
	}
	i := slices.IndexFunc(list, func(x settings.Server) bool { return x.ID == id })
	if i < 0 {
		return OpenView{}, errors.New("that server isn't saved any more")
	}
	sv := list[i]
	if slices.Contains(s.hub.Open(), id) {
		t, _ := s.hub.Tab(id)
		ov := t.RT.Overview()
		return OpenView{Overview: &ov}, nil
	}
	if l := s.Limits(); l.Open >= l.MaxServers {
		return OpenView{}, &TooManyServersError{Max: l.MaxServers}
	}
	ov, err := s.hub.OpenServer(sv)
	if err != nil {
		return OpenView{}, err
	}
	t, terr := s.hub.Tab(id)
	if terr != nil || t.RT.ConnErr() == nil {
		return OpenView{Overview: &ov}, nil
	}
	// It didn't connect: find out why, step by step. Only then is the
	// connection made a second time (sudo is asked once per connection).
	cerr := t.RT.ConnErr()
	if hkv, key := hostKeyView(cerr); hkv != nil {
		_ = s.hub.CloseServer(id)
		rep := ReportView{HostKey: hkv, Blocked: true, AtMs: time.Now().UnixMilli()}
		if hkv.Status == "changed" {
			rep.Rows = doctor.RemoteReport(doctor.RemoteFacts{Server: sv.Name, HostKeyChanged: true, KnownHostsFix: hkv.Fix})
		}
		s.remember(rep, key)
		return OpenView{HostKey: hkv, Report: &rep}, nil
	}
	sec, sudo, err := ServerSecrets(s.keychain, sv)
	if err != nil {
		return OpenView{Overview: &ov}, nil
	}
	rep, key := s.probe(context.Background(), sv, sec, sudo, s.hk)
	s.remember(rep, key)
	s.mu.Lock()
	s.reports[id] = rep
	s.mu.Unlock()
	return OpenView{Overview: &ov, Report: &rep}, nil
}

// Close closes a server's tab (the server stays saved).
func (s *RemoteService) Close(id string) error { return s.hub.CloseServer(id) }

// Report returns the latest capability report for a server (from Test,
// Open or Recheck); ok is false when there is none yet.
func (s *RemoteService) Report(id string) (ReportView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reports[id]
	return r, ok
}

// Recheck runs the report again for a saved server.
func (s *RemoteService) Recheck(id string) (ReportView, error) {
	list, err := settings.LoadServers(s.stateDir)
	if err != nil {
		return ReportView{}, err
	}
	i := slices.IndexFunc(list, func(x settings.Server) bool { return x.ID == id })
	if i < 0 {
		return ReportView{}, errors.New("that server isn't saved any more")
	}
	return s.Test(ServerInput{Server: list[i], Password: SecretInput{Keep: true}, KeyPassphrase: SecretInput{Keep: true}, SudoPassword: SecretInput{Keep: true}})
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
	if i < 0 {
		return errors.New("that server isn't saved any more")
	}
	list[i].Mutes = m
	return settings.SaveServers(s.stateDir, list)
}
