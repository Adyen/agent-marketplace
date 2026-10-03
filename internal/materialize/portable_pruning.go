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

func removePortableOutput(dst string, observed os.FileInfo, wantDigest string, boundary *destinationBoundary) (err error) {
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
			if !strings.HasPrefix(text, cache.CacheDir()+string(os.PathSeparator)) {
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
	if observed == nil {
		return nil
	}
	return removeVerifiedPortableOutput(parent, native, name, observed, wantDigest)
}

func removeEmptyPortableDirectory(rootPath, directoryPath string, boundary *destinationBoundary) (err error) {
	if rootPath == "" || directoryPath == "" {
		return fmt.Errorf("%w: empty portable cleanup path", fsboundary.ErrUnsafePath)
	}
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
