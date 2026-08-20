//go:build unix

package telemetryconfig

import (
	"os"
	"syscall"
)

// fileOwnerUID returns the numeric user ID that owns the file described by
// info. It is implemented for Unix-like systems; other platforms report an
// unknown owner so ownership checks are skipped.
func fileOwnerUID(info os.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, false
	}
	return int(st.Uid), true
}
