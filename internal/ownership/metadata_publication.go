package ownership

import (
	"errors"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (s *Store) captureMetadataParent() (*fsboundary.Dir, error) {
	return s.captureStateDirectory(filepath.Join(ownershipDirName, s.rootKey))
}

func (s *Store) captureStateDirectory(relative string) (parent *fsboundary.Dir, err error) {
	if !fs.ValidPath(filepath.ToSlash(relative)) || relative == "." {
		return nil, fsboundary.ErrUnsafePath
	}
	anchor, err := fsboundary.OpenDir(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := anchor.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			if parent != nil {
				_ = parent.Close()
				parent = nil
			}
		}
	}()
	rel, err := filepath.Rel(anchor.Path(), filepath.Join(s.canonicalStateRoot, relative))
	if err != nil {
		return nil, err
	}
	return anchor.OpenPath(rel)
}

func (s *Store) verifyMetadataParent(parent *fsboundary.Dir) error {
	return s.verifyStateDirectory(parent, filepath.Join(ownershipDirName, s.rootKey))
}

func (s *Store) verifyStateDirectory(parent *fsboundary.Dir, relative string) error {
	fresh, err := s.captureStateDirectory(relative)
	if err != nil {
		return err
	}
	unchanged := fresh.Identity().SameObject(parent.Identity())
	if err := fresh.Close(); err != nil {
		return err
	}
	if !unchanged {
		return fsboundary.ErrGenerationConflict
	}
	return nil
}

func (s *Store) writeMetadata(name string, data []byte) error {
	_, err := s.writeObservedMetadata(name, data)
	return err
}

func (s *Store) writeObservedMetadata(name string, data []byte) (pin metadataPin, err error) {
	parent, err := s.captureMetadataParent()
	if err != nil {
		return pin, err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	destination, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return pin, err
	}
	defer func() {
		if closeErr := destination.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if destination.Link() != nil {
		return pin, fsboundary.ErrUnsafeFile
	}
	native, err := parent.NativeRoot()
	if err != nil {
		return pin, err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	staged, stagedName, err := parent.CreateTemp(".tmp-" + name + "-*")
	if err != nil {
		return pin, err
	}
	info, err := staged.Stat()
	if err != nil {
		return pin, errors.Join(err, staged.Close())
	}
	created, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return pin, errors.Join(err, staged.Close())
	}
	defer func() {
		if staged != nil {
			if closeErr := staged.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		info, statErr := native.Lstat(stagedName)
		if errors.Is(statErr, fs.ErrNotExist) {
			return
		}
		if statErr != nil {
			err = errors.Join(err, statErr)
			return
		}
		identity, inspectErr := fsboundary.FileInfoIdentity(info)
		if inspectErr != nil {
			err = errors.Join(err, inspectErr)
			return
		}
		if created.SameObject(identity) {
			if removeErr := parent.Unlink(stagedName); removeErr != nil {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if _, err := staged.Write(data); err != nil {
		return pin, err
	}
	if err := staged.Chmod(0o600); err != nil {
		return pin, err
	}
	info, err = staged.Stat()
	if err != nil {
		return pin, err
	}
	produced, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return pin, err
	}
	closeErr := staged.Close()
	staged = nil
	if closeErr != nil {
		return pin, closeErr
	}
	if err := s.verifyMetadataParent(parent); err != nil {
		return pin, err
	}
	output, err := fsboundary.CaptureDestination(parent, stagedName, fsboundary.LinkPolicyRepository)
	if err != nil {
		return pin, err
	}
	defer func() {
		if closeErr := output.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if output.Link() != nil || !output.Identity().SameState(produced) {
		return pin, fsboundary.ErrGenerationConflict
	}
	if err := output.ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
		return pin, err
	}
	if err := destination.Publish(parent, stagedName); err != nil {
		return pin, err
	}
	info, err = native.Lstat(name)
	if err != nil {
		return pin, err
	}
	pin.file, err = fsboundary.FileInfoIdentity(info)
	if err != nil {
		return pin, err
	}
	if !pin.file.SameObject(produced) || !pin.file.Mode.IsRegular() || pin.file.Links != 1 {
		return pin, fsboundary.ErrGenerationConflict
	}
	pin.parent = parent.Identity()
	return pin, nil
}
