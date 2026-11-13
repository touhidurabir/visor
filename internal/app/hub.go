package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/touhidurabir/visor/internal/notify"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/touhidurabir/visor/internal/settings"
)

var (
	// ErrNoTab means the tab is not open (closed, or never opened).
	ErrNoTab = errors.New("that server's tab isn't open")
	// ErrTooManyServers means the limit of servers connected at once is reached.
	ErrTooManyServers = errors.New("too many servers are open")
)

// TooManyServersError says how many may be open (Settings → Servers).
type TooManyServersError struct{ Max int }

func (e *TooManyServersError) Error() string {
	return fmt.Sprintf("at most %d servers can be open at once: close one, or raise the limit in Settings → Servers", e.Max)
}

func (e *TooManyServersError) Is(target error) bool { return target == ErrTooManyServers }

// Tab is one open tab: a Runtime and the services bound to it.
type Tab struct {
	RT     *Runtime
	Server *ServerService
	Config *ConfigService
	Issue  *IssueService
	Log    *LogService
	Inbox  *InboxService
}

func newTab(rt *Runtime, emit func(string, any), logPrefix string) *Tab {
	l := NewLogService(rt, emit)
	l.prefix = logPrefix
	return &Tab{
		RT: rt, Server: NewServerService(rt), Config: NewConfigService(rt),
		Issue: NewIssueService(rt), Log: l, Inbox: NewInboxService(rt),
	}
}

// HubOptions configure a Hub.
type HubOptions struct {
	StateDir string
	Keychain settings.Keychain
	// Emit sends runtime events (wrapped by the Shell); LogEmit sends log
	// chunks (frontend only).
	Emit, LogEmit func(string, any)
	Notifier      notify.Sender
	// Settings returns the settings in use (for the limit and poll interval).
	Settings func() settings.Settings
	// HostKeys are where Visor's own known_hosts lives (DefaultHostKeys).
	HostKeys remote.HostKeys
	// Connect builds a server's connector; ConnectServer when nil (tests fake it).
	Connect func(settings.Server, settings.Keychain, remote.HostKeys) func(context.Context, string) (*RemoteSession, error)
	// Closed is told when a server tab closes (the Shell forgets its health).
	Closed func(id string)
}

// Hub owns the tabs: this computer's Runtime, always open, and one Runtime
// per open server, in the order they were opened.
type Hub struct {
	o     HubOptions
	local *Tab

	mu    sync.Mutex
	ctx   context.Context
	tabs  map[string]*Tab
	order []string
	// opening holds ids being connected, so a double click opens one tab.
	opening map[string]bool
}

// NewHub returns a Hub around this computer's Runtime.
func NewHub(local *Runtime, o HubOptions) *Hub {
	if o.Emit == nil {
		o.Emit = func(string, any) {}
	}
	if o.LogEmit == nil {
		o.LogEmit = func(string, any) {}
	}
	if o.Connect == nil {
		o.Connect = ConnectServer
	}
	if o.Settings == nil {
		o.Settings = local.Settings
	}
	return &Hub{
		o: o, local: newTab(local, o.LogEmit, ""), ctx: context.Background(),
		tabs: map[string]*Tab{}, opening: map[string]bool{},
	}
}

// HubService binds the Hub's life to the app's (and exposes nothing else).
type HubService struct{ hub *Hub }

// NewHubService returns the Wails service that starts and stops hub.
func NewHubService(hub *Hub) *HubService { return &HubService{hub: hub} }

// ServiceStartup starts this computer's Runtime with the app.
func (s *HubService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return s.hub.Start(ctx)
}

// ServiceShutdown stops every tab.
func (s *HubService) ServiceShutdown() error { return s.hub.Shutdown() }

// Start starts this computer's Runtime (ServiceStartup without Wails).
func (h *Hub) Start(ctx context.Context) error {
	h.mu.Lock()
	h.ctx = ctx
	h.mu.Unlock()
	return h.local.RT.Start(ctx)
}

// Shutdown stops every tab.
func (h *Hub) Shutdown() error {
	h.mu.Lock()
	tabs := make([]*Tab, 0, len(h.tabs))
	for _, t := range h.tabs {
		tabs = append(tabs, t)
	}
	h.tabs, h.order = map[string]*Tab{}, nil
	h.mu.Unlock()
	for _, t := range append(tabs, h.local) {
		_ = t.Log.ServiceShutdown()
		t.RT.Stop()
	}
	return nil
}

