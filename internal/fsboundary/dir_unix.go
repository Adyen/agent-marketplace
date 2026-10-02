//go:build unix

package fsboundary

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	// dirOpenFlags captures a directory. The root itself is authorized
	// lexically once by the caller, so it may resolve through symlinks.
	dirOpenFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC
	// componentOpenFlags walks one intermediate component below a captured
	// directory: symlinks in the middle of a path are never followed.
	componentOpenFlags = dirOpenFlags | unix.O_NOFOLLOW
	// leafOpenFlags opens a destination leaf without following a final
	// symlink and without blocking on special files such as FIFOs.
	leafOpenFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
)

// Dir is a captured directory: an open descriptor plus the identity and
// canonical path recorded at capture time. Every operation on a Dir is
// relative to the descriptor, so renaming or replacing the directory's
// path afterwards cannot redirect the operation; it stays bound to the
// captured object.
type Dir struct {
	file *os.File
	path string
	id   Identity
}

// OpenDir captures the directory at path as an authorized root. The path
// must be absolute; it is canonicalized once (symlinks in the root path are
// honored at capture time only) and the canonical directory is opened and
// pinned by identity. A missing path reports fs.ErrNotExist; a non-directory
// root is ErrUnsafePath.
func OpenDir(path string) (*Dir, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: root must be an absolute path: %s", ErrUnsafePath, path)
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("resolve root %s: %w", path, err)
	}
	fd, err := unix.Open(canonical, dirOpenFlags, 0)
	if errors.Is(err, unix.ENOTDIR) {
		return nil, fmt.Errorf("%w: root is not a directory: %s", ErrUnsafePath, canonical)
	}
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", canonical, err)
	}
	file := os.NewFile(uintptr(fd), canonical)
	id, err := identityOf(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect root %s: %w", canonical, err)
	}
	if !id.Mode.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("%w: root is not a directory: %s", ErrUnsafePath, canonical)
	}
	return &Dir{file: file, path: canonical, id: id}, nil
}

// Path returns the canonical path captured at open time. It is recorded for
// diagnostics and containment decisions; no operation resolves it again.
func (d *Dir) Path() string { return d.path }

// Identity returns the directory identity captured at open time.
func (d *Dir) Identity() Identity { return d.id }

// Close releases the captured descriptor.
func (d *Dir) Close() error { return d.file.Close() }

// Sync flushes the captured directory's metadata to stable storage.
func (d *Dir) Sync() error { return d.file.Sync() }

// errLeafSymlink marks a no-follow leaf open that stopped at a symlink. It
// is always wrapped together with a stable public refusal; destination
// capture uses it to apply the caller's link policy instead of refusing.
var errLeafSymlink = errors.New("fsboundary: leaf is a symlink")

// splitRelative validates a root-relative path and returns its components.
// Absolute paths, escapes, and empty or dot components are unsafe.
func splitRelative(rel string) ([]string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("%w: path must be relative: %s", ErrUnsafePath, rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: path escapes its root: %s", ErrUnsafePath, rel)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("%w: path escapes its root: %s", ErrUnsafePath, rel)
		}
	}
	return parts, nil
}

// openComponent opens one intermediate directory below parent without
// following symlinks.
func openComponent(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, componentOpenFlags, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR):
			return nil, fmt.Errorf("%w: unsafe path component %s", ErrUnsafePath, name)
		case errors.Is(err, unix.ENOENT):
			return nil, fmt.Errorf("open path component %s: %w", name, fs.ErrNotExist)
		default:
			return nil, fmt.Errorf("open path component %s: %w", name, err)
		}
	}
	file := os.NewFile(uintptr(fd), name)
	return file, nil
}

// openLeaf opens the final component below parent with no-follow and
// nonblocking flags and validates that the opened object is a singly linked
// regular file.
func openLeaf(parent *os.File, name string) (*os.File, Identity, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, leafOpenFlags, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.ELOOP):
			return nil, Identity{}, fmt.Errorf("%w: %s: %w", ErrUnsafePath, name, errLeafSymlink)
		case errors.Is(err, unix.ENXIO), errors.Is(err, unix.ENOTSOCK), errors.Is(err, unix.EOPNOTSUPP):
			// Linux reports ENXIO and macOS EOPNOTSUPP when a socket path is
			// opened; both are object-shape violations, not path violations.
			return nil, Identity{}, fmt.Errorf("%w: leaf is a special device or socket: %s", ErrUnsafeFile, name)
		case errors.Is(err, unix.ENOENT):
			return nil, Identity{}, fmt.Errorf("open leaf %s: %w", name, fs.ErrNotExist)
		default:
			return nil, Identity{}, fmt.Errorf("open leaf %s: %w", name, err)
		}
	}
	file := os.NewFile(uintptr(fd), name)
	id, err := identityOf(file)
	if err != nil {
		_ = file.Close()
		return nil, Identity{}, fmt.Errorf("inspect leaf %s: %w", name, err)
	}
	if err := validateLeafIdentity(id, name); err != nil {
		_ = file.Close()
		return nil, Identity{}, err
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		_ = file.Close()
		return nil, Identity{}, fmt.Errorf("restore blocking reads on %s: %w", name, err)
	}
	return file, id, nil
}

// validateLeafIdentity enforces the shared leaf contract: a regular file
// with exactly one link.
func validateLeafIdentity(id Identity, name string) error {
	if !id.Mode.IsRegular() {
		return fmt.Errorf("%w: leaf is not a regular file: %s", ErrUnsafeFile, name)
	}
	if id.Links != 1 {
		return fmt.Errorf("%w: leaf has %d links: %s", ErrUnsafeFile, id.Links, name)
	}
	return nil
}

// openRelative walks rel below d and opens its leaf. Intermediate components
// must be real directories; the leaf must be a singly linked regular file.
func (d *Dir) openRelative(rel string) (*os.File, Identity, error) {
	parts, err := splitRelative(rel)
	if err != nil {
		return nil, Identity{}, err
	}
	current := d.file
	for _, part := range parts[:len(parts)-1] {
		next, err := openComponent(current, part)
		if err != nil {
			if current != d.file {
				_ = current.Close()
			}
			return nil, Identity{}, err
		}
		if current != d.file {
			_ = current.Close()
		}
		current = next
	}
	if current != d.file {
		defer func() { _ = current.Close() }()
	}
	return openLeaf(current, parts[len(parts)-1])
}

// OpenFile opens rel below the captured directory with no-follow and
// nonblocking flags, validates that the opened object is a singly linked
// regular file, restores blocking reads, and returns the open descriptor
// with its captured identity.
func (d *Dir) OpenFile(rel string) (*os.File, Identity, error) {
	return d.openRelative(rel)
}

// identityOf captures the identity of an opened object through its
// descriptor (fstat), never through its name.
func identityOf(file *os.File) (Identity, error) {
	info, err := file.Stat()
	if err != nil {
		return Identity{}, err
	}
	return FileInfoIdentity(info)
}

func FileInfoIdentity(info os.FileInfo) (Identity, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, fmt.Errorf("unexpected stat payload for %s", info.Name())
	}
	return Identity{
		Device:       uint64(st.Dev),
		Inode:        st.Ino,
		Size:         info.Size(),
		ModTimeNS:    info.ModTime().UnixNano(),
		ChangeTimeNS: changeTimeNS(st),
		Links:        uint64(st.Nlink),
		Mode:         info.Mode(),
		UID:          st.Uid,
	}, nil
}

// FileIdentity captures the identity of an already opened file through its
// descriptor.
func FileIdentity(file *os.File) (Identity, error) {
	return identityOf(file)
}
