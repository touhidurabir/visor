package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/zalando/go-keyring"

	"github.com/touhidurabir/visor/internal/mcp"
	"github.com/touhidurabir/visor/internal/notify"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/settings"
)

// MCP events for the frontend.
const (
	// EventMcp carries an McpView whenever the page's state changes.
	EventMcp = "visor:mcp"
	// EventMcpCall carries one mcp.Call as it happens.
	EventMcpCall = "visor:mcp-call"
	// EventMcpAsk carries an mcp.Question waiting for the person.
	EventMcpAsk = "visor:mcp-ask"
	// EventMcpAskDone carries the id of a question answered or timed out.
	EventMcpAskDone = "visor:mcp-ask-done"
	// RouteMcp is the MCP server page.
	RouteMcp = "#/mcp"
)

func init() {
	application.RegisterEvent[McpView](EventMcp)
	application.RegisterEvent[mcp.Call](EventMcpCall)
	application.RegisterEvent[mcp.Question](EventMcpAsk)
	application.RegisterEvent[string](EventMcpAskDone)
}

// MCP server status, as the page's badge shows it.
const (
	McpOff       = "off"
	McpRunning   = "running"
	McpPortInUse = "port_in_use"
	McpFailed    = "failed"
)

// mcpTokenAccount is the Keychain account of the MCP token.
const mcpTokenAccount = "mcp/token"

// askFor is how long a question waits for the person before it's a no.
const askFor = 60 * time.Second

// McpOptions wire an McpService.
type McpOptions struct {
	Hub      *Hub
	Remote   *RemoteService
	Settings *SettingsService
	Doctor   *DoctorService // nil: diagnose isn't offered on this computer
	Keychain settings.Keychain
	Emit     func(string, any)
	Notifier notify.Sender
	// Show brings Visor's window forward, for a question.
	Show    func()
	Version string
	// Port, when set, replaces the saved port (VISOR_MCP_PORT, for tests).
	Port int
}

// McpService runs Visor's MCP server for AI agents and backs the MCP
// server page: the setting, the token, access per machine, recent calls,
// and the questions Visor asks before a change.
type McpService struct {
	o          McpOptions
	askTimeout time.Duration

	mu       sync.Mutex
	srv      *mcp.Server
	http     *http.Server
	status   string
	errText  string
	token    string
	tokenErr error // the Keychain or keyring couldn't be read
	asks     []*pendingAsk
	allowed  map[string]time.Time // client|machine|verb → until
	closed   bool
	// openMu lets one call open a saved server's tab while others wait.
	openMu sync.Mutex
}

type pendingAsk struct {
	q      mcp.Question
	answer chan bool
}

// McpMachineView is one row of the Access table.
type McpMachineView struct {
	ID, Name, Tag string
	Local         bool
	ReadOnly      bool
	Connected     bool
	// Level and Ask are as stored; MaxLevel is the highest allowed (read on
	// a Read only server); AskLocked: production always asks.
	Level     string
	Ask       bool
	MaxLevel  string
	AskLocked bool
}

// McpView is the MCP server page.
type McpView struct {
	Status    string // McpOff, McpRunning, McpPortInUse, McpFailed
	Error     string
	Enabled   bool
	Port      int
	URL       string
	Token     string
	AllowEnv  bool
	HasClaude bool
	Machines  []McpMachineView
	Tools     []mcp.ToolInfo
	Calls     []mcp.Call
	Asks      []mcp.Question
}

// NewMcpService returns the service; ServiceStartup starts the server when
// it is enabled.
func NewMcpService(o McpOptions) *McpService {
	if o.Emit == nil {
		o.Emit = func(string, any) {}
	}
	s := &McpService{o: o, askTimeout: askFor, status: McpOff, allowed: map[string]time.Time{}}
	s.srv = mcp.NewServer(&mcpBackend{s: s}, o.Version)
	return s
}

// localName is this computer's name in MCP: "This Mac" or "This computer".
func localName() string { return platform.Describe(platform.Current()).Local }

func (s *McpService) cfg() settings.MCPSettings { return s.o.Settings.rt.Settings().MCP }

func (s *McpService) port() int {
	if s.o.Port > 0 {
		return s.o.Port
	}
	return s.cfg().Port
}

