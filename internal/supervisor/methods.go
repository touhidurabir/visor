package supervisor

import (
	"context"
	"errors"
	"fmt"
)

// control runs a start/stop style call; with wait it blocks until the
// process changes state, so only ctx bounds it.
func (c *Client) control(ctx context.Context, wait bool, method string, params ...any) (any, error) {
	if wait {
		return c.callWaiting(ctx, method, params...)
	}
	return c.call(ctx, method, params...)
}

// ok checks a method that returns true on success.
func ok(method string, v any, err error) error {
	if err != nil {
		return err
	}
	if b, isBool := v.(bool); !isBool || !b {
		return fmt.Errorf("%s: expected true, got %#v", method, v)
	}
	return nil
}

func str(method string, v any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	s, isStr := v.(string)
	if !isStr {
		return "", fmt.Errorf("%s: expected string, got %T", method, v)
	}
	return s, nil
}

func results(method string, v any, err error) ([]ProcessResult, error) {
	if err != nil {
		return nil, err
	}
	r, err := toProcessResults(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	return r, nil
}

// GetState returns supervisord's own state.
func (c *Client) GetState(ctx context.Context) (SupervisorState, error) {
	const m = "supervisor.getState"
	v, err := c.call(ctx, m)
	if err != nil {
		return SupervisorState{}, err
	}
	f, err := newFields(v)
	if err != nil {
		return SupervisorState{}, fmt.Errorf("%s: %w", m, err)
	}
	s := SupervisorState{Code: f.num("statecode"), Name: f.str("statename")}
	if f.err != nil {
		return SupervisorState{}, fmt.Errorf("%s: %w", m, f.err)
	}
	return s, nil
}

// GetSupervisorVersion returns the supervisord version, e.g. "4.2.5".
func (c *Client) GetSupervisorVersion(ctx context.Context) (string, error) {
	const m = "supervisor.getSupervisorVersion"
	v, err := c.call(ctx, m)
	return str(m, v, err)
}

// GetAllProcessInfo returns every process supervisord manages.
func (c *Client) GetAllProcessInfo(ctx context.Context) ([]ProcessInfo, error) {
	const m = "supervisor.getAllProcessInfo"
	v, err := c.call(ctx, m)
	if err != nil {
		return nil, err
	}
	items, isArr := v.([]any)
	if !isArr {
		return nil, fmt.Errorf("%s: expected array, got %T", m, v)
	}
	out := make([]ProcessInfo, 0, len(items))
	for i, item := range items {
		p, err := toProcessInfo(item)
		if err != nil {
			return nil, fmt.Errorf("%s: process %d: %w", m, i, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// GetProcessInfo returns one process by name ("name" or "group:name").
func (c *Client) GetProcessInfo(ctx context.Context, name string) (ProcessInfo, error) {
	const m = "supervisor.getProcessInfo"
	v, err := c.call(ctx, m, name)
	if err != nil {
		return ProcessInfo{}, err
	}
	p, err := toProcessInfo(v)
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("%s: %w", m, err)
	}
	return p, nil
}

// ListMethods returns the XML-RPC methods supervisord supports.
func (c *Client) ListMethods(ctx context.Context) ([]string, error) {
	const m = "system.listMethods"
	v, err := c.call(ctx, m)
	if err != nil {
		return nil, err
	}
	s, err := toStrings(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m, err)
	}
	return s, nil
}

// StartProcess starts a process; with wait it returns once it is RUNNING.
func (c *Client) StartProcess(ctx context.Context, name string, wait bool) error {
	const m = "supervisor.startProcess"
	v, err := c.control(ctx, wait, m, name, wait)
	return ok(m, v, err)
}

// StopProcess stops a process; with wait it returns once it has stopped.
func (c *Client) StopProcess(ctx context.Context, name string, wait bool) error {
	const m = "supervisor.stopProcess"
	v, err := c.control(ctx, wait, m, name, wait)
	return ok(m, v, err)
}

// RestartProcess stops and then starts a process, waiting for each step.
// supervisord has no restart call; this matches `supervisorctl restart`,
// which also starts a process that was not running.
func (c *Client) RestartProcess(ctx context.Context, name string) error {
	if err := c.StopProcess(ctx, name, true); err != nil && !errors.Is(err, ErrNotRunning) {
		return err
	}
	return c.StartProcess(ctx, name, true)
}

// StartProcessGroup starts every process in a group.
func (c *Client) StartProcessGroup(ctx context.Context, group string, wait bool) ([]ProcessResult, error) {
	const m = "supervisor.startProcessGroup"
	v, err := c.control(ctx, wait, m, group, wait)
	return results(m, v, err)
}

// StopProcessGroup stops every process in a group.
func (c *Client) StopProcessGroup(ctx context.Context, group string, wait bool) ([]ProcessResult, error) {
	const m = "supervisor.stopProcessGroup"
	v, err := c.control(ctx, wait, m, group, wait)
	return results(m, v, err)
}

// StartAllProcesses starts every process.
func (c *Client) StartAllProcesses(ctx context.Context, wait bool) ([]ProcessResult, error) {
	const m = "supervisor.startAllProcesses"
	v, err := c.control(ctx, wait, m, wait)
	return results(m, v, err)
}

// StopAllProcesses stops every process.
func (c *Client) StopAllProcesses(ctx context.Context, wait bool) ([]ProcessResult, error) {
	const m = "supervisor.stopAllProcesses"
	v, err := c.control(ctx, wait, m, wait)
	return results(m, v, err)
}

// SignalProcess sends a signal ("HUP", "USR1", or a number) to a process.
func (c *Client) SignalProcess(ctx context.Context, name, signal string) error {
	const m = "supervisor.signalProcess"
	v, err := c.call(ctx, m, name, signal)
	return ok(m, v, err)
}

func (c *Client) tail(ctx context.Context, m, name string, offset, length int) (TailResult, error) {
	v, err := c.call(ctx, m, name, offset, length)
	if err != nil {
		return TailResult{}, err
	}
	items, isArr := v.([]any)
	if !isArr || len(items) != 3 {
		return TailResult{}, fmt.Errorf("%s: expected [bytes, offset, overflow], got %#v", m, v)
	}
	bytes, ok1 := items[0].(string)
	off, ok2 := items[1].(int)
	overflow, ok3 := items[2].(bool)
	if !ok1 || !ok2 || !ok3 {
		return TailResult{}, fmt.Errorf("%s: expected [string, int, bool], got %#v", m, v)
	}
	return TailResult{Bytes: bytes, Offset: off, Overflow: overflow}, nil
}

// TailProcessStdoutLog reads up to length bytes of a process's stdout log
// starting at offset.
func (c *Client) TailProcessStdoutLog(ctx context.Context, name string, offset, length int) (TailResult, error) {
	return c.tail(ctx, "supervisor.tailProcessStdoutLog", name, offset, length)
}

// TailProcessStderrLog is TailProcessStdoutLog for stderr.
func (c *Client) TailProcessStderrLog(ctx context.Context, name string, offset, length int) (TailResult, error) {
	return c.tail(ctx, "supervisor.tailProcessStderrLog", name, offset, length)
}

// ReadLog reads supervisord's main log. A negative offset reads that many
// bytes from the end; length 0 means to the end.
func (c *Client) ReadLog(ctx context.Context, offset, length int) (string, error) {
	const m = "supervisor.readLog"
	v, err := c.call(ctx, m, offset, length)
	return str(m, v, err)
}

// ClearProcessLogs truncates a process's stdout and stderr logs.
func (c *Client) ClearProcessLogs(ctx context.Context, name string) error {
	const m = "supervisor.clearProcessLogs"
	v, err := c.call(ctx, m, name)
	return ok(m, v, err)
}

// ReloadConfig re-reads the config files without applying them, and reports
// which program groups were added, changed or removed.
func (c *Client) ReloadConfig(ctx context.Context) (ConfigDiff, error) {
	const m = "supervisor.reloadConfig"
	v, err := c.call(ctx, m)
	if err != nil {
		return ConfigDiff{}, err
	}
	// The result is [[added, changed, removed]].
	outer, isArr := v.([]any)
	if !isArr || len(outer) != 1 {
		return ConfigDiff{}, fmt.Errorf("%s: unexpected result %#v", m, v)
	}
	lists, isArr := outer[0].([]any)
	if !isArr || len(lists) != 3 {
		return ConfigDiff{}, fmt.Errorf("%s: unexpected result %#v", m, v)
	}
	var d ConfigDiff
	for i, dst := range []*[]string{&d.Added, &d.Changed, &d.Removed} {
		if *dst, err = toStrings(lists[i]); err != nil {
			return ConfigDiff{}, fmt.Errorf("%s: %w", m, err)
		}
	}
	return d, nil
}

// AddProcessGroup activates a group that ReloadConfig reported as added.
func (c *Client) AddProcessGroup(ctx context.Context, name string) error {
	const m = "supervisor.addProcessGroup"
	v, err := c.call(ctx, m, name)
	return ok(m, v, err)
}

// RemoveProcessGroup removes a stopped group from the active config.
func (c *Client) RemoveProcessGroup(ctx context.Context, name string) error {
	const m = "supervisor.removeProcessGroup"
	v, err := c.call(ctx, m, name)
	return ok(m, v, err)
}

// Restart restarts supervisord itself.
func (c *Client) Restart(ctx context.Context) error {
	const m = "supervisor.restart"
	v, err := c.call(ctx, m)
	return ok(m, v, err)
}

// Shutdown stops supervisord and every process it manages.
func (c *Client) Shutdown(ctx context.Context) error {
	const m = "supervisor.shutdown"
	v, err := c.call(ctx, m)
	return ok(m, v, err)
}
