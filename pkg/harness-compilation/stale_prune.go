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

func removeStaleFileCandidate(path string, candidate StaleFileCandidate, beforeRename func() error) (err error) {
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
	defer func() {
		if closeErr := unix.Close(parentFD); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if err := verifyStaleParentFD(parentFD, candidate, parentPath); err != nil {
		return err
	}

	quarantine, reservation, err := createStaleQuarantine(parentFD)
	if quarantine == "" {
		return err
	}
	reserved := StaleFileCandidate{Info: reservation, Digest: sha256.Sum256(nil)}
	quarantineActive := true
	defer func() {
		if !quarantineActive {
			return
		}
		matches, inspectErr := staleCandidateFileMatches(parentFD, quarantine, reserved)
		if errors.Is(inspectErr, fs.ErrNotExist) {
			return
		}
		if inspectErr != nil || !matches {
			err = errors.Join(err, ErrStalePruneConflict, inspectErr)
			return
		}
		if removeErr := unix.Unlinkat(parentFD, quarantine, 0); removeErr != nil {
			err = errors.Join(err, removeErr)
		}
	}()
	if err != nil {
		return err
	}
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	slog.Debug("SDK stale payload ready for quarantine", "path", path, "phase", "sdk-stale-quarantine")
	matchesReservation, inspectErr := staleCandidateFileMatches(parentFD, quarantine, reserved)
	if inspectErr != nil || !matchesReservation {
		return errors.Join(ErrStalePruneConflict, inspectErr)
	}
	if err := unix.Renameat(parentFD, filepath.Base(path), parentFD, quarantine); err != nil {
		if err == unix.ENOENT {
			return nil
		}
		return fmt.Errorf("quarantine stale candidate %q: %w", candidate.Path, err)
	}

	var quarantined unix.Stat_t
	if statErr := unix.Fstatat(parentFD, quarantine, &quarantined, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
		quarantineActive = false
		return errors.Join(ErrStalePruneConflict, statErr)
	}
	matches, verifyErr := staleCandidateFileMatches(parentFD, quarantine, candidate)
	if verifyErr == nil && matches {
		slog.Debug("SDK stale quarantine payload verified for removal", "path", filepath.Join(parentPath, quarantine), "phase", "sdk-stale-quarantine-remove")
		matches, verifyErr = staleCandidateFileMatches(parentFD, quarantine, candidate)
		if verifyErr != nil || !matches {
			quarantineActive = false
			return errors.Join(ErrStalePruneConflict, verifyErr)
		}
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
	slog.Debug("SDK stale quarantine payload refused before restoration", "path", filepath.Join(parentPath, quarantine), "phase", "sdk-stale-quarantine-restore")
	if restoreErr := restoreStaleQuarantine(parentFD, quarantine, filepath.Base(path), quarantined); restoreErr != nil {
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

func createStaleQuarantine(parentFD int) (string, fs.FileInfo, error) {
	for range 32 {
		var token [12]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", nil, fmt.Errorf("generate stale quarantine name: %w", err)
		}
		name := ".agent-marketplace-stale-" + hex.EncodeToString(token[:])
		fd, err := unix.Openat(parentFD, name, unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if err == nil {
			file := os.NewFile(uintptr(fd), name)
			info, inspectErr := file.Stat()
			if closeErr := file.Close(); closeErr != nil {
				inspectErr = errors.Join(inspectErr, closeErr)
			}
			if inspectErr != nil {
				return name, info, inspectErr
			}
			return name, info, nil
		}
		if err != unix.EEXIST {
			return "", nil, fmt.Errorf("create stale quarantine: %w", err)
		}
	}
	return "", nil, errors.New("create stale quarantine: exhausted unique names")
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

func restoreStaleQuarantine(parentFD int, quarantine, destination string, expected unix.Stat_t) error {
	for step := range 2 {
		var current unix.Stat_t
		if err := unix.Fstatat(parentFD, quarantine, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if current.Dev != expected.Dev || current.Ino != expected.Ino || current.Mode != expected.Mode || current.Size != expected.Size || current.Nlink != expected.Nlink {
			return fsboundary.ErrGenerationConflict
		}
		if step == 1 {
			return unix.Unlinkat(parentFD, quarantine, 0)
		}
		if err := unix.Linkat(parentFD, quarantine, parentFD, destination, 0); err != nil {
			return err
		}
		expected.Nlink++
		slog.Debug("SDK observed quarantine generation restored", "path", quarantine, "phase", "sdk-stale-quarantine-restore-linked")
	}
	return nil
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
	return digestCapturedNoFollow(root, filepath.Base(absolute), path, expected)
}

func digestCapturedNoFollow(parent *fsboundary.Dir, name, path string, expected fs.FileInfo) (digest [sha256.Size]byte, err error) {
	if expected == nil {
		return digest, fsboundary.ErrGenerationConflict
	}
	file, opened, err := parent.OpenFile(name)
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
