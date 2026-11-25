//go:build darwin

package fsboundary

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// descriptorDirPath uses F_GETPATH to report the open descriptor's actual path spelling.
func descriptorDirPath(file *os.File) (string, error) {
	var buf [unix.PathMax]byte
	var pin runtime.Pinner
	pin.Pin(&buf[0])
	defer pin.Unpin()
	// FcntlInt takes the pointer argument as an integer, so pin its backing array.
	_, err := unix.FcntlInt(file.Fd(), unix.F_GETPATH, int(uintptr(unsafe.Pointer(&buf[0]))))
	runtime.KeepAlive(file)
	if err != nil {
		return "", err
	}
	return unix.ByteSliceToString(buf[:]), nil
}
