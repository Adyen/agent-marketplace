package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func readPortableManifestSnapshot(path string) (data []byte, err error) {
	anchor, err := filepath.Abs(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		return nil, err
	}
	scope, err := fsboundary.OpenDir(anchor)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			err = nil
		}
		return nil, err
	}
	defer func() {
		if closeErr := scope.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	parent, err := scope.OpenRoot(filepath.Base(filepath.Dir(path)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			err = nil
		}
		return nil, err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	file, admitted, err := parent.OpenFile(filepath.Base(path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	data, snapshot, err := fsboundary.ReadFile(file, admitted.Size)
	if err != nil {
		return nil, err
	}
	if !snapshot.SameState(admitted) {
		return nil, fmt.Errorf("%w: materialization manifest changed after admission", fsboundary.ErrGenerationConflict)
	}
	return data, nil
}
