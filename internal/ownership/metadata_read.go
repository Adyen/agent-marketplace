package ownership

import (
	"errors"
	"io/fs"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

var errMetadataCleanup = errors.New("ownership: metadata cleanup failed")
var errMetadataRead = errors.New("ownership: metadata read refused")

type metadataPin struct {
	parent fsboundary.Identity
	file   fsboundary.Identity
}

func (s *Store) verifyOriginalRecord(pin metadataPin, existed bool) error {
	return s.verifyOriginalMetadata(stateFileName, pin, existed)
}

func (s *Store) verifyOriginalMetadata(name string, pin metadataPin, existed bool) error {
	_, exists, current, err := s.readObservedMetadata(name)
	if err != nil {
		return err
	}
	if existed != exists || (existed && !pin.file.SameState(current.file)) || (pin.parent.Inode != 0 && !pin.parent.SameObject(current.parent)) {
		return errors.Join(ErrGenerationConflict, fsboundary.ErrGenerationConflict)
	}
	return nil
}

func (s *Store) readObservedMetadata(name string) (data []byte, exists bool, pin metadataPin, err error) {
	defer func() {
		if err != nil {
			err = errors.Join(errMetadataRead, err)
		}
	}()
	parent, err := s.captureMetadataParent()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return nil, false, pin, nil
		}
		return nil, false, pin, err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	data, exists, pin, err = readObservedMetadataFile(parent, name)
	if err != nil || !exists {
		return data, exists, pin, err
	}
	if err := s.verifyMetadataParent(parent); err != nil {
		return nil, false, pin, err
	}
	return data, true, pin, nil
}

func readObservedMetadataFile(parent *fsboundary.Dir, name string) (data []byte, exists bool, pin metadataPin, err error) {
	file, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, false, pin, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, errMetadataCleanup, closeErr)
		}
	}()
	if file.Link() != nil {
		return nil, false, pin, fsboundary.ErrUnsafeFile
	}
	if !file.Exists() {
		pin.parent = parent.Identity()
		return nil, false, pin, nil
	}
	data, pin.file, err = file.ReadTarget(file.Identity().Size)
	if err != nil {
		return nil, false, pin, err
	}
	pin.parent = parent.Identity()
	return data, true, pin, nil
}
