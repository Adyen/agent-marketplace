package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (s *portableState) captureManifestParent(create bool) (parent *fsboundary.Dir, err error) {
	anchor, err := filepath.Abs(filepath.Dir(filepath.Dir(s.path)))
	if err != nil {
		return nil, err
	}
	scope, err := fsboundary.OpenDir(anchor)
	if err != nil {
		return nil, fmt.Errorf("capture manifest scope: %w", err)
	}
	defer func() {
		if closeErr := scope.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			if parent != nil {
				if parentCloseErr := parent.Close(); parentCloseErr != nil {
					err = errors.Join(err, parentCloseErr)
				}
				parent = nil
			}
		}
	}()
	if s.pin.scope != (fsboundary.Identity{}) && !scope.Identity().SameObject(s.pin.scope) {
		return nil, fmt.Errorf("%w: manifest scope replaced", fsboundary.ErrGenerationConflict)
	}
	name := filepath.Base(filepath.Dir(s.path))
	if create && s.pin.parent == (fsboundary.Identity{}) {
		parent, err = scope.EnsureRoot(name)
	} else {
		parent, err = scope.OpenRoot(name)
	}
	if err != nil {
		return nil, fmt.Errorf("capture manifest parent: %w", err)
	}
	if s.pin.parent != (fsboundary.Identity{}) && !parent.Identity().SameObject(s.pin.parent) {
		_ = parent.Close()
		parent = nil
		return nil, fmt.Errorf("%w: manifest parent replaced", fsboundary.ErrGenerationConflict)
	}
	return parent, nil
}

func (s *portableState) captureManifestDestination(parent *fsboundary.Dir) (*fsboundary.Destination, error) {
	dest, err := fsboundary.CaptureDestination(parent, filepath.Base(s.path), fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, err
	}
	if dest.Link() != nil {
		_ = dest.Close()
		return nil, fmt.Errorf("%w: manifest leaf is a link", fsboundary.ErrUnsafePath)
	}
	if dest.Exists() != (s.pin.data != nil) || dest.Exists() && !dest.Identity().SameState(s.pin.file) {
		_ = dest.Close()
		return nil, fmt.Errorf("%w: manifest changed since load", fsboundary.ErrGenerationConflict)
	}
	if dest.Exists() {
		current, _, err := dest.ReadTarget(s.pin.file.Size)
		if err != nil {
			_ = dest.Close()
			return nil, err
		}
		if !bytes.Equal(current, s.pin.data) {
			_ = dest.Close()
			return nil, fmt.Errorf("%w: manifest content changed since load", fsboundary.ErrGenerationConflict)
		}
	}
	return dest, nil
}

func (s *portableState) removeManifest() (err error) {
	if s.pin.data == nil && s.pin.parent == (fsboundary.Identity{}) {
		return nil
	}
	parent, err := s.captureManifestParent(false)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	dest, err := s.captureManifestDestination(parent)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dest.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if err := s.verifyManifestParent(parent); err != nil {
		return err
	}
	if s.pin.data == nil {
		return nil
	}
	return dest.RemoveLeaf()
}

func (s *portableState) verifyManifestParent(parent *fsboundary.Dir) error {
	fresh, err := s.captureManifestParent(false)
	if err != nil {
		return err
	}
	unchanged := fresh.Identity().SameObject(parent.Identity())
	if err := fresh.Close(); err != nil {
		return err
	}
	if !unchanged {
		return fmt.Errorf("%w: staged manifest parent replaced", fsboundary.ErrGenerationConflict)
	}
	return nil
}

func (s *portableState) publishManifest(data []byte) (err error) {
	parent, err := s.captureManifestParent(true)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	dest, err := s.captureManifestDestination(parent)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dest.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if bytes.Equal(data, s.pin.data) {
		return s.verifyManifestParent(parent)
	}
	staged, name, err := parent.CreateTemp(".materialization-manifest-*")
	if err != nil {
		return err
	}
	defer func() {
		if staged != nil {
			if closeErr := staged.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		if cleanupErr := parent.Unlink(name); cleanupErr != nil && !errors.Is(cleanupErr, fs.ErrNotExist) {
			err = errors.Join(err, cleanupErr)
		}
	}()
	mode := fs.FileMode(0o644)
	if dest.Exists() {
		mode = s.pin.file.Mode.Perm()
	}
	if err := staged.Chmod(mode); err != nil {
		return err
	}
	if _, err := staged.Write(data); err != nil {
		return err
	}
	closeErr := staged.Close()
	staged = nil
	if closeErr != nil {
		return closeErr
	}
	if s.publicationHook != nil {
		s.publicationHook(s.path)
	}
	if err := s.verifyManifestParent(parent); err != nil {
		return err
	}
	return dest.Publish(parent, name)
}
