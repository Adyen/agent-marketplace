package ownership

import (
	"errors"
	"io/fs"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type metadataPin struct {
	parent fsboundary.Identity
	file   fsboundary.Identity
}

func (s *Store) readMetadata(name string) ([]byte, bool, error) {
	data, exists, _, err := s.readObservedMetadata(name)
	return data, exists, err
}

func (s *Store) readObservedMetadata(name string) (data []byte, exists bool, pin metadataPin, err error) {
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
	file, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, false, pin, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return nil, false, pin, fsboundary.ErrUnsafeFile
	}
	if !file.Exists() {
		return nil, false, pin, nil
	}
	data, pin.file, err = file.ReadTarget(file.Identity().Size)
	if err != nil {
		return nil, false, pin, err
	}
	if err := s.verifyMetadataParent(parent); err != nil {
		return nil, false, pin, err
	}
	pin.parent = parent.Identity()
	return data, true, pin, nil
}
