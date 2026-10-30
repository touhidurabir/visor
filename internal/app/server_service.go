// Package app is the only package that touches Wails: thin services that
// the frontend calls, built on the core packages.
package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/supervisor"
)

// ServerService exposes the supervisord connection to the frontend.
type ServerService struct {
	override string

	mu     sync.Mutex
	client *supervisor.Client
}

// NewServerService returns a service for the config at override, or the
// auto-discovered one when override is empty.
func NewServerService(override string) *ServerService {
	return &ServerService{override: override}
}

// connect builds the client on first use. A failure is not cached, so fixing
// the config and retrying works without a restart.
func (s *ServerService) connect() (*supervisor.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	d, err := config.Resolve(s.override)
	if err != nil {
		return nil, err
	}
	if d.Transport == nil {
		return nil, fmt.Errorf("config defines no socket or port: add [unix_http_server] to %s", d.ConfigPath)
	}
	var opts []supervisor.Option
	if d.Username != "" {
		opts = append(opts, supervisor.WithBasicAuth(d.Username, d.Password))
	}
	s.client = supervisor.New(d.Transport, opts...)
	return s.client, nil
}

// ListProcesses returns every process supervisord knows about.
func (s *ServerService) ListProcesses() ([]ProcessView, error) {
	c, err := s.connect()
	if err != nil {
		return nil, err
	}
	infos, err := c.GetAllProcessInfo(context.Background())
	if err != nil {
		if errors.Is(err, supervisor.ErrUnreachable) {
			return nil, fmt.Errorf("cannot reach supervisord: %w", err)
		}
		return nil, err
	}
	out := make([]ProcessView, 0, len(infos))
	for _, p := range infos {
		out = append(out, toProcessView(p))
	}
	return out, nil
}