// ServiceStartup starts the server when it is enabled. The token is read
// only then (or when the page opens), so people who don't use MCP never
// get a keyring prompt for it.
func (s *McpService) ServiceStartup(context.Context, application.ServiceOptions) error {
	if s.cfg().Enabled {
		if _, err := s.ensureToken(); err != nil {
			log.Printf("visor: MCP token: %v", err)
		}
		s.restart()
	}
	return nil
}

// ensureToken loads the token, or makes one when none is saved. A token
// that can't be read is never replaced: clients using it keep working once
// the keyring answers again.
func (s *McpService) ensureToken() (string, error) {
	s.mu.Lock()
	tok := s.token
	s.mu.Unlock()
	if tok != "" {
		return tok, nil
	}
	tok, err := s.o.Keychain.Get(settings.KeychainService, mcpTokenAccount)
	switch {
	case err == nil && tok != "":
	case err == nil || errors.Is(err, keyring.ErrNotFound):
		if tok, err = s.newToken(); err != nil {
			return "", err
		}
	default:
		s.mu.Lock()
		s.tokenErr = err
		s.mu.Unlock()
		return "", fmt.Errorf("Visor can't read the MCP token from the %s: %w", platform.Describe(platform.Current()).SecretStore, err) //nolint:staticcheck // shown as a sentence
	}
	s.mu.Lock()
	s.token, s.tokenErr = tok, nil
	s.mu.Unlock()
	return tok, nil
}

// Status is the server's status alone (the sidebar's dot), without reading
// the token.
func (s *McpService) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// ServiceShutdown stops the server and says no to every waiting question.
func (s *McpService) ServiceShutdown() error {
	s.mu.Lock()
	s.closed = true
	asks := s.asks
	s.asks = nil
	s.mu.Unlock()
	for _, a := range asks {
		a.answer <- false
	}
	s.stop()
	return nil
}

func (s *McpService) loadToken() string {
	tok, err := s.o.Keychain.Get(settings.KeychainService, mcpTokenAccount)
	if err != nil {
		return ""
	}
	return tok
}

func (s *McpService) newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := "vsr_" + base64.RawURLEncoding.EncodeToString(b)
	if err := s.o.Keychain.Set(settings.KeychainService, mcpTokenAccount, tok); err != nil {
		// Kept for this session: clients work until Visor restarts.
		log.Printf("visor: saving the MCP token: %v", err)
	}
	return tok, nil
}

func (s *McpService) currentToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// restart (re)starts the listener on the current port.
func (s *McpService) restart() {
	s.stop()
	port := s.port()
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.status, s.errText = McpFailed, err.Error()
		if errors.Is(err, syscall.EADDRINUSE) {
			s.status = McpPortInUse
			s.errText = fmt.Sprintf("Another app is using port %d. Choose another port.", port)
		}
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", s.srv.Handler(s.currentTokenUnlocked))
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.http, s.status, s.errText = hs, McpRunning, ""
	go func() {
		if err := hs.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("visor: MCP server: %v", err)
		}
	}()
}

// currentTokenUnlocked is the token for the handler (it takes the lock itself).
func (s *McpService) currentTokenUnlocked() string { return s.currentToken() }

func (s *McpService) stop() {
	s.mu.Lock()
	hs := s.http
	s.http, s.status, s.errText = nil, McpOff, ""
	s.mu.Unlock()
	if hs != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := hs.Shutdown(ctx); err != nil {
			_ = hs.Close()
		}
	}
}

func (s *McpService) changed() McpView {
	v := s.Get()
	s.o.Emit(EventMcp, v)
	return v
}

// Get is the MCP server page.
func (s *McpService) Get() McpView {
	_, tokErr := s.ensureToken()
	c := s.cfg()
	s.mu.Lock()
	v := McpView{
		Status: s.status, Error: s.errText, Enabled: c.Enabled, Port: s.port(), Token: s.token,
		AllowEnv: c.AllowEnv, Tools: mcp.Tools, Calls: s.srv.Calls(),
	}
	for _, a := range s.asks {
		v.Asks = append(v.Asks, a.q)
	}
	s.mu.Unlock()
	if tokErr != nil && v.Error == "" {
		v.Error = tokErr.Error()
	}
	v.URL = fmt.Sprintf("http://127.0.0.1:%d/mcp", v.Port)
	v.HasClaude = findClaude() != ""
	for _, m := range s.machines() {
		mv := McpMachineView{
			ID: m.ID, Name: m.Name, Tag: m.Tag, Local: m.Local, ReadOnly: m.ReadOnly, Connected: m.Connected,
			Level: m.Level.String(), Ask: m.Ask, MaxLevel: mcp.Configs.String(), AskLocked: m.Production(),
		}
		if m.ReadOnly {
			mv.MaxLevel = mcp.Read.String()
		}
		if m.Production() {
			mv.Ask = true
		}
		v.Machines = append(v.Machines, mv)
	}
	return v
}

