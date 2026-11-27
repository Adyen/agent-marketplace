//go:build unix && !darwin

package fsboundary

import (
	"os"
	"runtime"
	"strconv"
)

func descriptorDirPath(file *os.File) (string, error) {
	path, err := os.Readlink("/proc/self/fd/" + strconv.FormatUint(uint64(file.Fd()), 10))
	runtime.KeepAlive(file)
	return path, err
}
