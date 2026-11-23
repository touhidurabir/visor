package remote

import (
	"context"
	"encoding/json"
	"io/fs"
	"strconv"
	"time"

	"github.com/touhidurabir/visor/internal/usage"
)

// FileInfo is what the bridge reports for a path on the server.
type FileInfo struct {
	Path    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	UID     int
	GID     int
	Dev     uint64 // with Ino, the same file under another path (a link)
	Ino     uint64
	Owner   string
	Group   string
}

// IsDir reports whether the path is a directory.
func (f FileInfo) IsDir() bool { return f.Mode.IsDir() }

type statJSON struct {
	Mode  uint32  `json:"mode"`
	Size  int64   `json:"size"`
	Mtime float64 `json:"mtime"`
	UID   int     `json:"uid"`
	GID   int     `json:"gid"`
	Dev   uint64  `json:"dev"`
	Ino   uint64  `json:"ino"`
	Owner string  `json:"owner"`
	Group string  `json:"group"`
}

// unixMode turns a st_mode into an fs.FileMode (type bits and permissions).
func unixMode(m uint32) fs.FileMode {
	mode := fs.FileMode(m & 0o777)
	switch m & 0o170000 {
	case 0o040000:
		mode |= fs.ModeDir
	case 0o120000:
		mode |= fs.ModeSymlink
	case 0o140000:
		mode |= fs.ModeSocket
	case 0o010000:
		mode |= fs.ModeNamedPipe
	case 0o020000:
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case 0o060000:
		mode |= fs.ModeDevice
	}
	if m&0o4000 != 0 {
		mode |= fs.ModeSetuid
	}
	if m&0o2000 != 0 {
		mode |= fs.ModeSetgid
	}
	if m&0o1000 != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}

// Stat returns a path's details (following symlinks), as the bridge's user.
func (b *Bridge) Stat(ctx context.Context, path string) (FileInfo, error) {
	f, err := b.request(ctx, 'S', []byte(path))
	if err != nil {
		return FileInfo{}, err
	}
	var s statJSON
	if err := json.Unmarshal(f.payload, &s); err != nil {
		return FileInfo{}, err
	}
	sec := int64(s.Mtime)
	return FileInfo{
		Path: path, Size: s.Size, Mode: unixMode(s.Mode),
		ModTime: time.Unix(sec, int64((s.Mtime-float64(sec))*1e9)),
		UID:     s.UID, GID: s.GID, Dev: s.Dev, Ino: s.Ino, Owner: s.Owner, Group: s.Group,
	}, nil
}

// ReadFile reads a whole file (up to 32 MB).
func (b *Bridge) ReadFile(ctx context.Context, path string) ([]byte, error) {
	f, err := b.request(ctx, 'R', []byte(path))
	if err != nil {
		return nil, err
	}
	return f.payload, nil
}

// Tail reads at most max bytes from the end of a file.
func (b *Bridge) Tail(ctx context.Context, path string, max int) ([]byte, error) {
	req, _ := json.Marshal(map[string]any{"path": path, "max": max})
	f, err := b.request(ctx, 'T', req)
	if err != nil {
		return nil, err
	}
	return f.payload, nil
}

// Glob returns the paths matching pattern, sorted (Python's glob rules,
// which match filepath.Glob for supervisord's patterns).
func (b *Bridge) Glob(ctx context.Context, pattern string) ([]string, error) {
	f, err := b.request(ctx, 'G', []byte(pattern))
	if err != nil {
		return nil, err
	}
	var out []string
	err = json.Unmarshal(f.payload, &out)
	return out, err
}

type usageJSON struct {
	HZ   int64               `json:"hz"`
	Page uint64              `json:"page"`
	PIDs map[string][3]int64 `json:"pids"`
}

// Usage measures the process trees rooted at pids from the server's /proc,
// in one round trip. A pid the server doesn't show is left out.
func (b *Bridge) Usage(ctx context.Context, pids []int) (map[int]usage.Totals, error) {
	req, err := json.Marshal(map[string][]int{"pids": pids})
	if err != nil {
		return nil, err
	}
	f, err := b.request(ctx, 'M', req)
	if err != nil {
		return nil, err
	}
	var r usageJSON
	if err := json.Unmarshal(f.payload, &r); err != nil {
		return nil, err
	}
	tick := time.Second / time.Duration(max(r.HZ, 1))
	out := make(map[int]usage.Totals, len(r.PIDs))
	for k, v := range r.PIDs {
		pid, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		out[pid] = usage.Totals{CPU: time.Duration(v[0]) * tick, RSS: uint64(max(v[1], 0)) * r.Page, Procs: int(v[2])}
	}
	return out, nil
}
