package catalog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

var ErrHookSupportUnsafe = errors.New("marketplace.ingest.hook-support-unsafe")

// ReadHookSupportFile anchors every path component at the admitted root.
func ReadHookSupportFile(root, relative string) ([]byte, bool, error) {
	if err := artifact.ValidateSupportPath(relative); err != nil {
		return nil, false, fmt.Errorf("%w: invalid support path", ErrHookSupportUnsafe)
	}
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
		return nil, false, fmt.Errorf("%w: stat support file: %w", ErrHookSupportUnsafe, err)
	}
	const limit = 2 * 1024 * 1024
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size > limit {
		return nil, false, fmt.Errorf("%w: support must be a singly linked regular file within the byte limit", ErrHookSupportUnsafe)
	}
	if err := unix.SetNonblock(fileFD, false); err != nil {
		return nil, false, fmt.Errorf("%w: set blocking support read: %w", ErrHookSupportUnsafe, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, false, fmt.Errorf("%w: bounded support read: %w", ErrHookSupportUnsafe, err)
	}
	if len(data) > limit {
		return nil, false, fmt.Errorf("%w: support exceeds the byte limit", ErrHookSupportUnsafe)
	}
	return data, stat.Mode&0o111 != 0, nil
}
