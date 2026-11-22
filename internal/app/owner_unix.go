package app

import (
	"os"
	"os/user"
	"strconv"
	"syscall"

	"github.com/touhidurabir/visor/internal/remote"
)

// ownerName is who owns a file ("root"), or its uid when the name is
// unknown, or "" when the file info carries no owner.
func ownerName(fi os.FileInfo) string {
	if rf, ok := fi.Sys().(remote.FileInfo); ok { // a server's file
		return rf.Owner
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	id := strconv.FormatUint(uint64(st.Uid), 10)
	if u, err := user.LookupId(id); err == nil {
		return u.Username
	}
	return id
}
