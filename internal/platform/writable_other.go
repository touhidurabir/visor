//go:build !unix

package platform

// DirWritable is always false where Visor doesn't run.
func DirWritable(string) bool { return false }
