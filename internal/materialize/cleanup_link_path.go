package materialize

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var ErrManagedLinkCleanupConflict = errors.New("materialize: managed link cleanup conflict")

type managedLinkCandidate struct {
	scopeRoot string
	rel       string
	target    string
	parents   []fs.FileInfo
	info      fs.FileInfo
}

func cleanupLinkConflict(message string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrManagedLinkCleanupConflict, message)
	}
	return errors.Join(ErrManagedLinkCleanupConflict, fmt.Errorf("%s: %w", message, err))
}

func snapshotManagedLink(scopeRoot, rel, target string) (managedLinkCandidate, error) {
	if target == "" || !filepath.IsAbs(target) {
		return managedLinkCandidate{}, cleanupLinkConflict("invalid link target", nil)
	}
	parent, parents, err := openManagedLinkParent(scopeRoot, rel, nil)
	if err != nil {
		return managedLinkCandidate{}, err
	}
	defer func() { _ = parent.Close() }()
	name := filepath.Base(filepath.FromSlash(rel))
	info, err := parent.Lstat(name)
	if err != nil {
		return managedLinkCandidate{}, cleanupLinkConflict("inspect managed link", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return managedLinkCandidate{}, cleanupLinkConflict("managed path is not a symlink", nil)
	}
	got, err := parent.Readlink(name)
	if err != nil {
		return managedLinkCandidate{}, cleanupLinkConflict("read managed link target", err)
	}
	if got != target {
		return managedLinkCandidate{}, cleanupLinkConflict("managed link target changed", nil)
	}
	second, err := parent.Lstat(name)
	if err != nil {
		return managedLinkCandidate{}, cleanupLinkConflict("recheck managed link", err)
	}
	if !os.SameFile(info, second) || second.Mode() != info.Mode() || second.Size() != info.Size() || !second.ModTime().Equal(info.ModTime()) {
		return managedLinkCandidate{}, cleanupLinkConflict("managed link changed", nil)
	}
	return managedLinkCandidate{scopeRoot: scopeRoot, rel: filepath.ToSlash(rel), target: target, parents: parents, info: info}, nil
}

func openManagedLinkParent(scopeRoot, rel string, expected []fs.FileInfo) (*os.Root, []fs.FileInfo, error) {
	if !filepath.IsAbs(scopeRoot) || rel == "." || !fs.ValidPath(filepath.ToSlash(rel)) {
		return nil, nil, cleanupLinkConflict("invalid managed link path", nil)
	}
	if expected != nil && len(expected) == 0 {
		return nil, nil, cleanupLinkConflict("empty parent identity", nil)
	}
	root, err := os.OpenRoot(scopeRoot)
	if err != nil {
		return nil, nil, cleanupLinkConflict("open scope root", err)
	}
	closeRoot := func(r *os.Root) {
		if r != nil {
			_ = r.Close()
		}
	}
	rootInfo, err := root.Stat(".")
	if err != nil {
		closeRoot(root)
		return nil, nil, cleanupLinkConflict("inspect scope root", err)
	}
	if !rootInfo.IsDir() {
		closeRoot(root)
		return nil, nil, cleanupLinkConflict("scope root is not a directory", nil)
	}
	if expected != nil && !os.SameFile(rootInfo, expected[0]) {
		closeRoot(root)
		return nil, nil, cleanupLinkConflict("scope root changed", nil)
	}
	parents := []fs.FileInfo{rootInfo}
	parentPath := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	if parentPath == "." {
		if expected != nil && len(expected) != 1 {
			closeRoot(root)
			return nil, nil, cleanupLinkConflict("parent identity length changed", nil)
		}
		return root, parents, nil
	}
	current := root
	for _, component := range strings.Split(parentPath, "/") {
		entry, err := current.Lstat(component)
		if err != nil {
			closeRoot(current)
			return nil, nil, cleanupLinkConflict("inspect managed parent", err)
		}
		if !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			closeRoot(current)
			return nil, nil, cleanupLinkConflict("managed parent is not a real directory", nil)
		}
		next, err := current.OpenRoot(component)
		if err != nil {
			closeRoot(current)
			return nil, nil, cleanupLinkConflict("open managed parent", err)
		}
		nextInfo, err := next.Stat(".")
		if err != nil {
			closeRoot(next)
			closeRoot(current)
			return nil, nil, cleanupLinkConflict("inspect opened parent", err)
		}
		if !os.SameFile(entry, nextInfo) {
			closeRoot(next)
			closeRoot(current)
			return nil, nil, cleanupLinkConflict("managed parent replaced", nil)
		}
		rechecked, err := current.Lstat(component)
		if err != nil || !os.SameFile(entry, rechecked) || rechecked.Mode()&os.ModeSymlink != 0 {
			closeRoot(next)
			closeRoot(current)
			return nil, nil, cleanupLinkConflict("managed parent changed during open", err)
		}
		if expected != nil {
			if len(parents) >= len(expected) || !os.SameFile(expected[len(parents)], entry) {
				closeRoot(next)
				closeRoot(current)
				return nil, nil, cleanupLinkConflict("managed parent identity changed", nil)
			}
		}
		parents = append(parents, nextInfo)
		closeRoot(current)
		current = next
	}
	if expected != nil && len(expected) != len(parents) {
		closeRoot(current)
		return nil, nil, cleanupLinkConflict("parent identity length changed", nil)
	}
	return current, parents, nil
}

func (c managedLinkCandidate) open() (*os.Root, error) {
	if len(c.parents) == 0 || c.info == nil {
		return nil, cleanupLinkConflict("missing captured link identity", nil)
	}
	parent, _, err := openManagedLinkParent(c.scopeRoot, c.rel, c.parents)
	if err != nil {
		return nil, err
	}
	if err := c.matches(parent, filepath.Base(filepath.FromSlash(c.rel))); err != nil {
		_ = parent.Close()
		return nil, err
	}
	return parent, nil
}

