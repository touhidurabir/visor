package remote

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SSHConfig is what `ssh -G <host>` says about a host: the user's
// ~/.ssh/config applied, read only.
type SSHConfig struct {
	HostName      string
	Port          int
	User          string
	IdentityFiles []string
	IdentityAgent string // "" (use $SSH_AUTH_SOCK), "none", or a socket path
	ProxyJump     string // "user@host:port,host2" or ""
	ProxyCommand  string // not supported; reported when set
}

// ErrProxyCommand means the host uses ProxyCommand, which Visor can't run.
var ErrProxyCommand = errors.New("this host uses ProxyCommand in ~/.ssh/config, which Visor doesn't support; use ProxyJump")

// ParseSSHG reads `ssh -G` output.
func ParseSSHG(out []byte) SSHConfig {
	var c SSHConfig
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, val, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToLower(key) {
		case "hostname":
			c.HostName = val
		case "port":
			c.Port, _ = strconv.Atoi(val)
		case "user":
			c.User = val
		case "identityfile":
			c.IdentityFiles = append(c.IdentityFiles, val)
		case "identityagent":
			if !strings.EqualFold(val, "SSH_AUTH_SOCK") {
				c.IdentityAgent = val
			}
		case "proxyjump":
			if !strings.EqualFold(val, "none") {
				c.ProxyJump = val
			}
		case "proxycommand":
			if !strings.EqualFold(val, "none") {
				c.ProxyCommand = val
			}
		}
	}
	return c
}

// sshG runs `ssh -G host`; tests replace it.
var sshG = func(ctx context.Context, host string) ([]byte, error) {
	bin, err := exec.LookPath("ssh")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, bin, "-G", "--", host).Output()
}

// LookupSSHConfig asks ssh how it would connect to host. found is false
// when ssh isn't installed (then the fields Visor was given are used as-is).
func LookupSSHConfig(ctx context.Context, host string) (c SSHConfig, found bool) {
	out, err := sshG(ctx, host)
	if err != nil {
		return SSHConfig{}, false
	}
	return ParseSSHG(out), true
}

// Resolve fills what t leaves empty from ~/.ssh/config: the real host name,
// port, user, the first key file that exists, the agent and ProxyJump hosts.
// Fields set on t win.
func Resolve(ctx context.Context, t Target) (Target, error) {
	return resolve(ctx, t, 0)
}

func resolve(ctx context.Context, t Target, depth int) (Target, error) {
	if depth > 4 {
		return t, errors.New("ProxyJump chain is too long")
	}
	c, found := LookupSSHConfig(ctx, t.Host)
	if !found {
		if t.AgentSocket == "" {
			t.AgentSocket = DefaultAgentSocket()
		}
		return t, nil
	}
	if c.ProxyCommand != "" {
		return t, ErrProxyCommand
	}
	if c.HostName != "" {
		t.Host = c.HostName
	}
	if t.Port == 0 {
		t.Port = c.Port
	}
	if t.User == "" {
		t.User = c.User
	}
	if t.KeyPath == "" {
		for _, f := range c.IdentityFiles {
			if _, err := os.Stat(expandHome(f)); err == nil {
				t.KeyPath = f
				break
			}
		}
	}
	if t.AgentSocket == "" {
		switch {
		case strings.EqualFold(c.IdentityAgent, "none"):
		case c.IdentityAgent != "":
			t.AgentSocket = expandHome(c.IdentityAgent)
		default:
			t.AgentSocket = DefaultAgentSocket()
		}
	}
	if len(t.Jump) == 0 && c.ProxyJump != "" {
		for _, hop := range strings.Split(c.ProxyJump, ",") {
			j, err := parseHop(strings.TrimSpace(hop))
			if err != nil {
				return t, err
			}
			j, err = resolve(ctx, j, depth+1)
			if err != nil {
				return t, fmt.Errorf("jump host %s: %w", hop, err)
			}
			t.Jump = append(t.Jump, j)
		}
	}
	return t, nil
}

// ParseJump reads a ProxyJump list ("user@host:port,host2").
func ParseJump(s string) ([]Target, error) {
	var out []Target
	for _, hop := range strings.Split(s, ",") {
		t, err := parseHop(strings.TrimSpace(hop))
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// parseHop reads one ProxyJump entry: [user@]host[:port] or ssh://[user@]host[:port].
func parseHop(s string) (Target, error) {
	s = strings.TrimPrefix(s, "ssh://")
	var t Target
	if u, rest, ok := strings.Cut(s, "@"); ok {
		t.User, s = u, rest
	}
	if strings.HasPrefix(s, "[") { // [ipv6]:port
		end := strings.Index(s, "]")
		if end < 0 {
			return t, fmt.Errorf("bad ProxyJump host %q", s)
		}
		t.Host = s[1:end]
		s = s[end+1:]
		if p, ok := strings.CutPrefix(s, ":"); ok {
			t.Port, _ = strconv.Atoi(p)
		}
		return t, nil
	}
	if h, p, ok := strings.Cut(s, ":"); ok {
		t.Host = h
		port, err := strconv.Atoi(p)
		if err != nil {
			return t, fmt.Errorf("bad ProxyJump port %q", p)
		}
		t.Port = port
	} else {
		t.Host = s
	}
	if t.Host == "" {
		return t, errors.New("empty ProxyJump host")
	}
	return t, nil
}
