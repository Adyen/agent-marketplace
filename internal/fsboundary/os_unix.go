//go:build unix

package fsboundary

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// This file holds the Unix implementation of the platform primitives the
// portable fsboundary logic is written against: descriptor-relative opens
// with O_NOFOLLOW, renameat, unlinkat, and fstat-based identity.

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
	// stagedCreateFlags creates a brand-new staged file: it must not exist, it
	// is never a symlink, and it starts owner-only until the caller chmods it.
	stagedCreateFlags = unix.O_RDWR | unix.O_CREAT | unix.O_EXCL | unix.O_NOFOLLOW | unix.O_CLOEXEC
)

var openat = unix.Openat

// checkComponentName refuses path components the platform treats specially.
// Unix treats every name other than "." and ".." as an ordinary name.
func checkComponentName(string) error { return nil }

// openDirectoryPath opens the already-canonical directory path.
func openDirectoryPath(canonical string) (*os.File, error) {
	fd, err := unix.Open(canonical, dirOpenFlags, 0)
	if errors.Is(err, unix.ENOTDIR) {
		return nil, fmt.Errorf("%w: root is not a directory: %s", ErrUnsafePath, canonical)
	}
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", canonical, err)
	}
	return os.NewFile(uintptr(fd), canonical), nil
}

// openComponent opens one intermediate directory below parent without
// following symlinks.
func openComponent(parent *os.File, name string) (*os.File, error) {
	path := filepath.Join(parent.Name(), name)
	fd, err := openat(int(parent.Fd()), name, componentOpenFlags, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR):
			return nil, unsafeComponentError(parent, name, path)
		case errors.Is(err, unix.ENOENT):
			return nil, fmt.Errorf("open path component %s: %w", path, fs.ErrNotExist)
		default:
			return nil, fmt.Errorf("open path component %s: %w", path, err)
		}
	}
	file := os.NewFile(uintptr(fd), path)
	return file, nil
}

// unsafeComponentError classifies a refused intermediate component by its
// no-follow shape: open errnos for symlinks differ between Linux and macOS.
func unsafeComponentError(parent *os.File, name, path string) error {
	var st unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err == nil && st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("%w: %s is a symbolic link; intermediate path components must be real directories", ErrUnsafePath, path)
	}
	return fmt.Errorf("%w: %s is not a directory", ErrUnsafePath, path)
}

