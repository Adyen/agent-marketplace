//go:build darwin || linux

package ownership

import (
	"os"
	"syscall"
)

func openOwnershipFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