// Local is this computer's tab.
func (h *Hub) Local() *Tab { return h.local }

// Tab returns an open tab by id ("" and LocalID are this computer).
func (h *Hub) Tab(id string) (*Tab, error) {
	if id == "" || id == LocalID {
		return h.local, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.tabs[id]; ok {
		return t, nil
	}
	return nil, ErrNoTab
}

// Open returns the ids of the open server tabs, in the order opened.
func (h *Hub) Open() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.order...)
}

// OpenServer connects to s in a new tab (or returns the open one). The
// first connection attempt runs before it returns: its overview says
// whether supervisord answered, or why not.
func (h *Hub) OpenServer(s settings.Server) (OverviewView, error) {
	if !settings.ValidServerID(s.ID) {
		return OverviewView{}, fmt.Errorf("bad server id %q", s.ID)
	}
	h.mu.Lock()
	if t, ok := h.tabs[s.ID]; ok {
		h.mu.Unlock()
		return t.RT.Overview(), nil
	}
	if h.opening[s.ID] {
		h.mu.Unlock()
		return OverviewView{}, errors.New("already connecting to this server")
	}
	if most := h.o.Settings().Clamp().MaxServers; len(h.tabs)+len(h.opening) >= most {
		h.mu.Unlock()
		return OverviewView{}, &TooManyServersError{Max: most}
	}
	h.opening[s.ID] = true
	ctx := h.ctx
	h.mu.Unlock()

	prefs := h.o.Settings()
	rt := NewRuntime(RuntimeOptions{
		StateDir: settings.ServerStateDir(h.o.StateDir, s.ID),
		Settings: prefs,
		Keychain: h.o.Keychain,
		Emit:     h.o.Emit,
		Notifier: h.o.Notifier,
		Remote:   &RemoteSource{Server: s, Connect: h.o.Connect(s, h.o.Keychain, h.o.HostKeys)},
	})
	err := rt.Start(ctx)

	h.mu.Lock()
	delete(h.opening, s.ID)
	if err != nil {
		h.mu.Unlock()
		rt.Stop()
		return OverviewView{}, err
	}
	t := newTab(rt, h.o.LogEmit, s.ID+"-")
	h.tabs[s.ID] = t
	h.order = append(h.order, s.ID)
	h.mu.Unlock()
	ov := rt.Overview()
	rt.emitOverview()
	return ov, nil
}

// CloseServer disconnects a server tab. This computer's tab can't be closed.
func (h *Hub) CloseServer(id string) error {
	if id == "" || id == LocalID {
		return errors.New("this computer's tab can't be closed")
	}
	h.mu.Lock()
	t, ok := h.tabs[id]
	if ok {
		delete(h.tabs, id)
		for i, o := range h.order {
			if o == id {
				h.order = append(h.order[:i], h.order[i+1:]...)
				break
			}
		}
	}
	h.mu.Unlock()
	if !ok {
		return nil
	}
	_ = t.Log.ServiceShutdown()
	t.RT.Stop()
	if h.o.Closed != nil {
		h.o.Closed(id)
	}
	return nil
}

// Each calls fn for this computer's tab and every open server tab.
func (h *Hub) Each(fn func(id string, t *Tab)) {
	h.mu.Lock()
	ids := append([]string(nil), h.order...)
	tabs := make([]*Tab, len(ids))
	for i, id := range ids {
		tabs[i] = h.tabs[id]
	}
	h.mu.Unlock()
	fn(LocalID, h.local)
	for i, id := range ids {
		fn(id, tabs[i])
	}
}

// ApplySettings puts global settings (poll interval, crash rules) into use
// on every server tab; this computer's Runtime gets them from SettingsService.
func (h *Hub) ApplySettings(s settings.Settings) {
	h.mu.Lock()
	tabs := make([]*Tab, 0, len(h.tabs))
	for _, t := range h.tabs {
		tabs = append(tabs, t)
	}
	h.mu.Unlock()
	for _, t := range tabs {
		_ = t.RT.ApplySettings(s, false)
	}
}
