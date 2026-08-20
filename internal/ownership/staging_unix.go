//go:build unix

package ownership

import (
	"os"
	"syscall"
)

// O_EXCL prevents an attacker from substituting a pre-existing symlink at the
// final component, while O_NOFOLLOW makes that no-follow requirement explicit
// on Unix filesystems.
func openStagedFile(path string, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
}
