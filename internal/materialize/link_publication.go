package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func publishCacheLink(path, target string, validated os.FileInfo, boundary *destinationBoundary) error {
	parentPath, name := filepath.Dir(path), filepath.Base(path)
	capture := func(create bool) (*fsboundary.Dir, error) {
		if boundary != nil {
			return boundary.captureDirectory(parentPath, create)
		}
		if create {
			if err := os.MkdirAll(parentPath, 0o755); err != nil {
				return nil, err
			}
		}
		absolute, err := filepath.Abs(parentPath)
		if err != nil {
			return nil, err
		}
		return fsboundary.OpenDir(absolute)
	}
	parent, err := capture(true)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	root, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var expected fsboundary.Identity
	if validated != nil {
		expected, err = fsboundary.FileInfoIdentity(validated)
		if err != nil {
			return err
		}
		if !validated.Mode().IsRegular() && !validated.IsDir() && validated.Mode()&os.ModeSymlink == 0 || validated.Mode().IsRegular() && expected.Links != 1 {
			return fmt.Errorf("%w: unsupported cache-link destination shape", fsboundary.ErrUnsafeFile)
		}
	}
	verify := func() error {
		current, err := root.Lstat(name)
		if validated == nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: cache-link destination appeared", fsboundary.ErrGenerationConflict)
		}
		if err != nil {
			return fmt.Errorf("%w: cache-link destination unavailable: %w", fsboundary.ErrGenerationConflict, err)
		}
		identity, err := fsboundary.FileInfoIdentity(current)
		if err != nil {
			return err
		}
		if !identity.SameState(expected) {
			return fmt.Errorf("%w: cache-link destination changed", fsboundary.ErrGenerationConflict)
		}
		return nil
	}
	if err := verify(); err != nil {
		return err
	}
	file, staged, err := parent.CreateTemp(".agent-marketplace-link-*")
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		_ = root.Remove(staged)
		return err
	}
	defer func() { _ = root.Remove(staged) }()
	if err := root.Remove(staged); err != nil {
		return err
	}
	if err := root.Symlink(target, staged); err != nil {
		return err
	}
	if boundary != nil && boundary.linkPublicationHook != nil {
		boundary.linkPublicationHook(path)
	}
	fresh, err := capture(false)
	if err != nil {
		return fmt.Errorf("%w: cache-link parent unavailable: %w", fsboundary.ErrGenerationConflict, err)
	}
	if fresh == nil {
		return fmt.Errorf("%w: cache-link parent disappeared", fsboundary.ErrGenerationConflict)
	}
	unchanged := fresh.Identity().SameObject(parent.Identity())
	_ = fresh.Close()
	if !unchanged {
		return fmt.Errorf("%w: cache-link parent replaced", fsboundary.ErrGenerationConflict)
	}
	if err := verify(); err != nil {
		return err
	}
	if validated != nil && validated.IsDir() && validated.Mode()&os.ModeSymlink == 0 {
		if err := root.RemoveAll(name); err != nil {
			return err
		}
	}
	if err := root.Rename(staged, name); err != nil {
		return err
	}
	return nil
}
