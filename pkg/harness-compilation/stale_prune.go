package harnesscompile

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
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
		if errors.Is(err, os.ErrNotExist) {
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

func digestNoFollow(path string, expected fs.FileInfo) ([sha256.Size]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	opened, err := file.Stat()
	if err != nil {
		return [sha256.Size]byte{}, errors.Join(err, file.Close())
	}
	if expected == nil || !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		return [sha256.Size]byte{}, errors.Join(fmt.Errorf("stale candidate identity changed while reading"), file.Close())
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return [sha256.Size]byte{}, errors.Join(copyErr, closeErr)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}
