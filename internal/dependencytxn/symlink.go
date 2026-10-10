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

func readRegularFileAt(parent int, name string) (int, []byte, unix.Stat_t, error) {
	var beforeName unix.Stat_t
	if err := unix.Fstatat(parent, name, &beforeName, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: inspect staged file: %v", ErrRecoveryBlocked, err)
	}
	if beforeName.Mode&unix.S_IFMT != unix.S_IFREG || beforeName.Nlink != 1 {
		return -1, nil, unix.Stat_t{}, ErrRecoveryBlocked
	}
	if beforeName.Size < 0 || beforeName.Size > maxDependencyFileSize {
		return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: staged file exceeds %d bytes", ErrRecoveryBlocked, maxDependencyFileSize)
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: open staged file: %v", ErrRecoveryBlocked, err)
	}
	keepFD := false
	defer func() {
		if !keepFD {
			_ = unix.Close(fd)
		}
	}()
	var beforeFD unix.Stat_t
	if err := unix.Fstat(fd, &beforeFD); err != nil || !sameRegularFileState(beforeName, beforeFD) {
		return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: staged file changed while opening", ErrRecoveryBlocked)
	}
	data := make([]byte, 0, int(beforeFD.Size))
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := unix.Read(fd, buffer)
		if n > 0 {
			if int64(len(data))+int64(n) > maxDependencyFileSize {
				return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: staged file exceeds %d bytes", ErrRecoveryBlocked, maxDependencyFileSize)
			}
			data = append(data, buffer[:n]...)
		}
		if errors.Is(readErr, unix.EINTR) {
			continue
		}
		if readErr != nil {
			return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: read staged file: %v", ErrRecoveryBlocked, readErr)
		}
		if n == 0 {
			break
		}
	}
	var afterFD, afterName unix.Stat_t
	if err := unix.Fstat(fd, &afterFD); err != nil {
		return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: recheck staged file: %v", ErrRecoveryBlocked, err)
	}
	if err := unix.Fstatat(parent, name, &afterName, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: recheck staged file name: %v", ErrRecoveryBlocked, err)
	}
	if !sameRegularFileState(beforeFD, afterFD) || !sameRegularFileState(afterFD, afterName) || int64(len(data)) != afterFD.Size {
		return -1, nil, unix.Stat_t{}, fmt.Errorf("%w: staged file changed while reading", ErrRecoveryBlocked)
	}
	keepFD = true
	return fd, data, afterFD, nil
}

func sameRegularFileState(a, b unix.Stat_t) bool {
	return a.Mode&unix.S_IFMT == unix.S_IFREG && b.Mode&unix.S_IFMT == unix.S_IFREG &&
		a.Dev == b.Dev && a.Ino == b.Ino && a.Size == b.Size && a.Mode == b.Mode && a.Nlink == b.Nlink &&
		a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func replaceSymlinkAt(file journalFile) error {
	if file.Temp == "" || filepath.Dir(file.Temp) != filepath.Dir(file.Path) {
		return fmt.Errorf("%w: replacement stage is not beside %s", ErrRecoveryBlocked, file.Path)
	}
	if err := ensureContained(filepath.Dir(file.Path), file.Temp); err != nil {
		return fmt.Errorf("%w: %v", ErrRecoveryBlocked, err)
	}
	parent, name, err := openParentNoFollow(file.Path)
	if err != nil {
		return fmt.Errorf("%w: replacement path is unsafe: %w", ErrRecoveryBlocked, err)
	}
	defer func() { _ = unix.Close(parent) }()
	beforeLink, err := inspectLinkAt(parent, name)
	if err != nil {
		return err
	}
	if !beforeLink.exists || !beforeLink.symlink || beforeLink.target != file.OldLinkTarget || beforeLink.generation != file.Old {
		return fmt.Errorf("%w: symlink changed before replacement: %s", ErrRecoveryBlocked, file.Path)
	}
	stageName := filepath.Base(file.Temp)
	fd, data, stageInfo, err := readRegularFileAt(parent, stageName)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	if stageInfo.Nlink != 1 || uint32(stageInfo.Mode)&0o7777 != uint32(file.NewMode) || Digest(data) != file.NewDigest {
		return fmt.Errorf("%w: staged replacement changed for %s", ErrRecoveryBlocked, file.Path)
	}
	afterLink, err := inspectLinkAt(parent, name)
	if err != nil {
		return err
	}
	if !afterLink.exists || !afterLink.symlink || afterLink.target != file.OldLinkTarget || afterLink.generation != file.Old ||
		beforeLink.stat.Dev != afterLink.stat.Dev || beforeLink.stat.Ino != afterLink.stat.Ino {
		return fmt.Errorf("%w: symlink changed before replacement: %s", ErrRecoveryBlocked, file.Path)
	}
	var stageFD, stageNameInfo unix.Stat_t
	if err := unix.Fstat(fd, &stageFD); err != nil {
		return fmt.Errorf("%w: recheck staged replacement for %s: %v", ErrRecoveryBlocked, file.Path, err)
	}
	if err := unix.Fstatat(parent, stageName, &stageNameInfo, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("%w: recheck staged replacement name for %s: %v", ErrRecoveryBlocked, file.Path, err)
	}
	if !sameRegularFileState(stageInfo, stageFD) || !sameRegularFileState(stageFD, stageNameInfo) ||
		uint32(stageFD.Mode)&0o7777 != uint32(file.NewMode) {
		return fmt.Errorf("%w: staged replacement changed before publish for %s", ErrRecoveryBlocked, file.Path)
	}
	if err := unix.Renameat(parent, stageName, parent, name); err != nil {
		return fmt.Errorf("%w: dependency transaction: replace symlink %s: %w", ErrRecoveryBlocked, file.Path, err)
	}
	return nil
}
