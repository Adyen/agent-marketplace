package dependencytxn

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type linkSnapshot struct {
	target     string
	generation Generation
	stat       unix.Stat_t
	exists     bool
	symlink    bool
}

func ReadSymlinkSnapshot(path string) (string, Generation, error) {
	snapshot, err := inspectLinkDestination(path)
	if err != nil {
		return "", Generation{}, fmt.Errorf("dependency transaction: symlink snapshot %s: %w", path, err)
	}
	if !snapshot.exists {
		return "", Generation{}, nil
	}
	if !snapshot.symlink {
		return "", Generation{}, fmt.Errorf("%w: destination is not a symlink: %s", ErrUnsafePath, path)
	}
	return snapshot.target, snapshot.generation, nil
}

func inspectLinkDestination(path string) (linkSnapshot, error) {
	parent, name, err := openParentNoFollow(path)
	if err != nil {
		return linkSnapshot{}, err
	}
	defer func() { _ = unix.Close(parent) }()
	return inspectLinkAt(parent, name)
}

func inspectLinkAt(parent int, name string) (linkSnapshot, error) {
	var info unix.Stat_t
	if err := unix.Fstatat(parent, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return linkSnapshot{}, nil
		}
		return linkSnapshot{}, fmt.Errorf("%w: inspect symlink destination: %v", ErrUnsafePath, err)
	}
	snapshot := linkSnapshot{stat: info, exists: true}
	if info.Mode&unix.S_IFMT != unix.S_IFLNK {
		return snapshot, nil
	}
	target, err := readLinkAt(parent, name)
	if err != nil {
		return linkSnapshot{}, err
	}
	snapshot.target = target
	snapshot.symlink = true
	snapshot.generation = Generation{Exists: true, Digest: Digest([]byte(target))}
	return snapshot, nil
}

func openParentNoFollow(path string) (int, string, error) {
	canonical, err := normalizeDestinationPath(path)
	if err != nil {
		return -1, "", err
	}
	name := filepath.Base(canonical)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return -1, "", fmt.Errorf("%w: destination has no leaf name", ErrUnsafePath)
	}
	root := filepath.VolumeName(canonical) + string(filepath.Separator)
	parent := filepath.Dir(canonical)
	rel, err := filepath.Rel(root, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return -1, "", fmt.Errorf("%w: destination parent escapes root", ErrUnsafePath)
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_DIRECTORY
	fd, err := unix.Open(root, flags, 0)
	if err != nil {
		return -1, "", fmt.Errorf("%w: open destination root: %v", ErrUnsafePath, err)
	}
	if rel != "." {
		for _, component := range strings.Split(rel, string(filepath.Separator)) {
			if component == "" || component == "." {
				continue
			}
			if component == ".." {
				_ = unix.Close(fd)
				return -1, "", fmt.Errorf("%w: destination parent traverses upward", ErrUnsafePath)
			}
			next, openErr := unix.Openat(fd, component, flags, 0)
			if openErr != nil {
				_ = unix.Close(fd)
				return -1, "", fmt.Errorf("%w: open destination parent: %v", ErrUnsafePath, openErr)
			}
			_ = unix.Close(fd)
			fd = next
		}
	}
	return fd, name, nil
}

func readLinkAt(parent int, name string) (string, error) {
	buf := make([]byte, 4096)
	n, err := unix.Readlinkat(parent, name, buf)
	if err != nil {
		return "", fmt.Errorf("%w: read symlink target: %v", ErrUnsafePath, err)
	}
	if n == len(buf) {
		return "", fmt.Errorf("%w: symlink target exceeds platform path limit", ErrUnsafePath)
	}
	return string(buf[:n]), nil
}

func unlinkSymlinkAt(path, expectedTarget string) error {
	parent, name, err := openParentNoFollow(path)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(parent) }()
	before, err := inspectLinkAt(parent, name)
	if err != nil {
		return err
	}
	if !before.exists {
		return nil
	}
	if !before.symlink || before.target != expectedTarget {
		return fmt.Errorf("%w: symlink changed before deletion: %s", ErrRecoveryBlocked, path)
	}
	after, err := inspectLinkAt(parent, name)
	if err != nil {
		return err
	}
	if !after.exists || !after.symlink || after.target != expectedTarget || before.stat.Dev != after.stat.Dev || before.stat.Ino != after.stat.Ino {
		return fmt.Errorf("%w: symlink changed before deletion: %s", ErrRecoveryBlocked, path)
	}
	if err := unix.Unlinkat(parent, name, 0); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("dependency transaction: unlink symlink %s: %w", path, err)
	}
	return nil
}