// SetEnabled turns the MCP server on or off.
func (s *McpService) SetEnabled(on bool) (McpView, error) {
	if on {
		if _, err := s.ensureToken(); err != nil {
			return s.Get(), err
		}
	}
	if _, err := s.o.Settings.updateMCP(func(m *settings.MCPSettings) { m.Enabled = on }); err != nil {
		return s.Get(), err
	}
	if on {
		s.restart()
	} else {
		s.stop()
	}
	return s.changed(), nil
}

// SetPort moves the server to port (1024–65535), restarting it when on.
func (s *McpService) SetPort(port int) (McpView, error) {
	if port < 1024 || port > 65535 {
		return s.Get(), fmt.Errorf("choose a port from 1024 to 65535")
	}
	if _, err := s.o.Settings.updateMCP(func(m *settings.MCPSettings) { m.Port = port }); err != nil {
		return s.Get(), err
	}
	s.o.Port = 0 // a chosen port wins over the test override
	if s.cfg().Enabled {
		s.restart()
	}
	return s.changed(), nil
}

// RegenerateToken makes a new token; clients with the old one are refused
// from their next request.
func (s *McpService) RegenerateToken() (McpView, error) {
	tok, err := s.newToken()
	if err != nil {
		return s.Get(), err
	}
	s.mu.Lock()
	s.token = tok
	s.mu.Unlock()
	return s.changed(), nil
}

// SetAllowEnv is "Let AI read environment values".
func (s *McpService) SetAllowEnv(on bool) (McpView, error) {
	if _, err := s.o.Settings.updateMCP(func(m *settings.MCPSettings) { m.AllowEnv = on }); err != nil {
		return s.Get(), err
	}
	return s.changed(), nil
}

// SetAccess sets a machine's level ("off", "read", "control", "configs")
// and asking. A production server always asks; a Read only server stays
// at Read at most.
func (s *McpService) SetAccess(machineID, level string, ask bool) (McpView, error) {
	l, err := mcp.ParseLevel(level)
	if err != nil {
		return s.Get(), err
	}
	i := slices.IndexFunc(s.machines(), func(m mcp.Machine) bool { return m.ID == machineID })
	if i < 0 {
		return s.Get(), fmt.Errorf("no machine %q: it may have been deleted", machineID)
	}
	m := s.machines()[i]
	if m.ReadOnly && l > mcp.Read {
		l = mcp.Read
	}
	if m.Production() {
		ask = true
	}
	if _, err := s.o.Settings.updateMCP(func(c *settings.MCPSettings) {
		if c.Access == nil {
			c.Access = map[string]settings.MCPAccess{}
		}
		c.Access[machineID] = settings.MCPAccess{Level: l.String(), Ask: ask}
	}); err != nil {
		return s.Get(), err
	}
	return s.changed(), nil
}

// machines is this computer, then every saved server, with their levels.
func (s *McpService) machines() []mcp.Machine {
	c := s.cfg()
	level := func(id string, local bool) (mcp.Level, bool) {
		def, ask := mcp.Default(local)
		a, ok := c.Access[id]
		if !ok {
			return def, ask
		}
		l, err := mcp.ParseLevel(a.Level)
		if err != nil {
			return def, ask
		}
		return l, a.Ask
	}
	l, ask := level("local", true)
	out := []mcp.Machine{{ID: "local", Name: localName(), Local: true, Connected: true, Level: l, Ask: ask}}
	if s.o.Remote == nil {
		return out
	}
	list, err := settings.LoadServers(s.o.Remote.stateDir)
	if err != nil {
		log.Printf("visor: MCP: reading the servers: %v", err)
	}
	open := s.o.Hub.Open()
	for _, sv := range list {
		l, ask := level(sv.ID, false)
		m := mcp.Machine{ID: sv.ID, Name: sv.Name, Tag: sv.Tag, ReadOnly: sv.ReadOnly, Level: l, Ask: ask}
		if slices.Contains(open, sv.ID) {
			if t, err := s.o.Hub.Tab(sv.ID); err == nil {
				m.Connected = t.RT.Overview().Host.Conn == ConnConnected
			}
		}
		out = append(out, m)
	}
	return out
}

