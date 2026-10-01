package ingest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

var ErrHookSupportUnsafe = errors.New("marketplace.ingest.hook-support-unsafe")

// readHookSupportFile keeps traversal and the bounded read on no-follow
// descriptors. Nonblocking opens prevent special files from hanging ingestion.
func readHookSupportFile(root, relative string) ([]byte, bool, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, fmt.Errorf("%w: open support root: %w", ErrHookSupportUnsafe, err)
	}
	defer func() { _ = unix.Close(fd) }()
	parts := strings.Split(relative, "/")
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, false, fmt.Errorf("%w: open support directory: %w", ErrHookSupportUnsafe, err)
		}
		_ = unix.Close(fd)
		fd = next
	}
	fileFD, err := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, false, fmt.Errorf("%w: open support file: %w", ErrHookSupportUnsafe, err)
	}
	file := os.NewFile(uintptr(fileFD), relative)
	defer func() { _ = file.Close() }()
	var stat unix.Stat_t
	if err := unix.Fstat(fileFD, &stat); err != nil {
		return nil, false, err
	}
	const limit = 2 * 1024 * 1024
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size > limit {
		return nil, false, fmt.Errorf("%w: support must be a singly linked regular file within the byte limit", ErrHookSupportUnsafe)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > limit {
		return nil, false, fmt.Errorf("%w: support exceeds the byte limit", ErrHookSupportUnsafe)
	}
	return data, stat.Mode&0o111 != 0, nil
}
