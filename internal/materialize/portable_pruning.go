package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type managedLinkSnapshot struct {
	observed os.FileInfo
	text     string
}

func observeManagedLink(path string, roots ...*os.Root) (managedLinkSnapshot, error) {
	lstat, readlink := os.Lstat, os.Readlink
	if len(roots) > 0 {
		lstat, readlink = roots[0].Lstat, roots[0].Readlink
	}
	info, err := lstat(path)
	if err != nil {
		return managedLinkSnapshot{}, err
	}
	snapshot := managedLinkSnapshot{observed: info}
	if info.Mode()&os.ModeSymlink == 0 {
		return snapshot, nil
	}
	snapshot.text, err = readlink(path)
	if err != nil {
		return managedLinkSnapshot{}, err
	}
	fresh, err := lstat(path)
	if err != nil {
		return managedLinkSnapshot{}, err
	}
	before, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return managedLinkSnapshot{}, err
	}
	after, err := fsboundary.FileInfoIdentity(fresh)
	if err != nil {
		return managedLinkSnapshot{}, err
	}
	if !before.SameState(after) {
		return managedLinkSnapshot{}, fsboundary.ErrGenerationConflict
	}
	return snapshot, nil
}

func missingPruneEntry(err error) bool {
	return errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrGenerationConflict) &&
		!errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile)
}

func pruneManagedLink(dst string, snapshot managedLinkSnapshot, boundary *destinationBoundary, dryRun bool) error {
	if snapshot.observed == nil || snapshot.observed.Mode()&os.ModeSymlink == 0 || snapshot.text == "" {
		return fsboundary.ErrGenerationConflict
	}
	if boundary != nil && boundary.pruneClassificationHook != nil {
		boundary.pruneClassificationHook(dst)
	}
	return prunePortableOutput(dst, snapshot.observed, "", snapshot.text, boundary, dryRun)
}

func removePortableOutput(dst string, observed os.FileInfo, wantDigest string, boundary *destinationBoundary) error {
	return prunePortableOutput(dst, observed, wantDigest, "", boundary, false)
}

func prunePortableOutput(dst string, observed os.FileInfo, wantDigest, wantLinkText string, boundary *destinationBoundary, dryRun bool) (err error) {
	parent, err := capturePortableParent(dst, boundary, false)
	if err != nil {
		if observed == nil && errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile) {
			return nil
		}
		return err
	}
	if parent == nil {
		if observed == nil {
			return nil
		}
		return fsboundary.ErrGenerationConflict
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	name := filepath.Base(dst)
	var expected fsboundary.Identity
	if observed != nil {
		expected, err = fsboundary.FileInfoIdentity(observed)
		if err != nil {
			return err
		}
	}
	verify := func() error {
		info, err := native.Lstat(name)
		if observed == nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: portable prune destination appeared", fsboundary.ErrGenerationConflict)
		}
		if missingPruneEntry(err) && wantLinkText != "" {
			return err
		}
		if err != nil {
			return fmt.Errorf("%w: portable prune destination unavailable: %w", fsboundary.ErrGenerationConflict, err)
		}
		identity, err := fsboundary.FileInfoIdentity(info)
		if err != nil {
			return err
		}
		if !identity.SameState(expected) {
			return fmt.Errorf("%w: portable prune destination changed", fsboundary.ErrGenerationConflict)
		}
		if wantDigest == "" {
			if identity.Mode&os.ModeSymlink == 0 {
				return fsboundary.ErrGenerationConflict
			}
			text, err := native.Readlink(name)
			if err != nil {
				return err
			}
			if (wantLinkText != "" && text != wantLinkText) || !strings.HasPrefix(text, cache.CacheDir()+string(os.PathSeparator)) {
				return fsboundary.ErrGenerationConflict
			}
		}
		if wantDigest != "" {
			h := sha256.New()
			if err := writePortableDigestEntry(h, parent, name, ""); err != nil {
				return err
			}
			if "sha256:"+hex.EncodeToString(h.Sum(nil)) != wantDigest {
				return fmt.Errorf("%w: portable prune ownership changed", fsboundary.ErrGenerationConflict)
			}
		}
		return nil
	}
	if err := verify(); err != nil {
		return err
	}
	if boundary != nil && boundary.linkPublicationHook != nil {
		boundary.linkPublicationHook(dst)
	}
	fresh, err := capturePortableParent(dst, boundary, false)
	if err != nil {
		return fmt.Errorf("%w: portable prune parent unavailable: %w", fsboundary.ErrGenerationConflict, err)
	}
	unchanged := fresh != nil && fresh.Identity().SameObject(parent.Identity())
	if fresh != nil {
		if closeErr := fresh.Close(); closeErr != nil {
			return closeErr
		}
	}
	if !unchanged {
		return fmt.Errorf("%w: portable prune parent changed", fsboundary.ErrGenerationConflict)
	}
	if err := verify(); err != nil {
		return err
	}
	if observed == nil || dryRun {
		return nil
	}
	return native.RemoveAll(name)
}

func removeEmptyPortableDirectory(rootPath, directoryPath string, boundary *destinationBoundary) (err error) {
	if rootPath == "" || directoryPath == "" {
		return fmt.Errorf("%w: empty portable cleanup path", fsboundary.ErrUnsafePath)
	}
	// Keep the lexical root: root.Path() may have a different alias topology.
	rootPath, err = filepath.Abs(rootPath)
	if err != nil {
		return err
	}
	directoryPath, err = filepath.Abs(directoryPath)
	if err != nil {
		return err
	}
	var root *fsboundary.Dir
	if boundary != nil {
		root, err = boundary.captureDirectory(rootPath, false)
	} else {
		root, err = fsboundary.OpenDir(rootPath)
	}
	if err != nil {
		return err
	}
	if root == nil {
		return fs.ErrNotExist
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	rel, err := filepath.Rel(rootPath, directoryPath)
	if err != nil {
		return err
	}
	parent, err := root.OpenPath(filepath.Dir(rel))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := native.Lstat(filepath.Base(rel))
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fsboundary.ErrUnsafePath
	}
	return parent.Rmdir(filepath.Base(rel))
}