// allowKey is what "Allow for 10 minutes" covers: that client, machine and
// verb. Production and forced questions (KILL) always ask.
func allowKey(q mcp.Question) string {
	if q.Production || q.Force {
		return ""
	}
	return q.Client + "|" + q.MachineID + "|" + q.Verb
}

// ask shows q in Visor (the window comes forward, with a notification
// pointing to it) and waits up to a minute for Answer. No answer, closing
// Visor or the agent going away is a no.
func (s *McpService) ask(ctx context.Context, q mcp.Question) (bool, error) {
	key := allowKey(q)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false, nil
	}
	if key != "" && time.Now().Before(s.allowed[key]) {
		s.mu.Unlock()
		return true, nil
	}
	a := &pendingAsk{q: q, answer: make(chan bool, 1)}
	s.asks = append(s.asks, a)
	timeout := s.askTimeout
	s.mu.Unlock()

	s.o.Emit(EventMcpAsk, q)
	if s.o.Show != nil {
		s.o.Show()
	}
	if s.o.Notifier != nil {
		title := fmt.Sprintf("%s wants to %s %s on %s", q.Client, q.Verb, q.Subject, q.Machine)
		if err := s.o.Notifier.Send(notify.Message{
			ID: "mcp-" + q.ID, Title: title, Body: "Open Visor to allow or deny it.",
			Thread: "visor-mcp", Route: RouteMcp,
		}); err != nil {
			log.Printf("visor: MCP question notification: %v", err)
		}
	}

	var ok bool
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ok = <-a.answer:
	case <-timer.C:
	case <-ctx.Done():
	}
	s.mu.Lock()
	s.asks = slices.DeleteFunc(s.asks, func(x *pendingAsk) bool { return x == a })
	s.mu.Unlock()
	s.o.Emit(EventMcpAskDone, q.ID)
	return ok, nil
}

// Answer answers a waiting question. minutes 10 also allows that client's
// next changes on that machine for 10 minutes.
func (s *McpService) Answer(askID string, allow bool, minutes int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.asks, func(a *pendingAsk) bool { return a.q.ID == askID })
	if i < 0 {
		return errors.New("that question isn't waiting any more")
	}
	a := s.asks[i]
	s.asks = slices.Delete(s.asks, i, i+1)
	if key := allowKey(a.q); allow && minutes > 0 && key != "" {
		s.allowed[key] = time.Now().Add(time.Duration(min(minutes, 10)) * time.Minute)
	}
	a.answer <- allow
	return nil
}

// claudeDirs are where Claude Code's installers put `claude`; an app
// started from the Dock has a short PATH that misses most of them.
func claudeDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := filepath.SplitList(os.Getenv("PATH"))
	return append(dirs, filepath.Join(home, ".claude", "local"), filepath.Join(home, ".local", "bin"),
		"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".npm-global", "bin"))
}

func findClaude() string {
	for _, d := range claudeDirs() {
		p := filepath.Join(d, "claude")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// ClaudeCommand is the command "Add to Claude Code" runs, as the page shows it.
func (s *McpService) ClaudeCommand() string {
	v := s.Get()
	return fmt.Sprintf(`claude mcp add --scope user --transport http visor %s --header "Authorization: Bearer %s"`, v.URL, v.Token)
}

// AddToClaudeCode adds Visor to Claude Code for this user, replacing an
// older "visor" entry (after a new token or port), and returns its output.
func (s *McpService) AddToClaudeCode() (string, error) {
	claude := findClaude()
	if claude == "" {
		return "", errors.New("Visor didn't find Claude Code's claude command: copy the command instead") //nolint:staticcheck // shown as a sentence
	}
	v := s.Get()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, claude, "mcp", "remove", "--scope", "user", "visor").Run() // shortcut: fails harmlessly when absent
	out, err := exec.CommandContext(ctx, claude, "mcp", "add", "--scope", "user", "--transport", "http", "visor", v.URL,
		"--header", "Authorization: Bearer "+v.Token).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("claude mcp add failed: %v: %s", err, text)
	}
	return text, nil
}
