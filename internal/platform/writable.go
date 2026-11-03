//go:build unix

package platform

import "syscall"

// DirWritable reports whether this user may create files in dir. It asks
// the kernel (access(2)) instead of writing a test file, so checking leaves
// nothing behind.
func DirWritable(dir string) bool {
	return dir != "" && syscall.Access(dir, 0x2 /* W_OK */) == nil
}
