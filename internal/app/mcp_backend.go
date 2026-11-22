package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/touhidurabir/visor/internal/mcp"
)

// mcpBackend is mcp.Backend over the hub's tabs.
type mcpBackend struct{ s *McpService }

var _ mcp.Backend = (*mcpBackend)(nil)

func (b *mcpBackend) Machines() []mcp.Machine { return b.s.machines() }

func (b *mcpBackend) AllowEnv() bool { return b.s.cfg().AllowEnv }

func (b *mcpBackend) Ask(ctx context.Context, q mcp.Question) (bool, error) { return b.s.ask(ctx, q) }

func (b *mcpBackend) Recorded(c mcp.Call) { b.s.o.Emit(EventMcpCall, c) }

// Target is m's tab. A saved server whose tab isn't open is opened, as + →
// server does (D5); one that needs the person (a password not saved, a new
// host key) isn't.
func (b *mcpBackend) Target(_ context.Context, m mcp.Machine) (mcp.Target, error) {
	o := b.s.o
	env := b.s.cfg().AllowEnv
	if m.Local {
		diag := func() (any, error) {
			if o.Doctor == nil {
				return nil, errors.New("Doctor isn't available in this build") //nolint:staticcheck // shown as a sentence
			}
			return o.Doctor.Run(), nil
		}
		return &tabTarget{t: o.Hub.Local(), name: m.Name, diagnose: diag, env: env}, nil
	}
	t, err := o.Hub.Tab(m.ID)
	if err != nil || !slices.Contains(o.Hub.Open(), m.ID) {
		ov, err := o.Remote.Open(m.ID)
		switch {
		case err != nil:
			return nil, fmt.Errorf("%s isn't connected and Visor couldn't open it (%v). Open it in Visor first", m.Name, err)
		case ov.Overview == nil:
			return nil, fmt.Errorf("%s isn't connected and needs you in Visor (a password, a host key, or a connection problem). Open it in Visor first", m.Name)
		}
		if t, err = o.Hub.Tab(m.ID); err != nil {
			return nil, err
		}
		waitFirstPoll(t, 10*time.Second)
	}
	diag := func() (any, error) {
		if rep, ok := o.Remote.Report(m.ID); ok {
			return rep, nil
		}
		return o.Remote.Recheck(m.ID)
	}
	return &tabTarget{t: t, name: m.Name, diagnose: diag, env: env}, nil
}

// waitFirstPoll waits until a tab just opened has heard from supervisord
// (or failed to), so the agent's first call sees real data.
func waitFirstPoll(t *Tab, limit time.Duration) {
	for end := time.Now().Add(limit); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if sv := t.RT.Overview().Server; sv.Reachable || sv.Error != "" {
			return
		}
	}
}
