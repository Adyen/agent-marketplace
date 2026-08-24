//go:build !unix

package telemetryconfig

import "os"

// fileOwnerUID returns the numeric user ID that owns the file described by
// info. Non-Unix platforms do not expose a stable UID through os.FileInfo, so
// ownership checks are skipped.
func fileOwnerUID(info os.FileInfo) (int, bool) {
	return -1, false
}
