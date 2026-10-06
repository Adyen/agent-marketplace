package harnesscompile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"golang.org/x/sys/unix"
)

// ErrStalePruneConflict reports that a stale output changed after it was
// selected for pruning. The changed path is intentionally preserved.
var ErrStalePruneConflict = errors.New("stale output changed during pruning")

// StaleFileCandidate is the no-follow identity snapshot captured when sync
// discovers an output that is not in the desired artifact set. Consumers that
// publish in a later transaction must pass these candidates back to
// RevalidateStaleFileCandidate before removing them.
type StaleFileCandidate struct {
	Path    string
	Info    fs.FileInfo
	Digest  [sha256.Size]byte
	parents []staleParentSnapshot
}

type staleParentSnapshot struct {
	path string
	info fs.FileInfo
}

func snapshotStaleFileCandidate(path, rel string) (StaleFileCandidate, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return StaleFileCandidate{}, err
	}
	candidate := StaleFileCandidate{Path: rel, Info: info}
	parents, err := snapshotStaleParents(path, rel)
	if err != nil {
		return StaleFileCandidate{}, err
	}
	candidate.parents = parents
	if !info.Mode().IsRegular() {
		// Symlinks and special files are never candidates for destructive
		// pruning. Keeping their identity lets revalidation report and preserve
		// them without following a user-controlled path.
		return candidate, nil
	}
	digest, err := digestNoFollow(path, info)
	if err != nil {
		return StaleFileCandidate{}, err
	}
	candidate.Digest = digest
	return candidate, nil
}

func snapshotStaleParents(path, rel string) ([]staleParentSnapshot, error) {
	root := path
	for current := filepath.FromSlash(rel); current != "."; current = filepath.Dir(current) {
		root = filepath.Dir(root)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("stale candidate output root is not a real directory")
	}
	// Include the output root itself. Conversion publication discovers stale
	// files before it publishes and removes them afterward, so substituting the
	// root in that interval must preserve the replacement tree rather than
	// redirecting a stale removal into it.
	parents := []staleParentSnapshot{{path: root, info: rootInfo}}
	for current := filepath.Dir(filepath.FromSlash(rel)); current != "."; current = filepath.Dir(current) {
		parentPath := filepath.Join(root, current)
		info, err := os.Lstat(parentPath)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("stale candidate parent %q is not a real directory", current)
		}
		parents = append(parents, staleParentSnapshot{path: parentPath, info: info})
	}
	return parents, nil
}

func staleParentsUnchanged(parents []staleParentSnapshot) (bool, error) {
	for _, parent := range parents {
		current, err := os.Lstat(parent.path)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(parent.info, current) {
			return false, nil
		}
	}
	return true, nil
}

// RevalidateStaleFileCandidate checks the candidate's inode, parent
// components, and bytes without following a symlink. It returns false when
// the path or any parent disappeared or changed; callers must preserve it.
func RevalidateStaleFileCandidate(path string, candidate StaleFileCandidate) (bool, error) {
	parentsOK, err := staleParentsUnchanged(candidate.parents)
	if err != nil || !parentsOK {
		return false, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if candidate.Info == nil || !candidate.Info.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(candidate.Info, current) {
		return false, nil
	}
	digest, err := digestNoFollow(path, candidate.Info)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, fsboundary.ErrGenerationConflict) {
			return false, nil
		}
		return false, err
	}
	if digest != candidate.Digest {
		return false, nil
	}
	after, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	parentsOK, err = staleParentsUnchanged(candidate.parents)
	if err != nil || !parentsOK {
		return false, err
	}
	return after.Mode().IsRegular() && os.SameFile(current, after), nil
}

func RemoveStaleFileCandidate(path string, candidate StaleFileCandidate) error {
	return removeStaleFileCandidate(path, candidate, nil)
}

