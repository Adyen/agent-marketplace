//go:build !windows

package config

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func fileTypeString(mode os.FileMode) string {
	switch {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsDir():
		return "directory"
	case mode.IsRegular():
		return "regular file"
	default:
		return "other"
	}
}

func ownerAndGroup(info os.FileInfo) (owner, group string) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "-", "-"
	}

	if u, err := user.LookupId(strconv.Itoa(int(st.Uid))); err == nil {
		owner = u.Username
	} else {
		owner = strconv.Itoa(int(st.Uid))
	}

	if g, err := user.LookupGroupId(strconv.Itoa(int(st.Gid))); err == nil {
		group = g.Name
	} else {
		group = strconv.Itoa(int(st.Gid))
	}

	return owner, group
}

func isTrustedOwner(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return st.Uid == 0
}

func ownedByCurrentProcess(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid())
}
