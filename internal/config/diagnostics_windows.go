//go:build windows

package config

import "os"

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
	// Windows does not expose POSIX uid/gid through os.FileInfo, so report
	// the owner/group as unknown rather than pretending it was verified.
	return "unknown", "unknown"
}

// isTrustedOwner always returns true on Windows because the underlying
// filesystem does not expose POSIX ownership. This is a known limitation:
// callers should treat the managed-installation trust model as unverifiable
// on Windows and rely on other host-level controls.
func isTrustedOwner(info os.FileInfo) bool {
	return true
}
