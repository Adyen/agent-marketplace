package ownership

import (
	"errors"
	"log/slog"
	"path/filepath"
	"syscall"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type safetyAttempt struct {
	relative string
	parent   fsboundary.Identity
	members  []safetyLeafRemoval
}

func (s *Store) removeSafetyAttempt(attempt *safetyAttempt) (err error) {
	if attempt == nil {
		return fsboundary.ErrUnsafePath
	}
	parent, err := s.captureStateDirectory(attempt.relative)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if !parent.Identity().SameObject(attempt.parent) {
		return fsboundary.ErrGenerationConflict
	}
	for _, member := range attempt.members {
		pin, exists, err := captureSafetyPayload(parent, member.name)
		if err != nil {
			return err
		}
		if exists != member.exists || (exists && !pin.file.SameState(member.identity)) {
			return fsboundary.ErrGenerationConflict
		}
	}
	for _, member := range attempt.members {
		if err := s.removeSafetyLeaf(parent, attempt.relative, member); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) removeEmptySafetyDirectory(attempt *safetyAttempt) (err error) {
	if attempt == nil {
		return fsboundary.ErrUnsafePath
	}
	parent, err := s.captureStateDirectory(filepath.Dir(attempt.relative))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	verify := func() error {
		child, err := parent.OpenPath(filepath.Base(attempt.relative))
		if err != nil {
			return err
		}
		unchanged := child.Identity().SameObject(attempt.parent)
		if closeErr := child.Close(); closeErr != nil {
			return closeErr
		}
		if !unchanged {
			return fsboundary.ErrGenerationConflict
		}
		return s.verifyStateDirectory(parent, filepath.Dir(attempt.relative))
	}
	if err := verify(); err != nil {
		return err
	}
	slog.Debug("Empty ownership safety directory selected for cleanup", "path", filepath.Join(parent.Path(), filepath.Base(attempt.relative)), "phase", "ownership-empty-safety-cleanup")
	if err := verify(); err != nil {
		return err
	}
	if err := parent.Rmdir(filepath.Base(attempt.relative)); err != nil && !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
		return err
	}
	return nil
}

type safetyLeafRemoval struct {
	name     string
	identity fsboundary.Identity
	exists   bool
}

func (s *Store) removeSafetyLeaf(parent *fsboundary.Dir, relative string, removal safetyLeafRemoval) (err error) {
	if err := s.verifyStateDirectory(parent, relative); err != nil {
		return err
	}
	file, err := fsboundary.CaptureDestination(parent, removal.name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil || file.Exists() != removal.exists || (removal.exists && !file.Identity().SameState(removal.identity)) {
		return fsboundary.ErrGenerationConflict
	}
	return file.RemoveLeaf()
}
