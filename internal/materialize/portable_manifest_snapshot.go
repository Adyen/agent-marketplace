package materialize

import (
	"errors"
	"fmt"
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
		return nil, err
	}
	defer func() { err = errors.Join(err, scope.Close()) }()
	parent, err := scope.OpenRoot(filepath.Base(filepath.Dir(path)))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	file, admitted, err := parent.OpenFile(filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	data, snapshot, err := fsboundary.ReadFile(file, admitted.Size)
	if err != nil {
		return nil, err
	}
	if !snapshot.SameState(admitted) {
		return nil, fmt.Errorf("%w: materialization manifest changed after admission", fsboundary.ErrGenerationConflict)
	}
	return data, nil
}