func (c managedLinkCandidate) matches(parent *os.Root, name string) error {
	if c.info == nil {
		return cleanupLinkConflict("missing captured link info", nil)
	}
	info, err := parent.Lstat(name)
	if err != nil {
		return cleanupLinkConflict("inspect managed link", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return cleanupLinkConflict("managed path is no longer a symlink", nil)
	}
	if !os.SameFile(c.info, info) || info.Mode() != c.info.Mode() || info.Size() != c.info.Size() || !info.ModTime().Equal(c.info.ModTime()) {
		return cleanupLinkConflict("managed link metadata changed", nil)
	}
	got, err := parent.Readlink(name)
	if err != nil {
		return cleanupLinkConflict("read managed link target", err)
	}
	if got != c.target {
		return cleanupLinkConflict("managed link target changed", nil)
	}
	second, err := parent.Lstat(name)
	if err != nil {
		return cleanupLinkConflict("recheck managed link", err)
	}
	if !os.SameFile(info, second) || second.Mode() != info.Mode() || second.Size() != info.Size() || !second.ModTime().Equal(info.ModTime()) {
		return cleanupLinkConflict("managed link changed", nil)
	}
	return nil
}

func removeEmptyManagedQuarantine(parent *os.Root, name string, expected fs.FileInfo) error {
	info, err := parent.Lstat(name)
	if err != nil || expected == nil || !expected.IsDir() || !os.SameFile(expected, info) {
		return cleanupLinkConflict("quarantine ownership changed at "+filepath.Join(parent.Name(), name), err)
	}
	return parent.Remove(name)
}

func (c managedLinkCandidate) remove(parent *os.Root, beforeMove, afterMove func() error) (retErr error) {
	validated, err := c.open()
	if err != nil {
		return err
	}
	_ = validated.Close()
	if parent == nil {
		return cleanupLinkConflict("missing parent descriptor", nil)
	}
	parentInfo, err := parent.Stat(".")
	if err != nil || !os.SameFile(parentInfo, c.parents[len(c.parents)-1]) {
		return cleanupLinkConflict("parent descriptor differs from captured parent", err)
	}
	name := filepath.Base(filepath.FromSlash(c.rel))
	if err := c.matches(parent, name); err != nil {
		return err
	}
	var quarantine *os.Root
	var quarantineName string
	var quarantineInfo fs.FileInfo
	for range 32 {
		quarantineName = ".agent-marketplace-cleanup-" + rand.Text()
		if err := parent.Mkdir(quarantineName, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return cleanupLinkConflict("create quarantine", err)
		}
		quarantineInfo, err = parent.Lstat(quarantineName)
		if err != nil || !quarantineInfo.IsDir() || quarantineInfo.Mode().Perm() != 0o700 {
			return cleanupLinkConflict("unsafe quarantine retained at "+filepath.Join(parent.Name(), quarantineName), err)
		}
		quarantine, err = parent.OpenRoot(quarantineName)
		if err != nil {
			return errors.Join(cleanupLinkConflict("open quarantine", err), removeEmptyManagedQuarantine(parent, quarantineName, quarantineInfo))
		}
		openedInfo, statErr := quarantine.Stat(".")
		if statErr != nil || !os.SameFile(quarantineInfo, openedInfo) {
			_ = quarantine.Close()
			return errors.Join(cleanupLinkConflict("quarantine replaced", statErr), removeEmptyManagedQuarantine(parent, quarantineName, quarantineInfo))
		}
		break
	}
	if quarantine == nil {
		return cleanupLinkConflict("quarantine name exhausted", nil)
	}
	keep := false
	defer func() {
		retErr = errors.Join(retErr, quarantine.Close())
		if keep {
			retErr = errors.Join(retErr, cleanupLinkConflict("entry preserved in quarantine "+filepath.Join(parent.Name(), quarantineName, "entry"), nil))
			return
		}
		retErr = errors.Join(retErr, removeEmptyManagedQuarantine(parent, quarantineName, quarantineInfo))
	}()
	parentFile, err := parent.Open(".")
	if err != nil {
		return cleanupLinkConflict("open parent descriptor", err)
	}
	defer func() { _ = parentFile.Close() }()
	qFile, err := quarantine.Open(".")
	if err != nil {
		return cleanupLinkConflict("open quarantine descriptor", err)
	}
	defer func() { _ = qFile.Close() }()
	if beforeMove != nil {
		if err := beforeMove(); err != nil {
			return cleanupLinkConflict("before move", err)
		}
	}
	if err := unix.Renameat(int(parentFile.Fd()), name, int(qFile.Fd()), "entry"); err != nil {
		return cleanupLinkConflict("move managed link", err)
	}
	keep = true
	var hookErr error
	if afterMove != nil {
		hookErr = afterMove()
	}
	verifyErr := errors.Join(hookErr, c.matches(quarantine, "entry"))
	if verifyErr == nil {
		if err := quarantine.Remove("entry"); err != nil {
			return cleanupLinkConflict("remove quarantined link", err)
		}
		keep = false
		return nil
	}
	conflictErr := cleanupLinkConflict("quarantined object changed", verifyErr)
	if err := unix.Linkat(int(qFile.Fd()), "entry", int(parentFile.Fd()), name, 0); err != nil {
		return errors.Join(conflictErr, err)
	}
	if err := quarantine.Remove("entry"); err != nil {
		return errors.Join(conflictErr, err)
	}
	keep = false
	return conflictErr
}
