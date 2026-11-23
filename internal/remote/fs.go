package remote

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"time"

	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/usage"
)

// BridgeFS reads a server's files through the bridge (config.FS), as the
// SSH user, or as root when the server uses sudo.
type BridgeFS struct {
	T       *BridgeTransport
	Timeout time.Duration // per call; 15s when zero
}

var _ config.FS = BridgeFS{}

// ErrNotConnected: the server's tab isn't connected right now.
var ErrNotConnected = errors.New("not connected to the server")

func (f BridgeFS) bridge() (*Bridge, context.Context, context.CancelFunc, error) {
	d := f.Timeout
	if d == 0 {
		d = 15 * time.Second
	}
	if f.T == nil {
		return nil, nil, nil, ErrNotConnected
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	b, err := f.T.Bridge(ctx)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return b, ctx, cancel, nil
}

// ReadFile reads a whole file.
func (f BridgeFS) ReadFile(name string) ([]byte, error) {
	b, ctx, cancel, err := f.bridge()
	if err != nil {
		return nil, err
	}
	defer cancel()
	data, err := b.ReadFile(ctx, name)
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	return data, nil
}

// Stat returns a file's details.
func (f BridgeFS) Stat(name string) (fs.FileInfo, error) {
	b, ctx, cancel, err := f.bridge()
	if err != nil {
		return nil, err
	}
	defer cancel()
	st, err := b.Stat(ctx, name)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	return statInfo{st}, nil
}

// Glob returns the matching paths, sorted.
func (f BridgeFS) Glob(pattern string) ([]string, error) {
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, err
	}
	b, ctx, cancel, err := f.bridge()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return b.Glob(ctx, pattern)
}

// Tail reads at most max bytes from the end of a file.
func (f BridgeFS) Tail(name string, max int) ([]byte, error) {
	b, ctx, cancel, err := f.bridge()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return b.Tail(ctx, name, max)
}

// Usage measures the server's process trees rooted at pids.
func (f BridgeFS) Usage(pids []int) (map[int]usage.Totals, error) {
	b, ctx, cancel, err := f.bridge()
	if err != nil {
		return nil, err
	}
	defer cancel()
	return b.Usage(ctx, pids)
}

// Machine is the server's core count and total memory.
func (f BridgeFS) Machine() (usage.Machine, error) {
	b, ctx, cancel, err := f.bridge()
	if err != nil {
		return usage.Machine{}, err
	}
	defer cancel()
	return b.Machine(ctx)
}

// statInfo is a FileInfo as an fs.FileInfo.
type statInfo struct{ f FileInfo }

func (s statInfo) Name() string       { return path.Base(s.f.Path) }
func (s statInfo) Size() int64        { return s.f.Size }
func (s statInfo) Mode() fs.FileMode  { return s.f.Mode }
func (s statInfo) ModTime() time.Time { return s.f.ModTime }
func (s statInfo) IsDir() bool        { return s.f.Mode.IsDir() }
func (s statInfo) Sys() any           { return s.f }
