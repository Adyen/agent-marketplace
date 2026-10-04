package ownership

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (s *Store) verifySelectedLockRoot() error {
	if s.lockRootErr != nil {
		return s.lockRootErr
	}
	current, err := CanonicalRoot(filepath.Dir(s.LockPath()))
	if err != nil {
		return err
	}
	if current != s.canonicalLockRoot {
		return fsboundary.ErrGenerationConflict
	}
	return nil
}

func (s *Store) openLockFile() (file *os.File, err error) {
	if err := s.verifySelectedLockRoot(); err != nil {
		return nil, err
	}
	if err := preparePrivateDirectory(s.canonicalLockRoot); err != nil {
		return nil, err
	}
	parent, err := captureAbsoluteDirectory(s.canonicalLockRoot)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			if file != nil {
				err = errors.Join(err, file.Close())
				file = nil
			}
		}
	}()
	if err := s.verifySelectedLockRoot(); err != nil {
		return nil, err
	}
	return parent.OpenPersistentFile(filepath.Base(s.LockPath()), 0o644)
}

func (s *Store) validateLockAuthority(file *os.File) (err error) {
	if err := validateLockFile(file); err != nil {
		return err
	}
	if err := s.verifySelectedLockRoot(); err != nil {
		return err
	}
	parent, err := captureAbsoluteDirectory(s.canonicalLockRoot)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	named, observed, err := parent.OpenFile(filepath.Base(s.LockPath()))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := named.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	identity, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return err
	}
	if !observed.SameObject(identity) {
		return fsboundary.ErrGenerationConflict
	}
	return s.verifySelectedLockRoot()
}