// openLeaf opens the final component below parent with no-follow and
// nonblocking flags and validates that the opened object is a singly linked
// regular file.
func openLeaf(parent *os.File, name string) (*os.File, Identity, error) {
	path := filepath.Join(parent.Name(), name)
	fd, err := openat(int(parent.Fd()), name, leafOpenFlags, 0)
	if err != nil {
		switch {
		case errors.Is(err, unix.ELOOP):
			return nil, Identity{}, fmt.Errorf("%w: %s: %w", ErrUnsafePath, path, errLeafSymlink)
		case errors.Is(err, unix.ENXIO), errors.Is(err, unix.ENOTSOCK), errors.Is(err, unix.EOPNOTSUPP):
			// Linux reports ENXIO and macOS EOPNOTSUPP when a socket path is
			// opened; both are object-shape violations, not path violations.
			return nil, Identity{}, fmt.Errorf("%w: leaf is a special device or socket: %s", ErrUnsafeFile, path)
		case errors.Is(err, unix.ENOENT):
			return nil, Identity{}, fmt.Errorf("open leaf %s: %w", path, fs.ErrNotExist)
		default:
			return nil, Identity{}, fmt.Errorf("open leaf %s: %w", path, err)
		}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, Identity{}, errors.Join(fmt.Errorf("inspect leaf %s: %w", path, err), unix.Close(fd))
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, Identity{}, errors.Join(fmt.Errorf("%w: leaf is not a regular file: %s", ErrUnsafeFile, path), unix.Close(fd))
	}
	if st.Nlink != 1 {
		return nil, Identity{}, errors.Join(fmt.Errorf("%w: leaf has %d links: %s", ErrUnsafeFile, st.Nlink, path), unix.Close(fd))
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		return nil, Identity{}, errors.Join(fmt.Errorf("restore blocking reads on %s: %w", path, err), unix.Close(fd))
	}
	file := os.NewFile(uintptr(fd), path)
	id, err := identityOf(file)
	if err != nil {
		_ = file.Close()
		return nil, Identity{}, fmt.Errorf("inspect leaf %s: %w", path, err)
	}
	if err := validateLeafIdentity(id, path); err != nil {
		_ = file.Close()
		return nil, Identity{}, err
	}
	return file, id, nil
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

// FileInfoIdentity derives an identity from file metadata. On Unix the
// metadata carries the device, inode, change time, link count, and owner.
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

// ownedByCurrentUser reports whether id names an object owned by the
// effective user.
func ownedByCurrentUser(id Identity) bool { return id.UID == uint32(os.Geteuid()) }

// restoreBlocking returns a descriptor opened nonblocking to blocking mode.
func restoreBlocking(file *os.File) error { return unix.SetNonblock(int(file.Fd()), false) }

// syncDir flushes a captured directory's metadata to stable storage.
func syncDir(file *os.File) error { return file.Sync() }

// syncFile flushes an opened regular file to stable storage.
func syncFile(file *os.File) error { return file.Sync() }

// mkdirAt creates the directory name below parent. An existing name,
// including a symlink, reports an error satisfying fs.ErrExist.
func mkdirAt(parent *os.File, name string, perm os.FileMode) error {
	return unix.Mkdirat(int(parent.Fd()), name, uint32(perm.Perm()))
}

// rmdirAt removes the empty directory name below parent.
func rmdirAt(parent *os.File, name string) error {
	return unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
}

// unlinkAt removes the non-directory leaf name below parent without
// following it.
func unlinkAt(parent *os.File, name string) error {
	return unix.Unlinkat(int(parent.Fd()), name, 0)
}

// renameAt renames src below srcParent to dst below dstParent, replacing an
// existing destination.
func renameAt(srcParent *os.File, src string, dstParent *os.File, dst string) error {
	return unix.Renameat(int(srcParent.Fd()), src, int(dstParent.Fd()), dst)
}

// createExclusiveAt creates a brand-new private regular file name below
// parent, open for reading and writing. An existing name reports an error
// satisfying fs.ErrExist; a symlink is never followed.
func createExclusiveAt(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, stagedCreateFlags, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

// openPersistentAt opens name below parent for reading and writing,
// creating it with mode when absent. The leaf must be a singly linked
// regular file and is never a followed symlink. The returned file is
// reported under display.
func openPersistentAt(parent *os.File, name string, mode fs.FileMode, display string) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_CREAT, uint32(mode.Perm()))
	if errors.Is(err, unix.ENOENT) {
		fd, err = unix.Openat(int(parent.Fd()), name, flags, 0)
	}
	if err != nil {
		return nil, err
	}
	// Validate and restore blocking I/O on the raw descriptor before wrapping
	// it, so a refused leaf never reaches os.NewFile and an admitted one is
	// not registered as a nonblocking descriptor.
	var st unix.Stat_t
	err = unix.Fstat(fd, &st)
	if err == nil && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1) {
		err = ErrUnsafeFile
	}
	if err == nil {
		if blockErr := unix.SetNonblock(fd, false); blockErr != nil {
			err = fmt.Errorf("restore blocking I/O on %s: %w", name, blockErr)
		}
	}
	if err != nil {
		if closeErr := unix.Close(fd); closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), display), nil
}

// isSymlinkAt reports whether name below parent is a symbolic link.
func isSymlinkAt(parent *os.File, name string) bool {
	var stat unix.Stat_t
	err := unix.Fstatat(int(parent.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	return err == nil && stat.Mode&unix.S_IFMT == unix.S_IFLNK
}

// readLink reads the raw text of the symlink name below parent. A leaf that
// stopped being a symlink since the no-follow open is a generation conflict.
func readLink(parent *os.File, name string) (string, error) {
	buf := make([]byte, 256)
	for {
		n, err := unix.Readlinkat(int(parent.Fd()), name, buf)
		if err != nil {
			switch {
			case errors.Is(err, unix.EINVAL):
				return "", fmt.Errorf("%w: link %s replaced by a non-link during capture", ErrGenerationConflict, name)
			case errors.Is(err, unix.ENOENT):
				return "", fmt.Errorf("read link %s: %w", name, fs.ErrNotExist)
			default:
				return "", fmt.Errorf("read link %s: %w", name, err)
			}
		}
		if n < len(buf) {
			return string(buf[:n]), nil
		}
		if len(buf) >= maxLinkText {
			return "", fmt.Errorf("%w: link text over %d bytes: %s", ErrUnsafePath, maxLinkText, name)
		}
		buf = make([]byte, len(buf)*2)
	}
}

// openFilesystemRoot opens "/" as a walk root for user-policy targets that
// live outside the captured root.
func openFilesystemRoot() (*os.File, error) {
	fd, err := unix.Open("/", dirOpenFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	return os.NewFile(uintptr(fd), "/"), nil
}
