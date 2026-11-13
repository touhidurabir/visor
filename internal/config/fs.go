package config

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FS is how the config package reads supervisord's files: this machine's
// disk (OS), or a server's through the remote bridge.
type FS interface {
	ReadFile(name string) ([]byte, error)
	Stat(name string) (fs.FileInfo, error)
	Glob(pattern string) ([]string, error)
}

type osFS struct{}

func (osFS) ReadFile(name string) ([]byte, error)  { return os.ReadFile(name) }
func (osFS) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }
func (osFS) Glob(pattern string) ([]string, error) { return filepath.Glob(pattern) }

// OS reads this machine's disk.
var OS FS = osFS{}

// Files is the FS d was discovered on (OS when unset).
func (d Discovery) Files() FS {
	if d.FS == nil {
		return OS
	}
	return d.FS
}
