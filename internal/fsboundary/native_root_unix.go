//go:build unix

package fsboundary

import (
	"fmt"
	"os"
)

func (d *Dir) NativeRoot() (*os.Root, error) {
	return os.OpenRoot(fmt.Sprintf("/dev/fd/%d", d.file.Fd()))
}
