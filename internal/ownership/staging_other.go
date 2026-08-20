//go:build !unix

package ownership

import "os"

// O_EXCL refuses a pre-existing final component, including a symlink, on the
// non-Unix targets supported by this package.
func openStagedFile(path string, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
}
