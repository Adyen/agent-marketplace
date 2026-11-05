//go:build unix

package ownership

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// MaxAggregateFileBytes bounds one aggregate read, matching the 64 MiB safety
// ceiling used for canonical source archives. A file that does not fit inside
// it — including one that grows past the bound mid-read — is unsafe input.
const MaxAggregateFileBytes int64 = 64 << 20

// ReadAggregateFileAt reads rel beneath root without ever resolving a path
// more than once: the root is opened as a directory descriptor, each ancestor
// component is opened relative to its parent descriptor with O_DIRECTORY and
// O_NOFOLLOW, and the final component is opened with O_NOFOLLOW and O_NONBLOCK
// (so a FIFO cannot block the open). The opened descriptor is then
// fstat-validated as a regular file without extra hard links and read through
// a bounded LimitReader. A symlink in ANY component, a non-regular file, a
// hard-linked file, or a file exceeding the bound is refused with
// ErrUnsafeAggregateFile; nothing outside root can be reached because no
// component resolution ever leaves the descriptor chain.
func ReadAggregateFileAt(root, rel string) ([]byte, error) {
	components, err := aggregateRelComponents(rel)
	if err != nil {
		return nil, err
	}
	label := filepath.Join(root, filepath.FromSlash(rel))

	dirfd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open aggregate root", Path: root, Err: err}
	}
	for _, component := range components[:len(components)-1] {
		next, openErr := unix.Openat(dirfd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(dirfd)
		if openErr != nil {
			if errors.Is(openErr, unix.ELOOP) || errors.Is(openErr, unix.ENOTDIR) {
				slog.Warn("refused unsafe aggregate file", "path", label, "reason", "symlinked or non-directory component", "component", component)
				return nil, fmt.Errorf("%w: %q component %q is not a real directory", ErrUnsafeAggregateFile, label, component)
			}
			return nil, &os.PathError{Op: "open aggregate", Path: label, Err: openErr}
		}
		dirfd = next
	}
	fd, err := unix.Openat(dirfd, components[len(components)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	_ = unix.Close(dirfd)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			slog.Warn("refused unsafe aggregate file", "path", label, "reason", "symlink")
			return nil, fmt.Errorf("%w: open %q: %w", ErrUnsafeAggregateFile, label, err)
		}
		return nil, &os.PathError{Op: "open aggregate", Path: label, Err: err}
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("ownership: inspect aggregate %q: %w", label, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink > 1 {
		slog.Warn("refused unsafe aggregate file", "path", label, "mode", stat.Mode, "links", stat.Nlink)
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: %q must be a regular file without multiple links", ErrUnsafeAggregateFile, label)
	}
	if stat.Size > MaxAggregateFileBytes {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: %q exceeds the %d-byte aggregate limit", ErrUnsafeAggregateFile, label, MaxAggregateFileBytes)
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("ownership: restore blocking aggregate reads %q: %w", label, err)
	}
	file := os.NewFile(uintptr(fd), label)
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, MaxAggregateFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ownership: read aggregate %q: %w", label, err)
	}
	if int64(len(data)) > MaxAggregateFileBytes {
		// The file grew between fstat and the read; refuse rather than trust
		// bytes observed past the bound.
		return nil, fmt.Errorf("%w: %q exceeds the %d-byte aggregate limit", ErrUnsafeAggregateFile, label, MaxAggregateFileBytes)
	}
	return data, nil
}

// aggregateRelComponents splits a slash-separated root-relative path into
// traversal-safe components. Absolute paths, empty or "." or ".." components,
// and separators the kernel would treat differently are refused before any
// descriptor is opened.
func aggregateRelComponents(rel string) ([]string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return nil, fmt.Errorf("%w: aggregate path %q is not relative", ErrUnsafeAggregateFile, rel)
	}
	components := strings.Split(rel, "/")
	for _, c := range components {
		if c == "" || c == "." || c == ".." || c != filepath.Base(filepath.FromSlash(c)) {
			return nil, fmt.Errorf("%w: aggregate path %q is not a clean relative path", ErrUnsafeAggregateFile, rel)
		}
	}
	return components, nil
}
