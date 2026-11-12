package app

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// ownerName is who owns a file ("root"), or its uid when the name is
// unknown, or "" when the file info carries no owner.
func ownerName(fi os.FileInfo) string {
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
