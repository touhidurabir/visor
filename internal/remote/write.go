package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/touhidurabir/visor/internal/apply"
)

// The bridge's write requests: the server checks the hash and renames, so
// an edit made elsewhere is never overwritten and a dropped connection
// leaves the old file or the new one.

type writeReq struct {
	Path string `json:"path"`
	Data []byte `json:"data,omitempty"` // base64 in JSON
	Hash string `json:"hash,omitempty"`
	What string `json:"what,omitempty"`
}

func (b *Bridge) do(ctx context.Context, kind byte, r writeReq) error {
	req, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = b.request(ctx, kind, req)
	return err
}

// Replace swaps a file's content if it still hashes to hash (see apply.Disk).
func (b *Bridge) Replace(ctx context.Context, path string, data []byte, hash string) error {
	return b.do(ctx, 'W', writeReq{Path: path, Data: data, Hash: hash})
}

// Create writes a new file (0644), never replacing one.
func (b *Bridge) Create(ctx context.Context, path string, data []byte) error {
	return b.do(ctx, 'C', writeReq{Path: path, Data: data})
}

// Delete removes a file that still hashes to hash.
func (b *Bridge) Delete(ctx context.Context, path, hash string) error {
	return b.do(ctx, 'U', writeReq{Path: path, Hash: hash})
}

// Check asks whether the bridge's user may "write" a file, "remove" it, or
// write in a "folder" (EACCES when not).
func (b *Bridge) Check(ctx context.Context, path, what string) error {
	return b.do(ctx, 'A', writeReq{Path: path, What: what})
}

// UserExists reports whether a user name or uid exists on the server.
func (b *Bridge) UserExists(ctx context.Context, name string) (bool, error) {
	f, err := b.request(ctx, 'L', []byte(name))
	if err != nil {
		return false, err
	}
	var ok bool
	err = json.Unmarshal(f.payload, &ok)
	return ok, err
}

// BridgeFS changes the server's config files too (apply.Disk).
var _ apply.Disk = BridgeFS{}

// call runs one bridge request with BridgeFS's timeout.
func (f BridgeFS) call(fn func(*Bridge, context.Context) error) error {
	b, ctx, cancel, err := f.bridge()
	if err != nil {
		return err
	}
	defer cancel()
	return fn(b, ctx)
}

// Exists reports whether path is there (following symlinks).
// shortcut: a dangling symlink reads as missing; add an lstat request if that matters.
func (f BridgeFS) Exists(path string) (bool, error) {
	_, err := f.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (f BridgeFS) Replace(path string, content []byte, hash string) error {
	return diskError(path, f.call(func(b *Bridge, ctx context.Context) error { return b.Replace(ctx, path, content, hash) }))
}

func (f BridgeFS) Create(path string, content []byte) error {
	return diskError(path, f.call(func(b *Bridge, ctx context.Context) error { return b.Create(ctx, path, content) }))
}

func (f BridgeFS) Delete(path, hash string) error {
	return diskError(path, f.call(func(b *Bridge, ctx context.Context) error { return b.Delete(ctx, path, hash) }))
}

func (f BridgeFS) CheckWritable(path string) error {
	return diskError(path, f.call(func(b *Bridge, ctx context.Context) error { return b.Check(ctx, path, "write") }))
}

func (f BridgeFS) CheckRemovable(path string) error {
	return diskError(path, f.call(func(b *Bridge, ctx context.Context) error { return b.Check(ctx, path, "remove") }))
}

// FolderWritable reports whether the bridge's user may add files in dir.
func (f BridgeFS) FolderWritable(dir string) bool {
	return f.call(func(b *Bridge, ctx context.Context) error { return b.Check(ctx, dir, "folder") }) == nil
}

// UserExists reports whether a user exists on the server (false when it
// can't be asked).
func (f BridgeFS) UserExists(name string) bool {
	var ok bool
	_ = f.call(func(b *Bridge, ctx context.Context) error {
		var err error
		ok, err = b.UserExists(ctx, name)
		return err
	})
	return ok
}

// diskError maps the server's answer to the errors apply's callers check.
func diskError(path string, err error) error {
	var re *RemoteError
	if err == nil || !errors.As(err, &re) {
		return err
	}
	switch re.Code {
	case "ECHANGED":
		return fmt.Errorf("%s: %w", re.Msg, apply.ErrChangedOnDisk)
	case "EEXIST":
		return fmt.Errorf("%s: %w", path, apply.ErrExists)
	case "EACCES", "EPERM", "EROFS":
		return fmt.Errorf("cannot write %s: %w", path, apply.ErrNotWritable)
	}
	return &fs.PathError{Op: "write", Path: path, Err: err}
}
