package lockfile

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const MaxFileSize int64 = 4 << 20

var (
	ErrUnsafeFile   = errors.New("unsafe lock file")
	ErrFileTooLarge = errors.New("lock file too large")
)

func Read(path string) (Document, error) {
	data, err := ReadBytes(path)
	if err != nil {
		return Document{}, err
	}
	return Decode(data)
}

func ReadBytes(path string) ([]byte, error) {
	data, _, err := ReadBytesInfo(path)
	return data, err
}

func ReadBytesInfo(path string) ([]byte, os.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, nil, fmt.Errorf("%w: %q is a symlink", ErrUnsafeFile, path)
		}
		return nil, nil, fmt.Errorf("open lock %q: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect lock %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%w: %q is not a regular file", ErrUnsafeFile, path)
	}
	if info.Size() > MaxFileSize {
		return nil, nil, fmt.Errorf("%w: %q exceeds %d bytes", ErrFileTooLarge, path, MaxFileSize)
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		return nil, nil, fmt.Errorf("restore blocking lock reads %q: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read lock %q: %w", path, err)
	}
	if int64(len(data)) > MaxFileSize {
		return nil, nil, fmt.Errorf("%w: %q exceeds %d bytes", ErrFileTooLarge, path, MaxFileSize)
	}
	return data, info, nil
}
