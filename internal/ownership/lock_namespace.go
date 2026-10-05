package ownership

import (
	"errors"
	"io/fs"
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

// openLockFile captures the selected lock directory once and opens the lock
// leaf below it. On success the caller owns both descriptors: the captured
// parent is reused for every named-leaf validation of this acquisition instead
// of re-walking the lock root, and must be closed when acquisition ends.
func (s *Store) openLockFile() (file *os.File, parent *fsboundary.Dir, err error) {
	if err := s.verifySelectedLockRoot(); err != nil {
		return nil, nil, err
	}
	// The lock directory exists after the first acquisition, so capture it
	// directly and only walk-and-create when it is genuinely absent.
	parent, err = captureAbsoluteDirectory(s.canonicalLockRoot)
	if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
		if err := preparePrivateDirectory(s.canonicalLockRoot); err != nil {
			return nil, nil, err
		}
		parent, err = captureAbsoluteDirectory(s.canonicalLockRoot)
	}
	if err != nil {
		return nil, nil, err
	}
	if err := s.verifySelectedLockRoot(); err != nil {
		return nil, nil, closeLockParent(parent, err)
	}
	file, err = parent.OpenPersistentFile(filepath.Base(s.LockPath()), 0o644)
	if err != nil {
		return nil, nil, closeLockParent(parent, err)
	}
	return file, parent, nil
}

func closeLockParent(parent *fsboundary.Dir, err error) error {
	if closeErr := parent.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

// validateLockAuthority proves the held descriptor is still the regular,
// singly linked leaf named below the captured lock directory, and that the
// selected lock root still resolves to that directory.
func (s *Store) validateLockAuthority(file *os.File, parent *fsboundary.Dir) (err error) {
	if err := validateLockFile(file); err != nil {
		return err
	}
	if err := s.verifySelectedLockRoot(); err != nil {
		return err
	}
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