func removeStaleFileCandidate(path string, candidate StaleFileCandidate, beforeRename func() error) error {
	unchanged, err := RevalidateStaleFileCandidate(path, candidate)
	if err != nil {
		return err
	}
	if !unchanged {
		return fmt.Errorf("%w: %s", ErrStalePruneConflict, candidate.Path)
	}

	parentPath := filepath.Dir(path)
	parentFD, err := unix.Open(parentPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open stale candidate parent: %w", err)
	}
	defer func() { _ = unix.Close(parentFD) }()
	if err := verifyStaleParentFD(parentFD, candidate, parentPath); err != nil {
		return err
	}

	quarantine, err := createStaleQuarantine(parentFD)
	if err != nil {
		return err
	}
	quarantineActive := true
	defer func() {
		if quarantineActive {
			_ = unix.Unlinkat(parentFD, quarantine, 0)
		}
	}()
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	slog.Debug("SDK stale payload ready for quarantine", "path", path, "phase", "sdk-stale-quarantine")
	if err := unix.Renameat(parentFD, filepath.Base(path), parentFD, quarantine); err != nil {
		if err == unix.ENOENT {
			return nil
		}
		return fmt.Errorf("quarantine stale candidate %q: %w", candidate.Path, err)
	}

	matches, verifyErr := staleCandidateFileMatches(parentFD, quarantine, candidate)
	if verifyErr == nil && matches {
		if err := unix.Unlinkat(parentFD, quarantine, 0); err != nil {
			quarantineActive = false
			return fmt.Errorf("remove quarantined stale candidate %q: %w", candidate.Path, err)
		}
		quarantineActive = false
		return nil
	}

	conflict := fmt.Errorf("%w: %s", ErrStalePruneConflict, candidate.Path)
	if verifyErr != nil {
		conflict = errors.Join(conflict, verifyErr)
	}
	if restoreErr := restoreStaleQuarantine(parentFD, quarantine, filepath.Base(path)); restoreErr != nil {
		quarantineActive = false
		return errors.Join(conflict, fmt.Errorf("stale candidate %q preserved in quarantine %q: %w", candidate.Path, quarantine, restoreErr))
	}
	quarantineActive = false
	return conflict
}

func verifyStaleParentFD(parentFD int, candidate StaleFileCandidate, parentPath string) error {
	duplicate, err := unix.Dup(parentFD)
	if err != nil {
		return fmt.Errorf("duplicate stale candidate parent descriptor: %w", err)
	}
	parentFile := os.NewFile(uintptr(duplicate), "stale-parent")
	defer func() { _ = parentFile.Close() }()
	parent, err := parentFile.Stat()
	if err != nil {
		return fmt.Errorf("stat stale candidate parent: %w", err)
	}
	var expected fs.FileInfo
	for _, snapshot := range candidate.parents {
		if snapshot.path == parentPath {
			expected = snapshot.info
			break
		}
	}
	if expected == nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, parent) {
		return fmt.Errorf("%w: stale candidate parent changed", ErrStalePruneConflict)
	}
	return nil
}

func createStaleQuarantine(parentFD int) (string, error) {
	for range 32 {
		var token [12]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", fmt.Errorf("generate stale quarantine name: %w", err)
		}
		name := ".agent-marketplace-stale-" + hex.EncodeToString(token[:])
		fd, err := unix.Openat(parentFD, name, unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if err == nil {
			if closeErr := unix.Close(fd); closeErr != nil {
				_ = unix.Unlinkat(parentFD, name, 0)
				return "", fmt.Errorf("close stale quarantine: %w", closeErr)
			}
			return name, nil
		}
		if err != unix.EEXIST {
			return "", fmt.Errorf("create stale quarantine: %w", err)
		}
	}
	return "", errors.New("create stale quarantine: exhausted unique names")
}

func staleCandidateFileMatches(parentFD int, name string, candidate StaleFileCandidate) (matches bool, err error) {
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return false, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if candidate.Info == nil || !info.Mode().IsRegular() || !os.SameFile(candidate.Info, info) {
		return false, nil
	}
	expected, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return false, err
	}
	digest, err := digestOpenFile(file, expected)
	if err != nil {
		return false, err
	}
	return digest == candidate.Digest, nil
}

func digestOpenFile(file *os.File, expected fsboundary.Identity) ([sha256.Size]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return [sha256.Size]byte{}, err
	}
	before, err := fsboundary.FileIdentity(file)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	if !before.Mode.IsRegular() || before.Links != 1 {
		return [sha256.Size]byte{}, fsboundary.ErrUnsafeFile
	}
	if !before.SameState(expected) {
		return [sha256.Size]byte{}, fsboundary.ErrGenerationConflict
	}
	data, identity, err := fsboundary.ReadFile(file, expected.Size)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	if !identity.SameState(before) {
		return [sha256.Size]byte{}, fsboundary.ErrGenerationConflict
	}
	return sha256.Sum256(data), nil
}

func restoreStaleQuarantine(parentFD int, quarantine, destination string) error {
	if err := unix.Linkat(parentFD, quarantine, parentFD, destination, 0); err != nil {
		return err
	}
	return unix.Unlinkat(parentFD, quarantine, 0)
}

func digestNoFollow(path string, expected fs.FileInfo) (digest [sha256.Size]byte, err error) {
	if expected == nil {
		return digest, fsboundary.ErrGenerationConflict
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return digest, err
	}
	root, err := fsboundary.OpenDir(filepath.Dir(absolute))
	if err != nil {
		return digest, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	file, opened, err := root.OpenFile(filepath.Base(absolute))
	if err != nil {
		return digest, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	original, err := fsboundary.FileInfoIdentity(expected)
	if err != nil {
		return digest, err
	}
	if !opened.SameState(original) {
		return digest, fsboundary.ErrGenerationConflict
	}
	slog.Debug("SDK stale payload admitted", "path", path, "phase", "sdk-stale-read")
	return digestOpenFile(file, original)
}
