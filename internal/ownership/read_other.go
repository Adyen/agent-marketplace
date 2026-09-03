//go:build !darwin && !linux

package ownership

import "os"

// Non-Unix builds are retained for compile-only compatibility. The supported
// runtime platforms use read_unix.go, which adds O_NOFOLLOW.
func openOwnershipFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY, 0)
}
