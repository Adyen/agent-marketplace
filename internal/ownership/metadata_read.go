package ownership

import (
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (s *Store) readMetadata(name string) (data []byte, exists bool, err error) {
	anchor, err := fsboundary.OpenDir(string(filepath.Separator))
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if closeErr := anchor.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	rel, err := filepath.Rel(anchor.Path(), s.canonicalStateRoot)
	if err != nil {
		return nil, false, err
	}
	root, err := anchor.OpenPath(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	file, err := fsboundary.CaptureDestination(root, filepath.Join(ownershipDirName, s.rootKey, name), fsboundary.LinkPolicyRepository)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile) && !errors.Is(err, fsboundary.ErrGenerationConflict) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return nil, false, fsboundary.ErrUnsafeFile
	}
	if !file.Exists() {
		return nil, false, nil
	}
	data, _, err = file.ReadTarget(file.Identity().Size)
	return data, true, err
}
