package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type portableManifestSnapshot struct {
	data   []byte
	file   fsboundary.Identity
	scope  fsboundary.Identity
	parent fsboundary.Identity
}

func portableManifestSnapshotMatches(pinned, current portableManifestSnapshot) bool {
	zero := fsboundary.Identity{}
	if pinned.scope != zero && !current.scope.SameObject(pinned.scope) ||
		pinned.parent != zero && !current.parent.SameObject(pinned.parent) {
		return false
	}
	if pinned.data == nil {
		return current.data == nil && current.file == zero
	}
	return current.data != nil && bytes.Equal(current.data, pinned.data) && current.file.SameState(pinned.file)
}

func readPortableManifestSnapshot(path string) (snapshot portableManifestSnapshot, err error) {
	anchor, err := filepath.Abs(filepath.Dir(filepath.Dir(path)))
	if err != nil {
		return snapshot, err
	}
	scope, err := fsboundary.OpenDir(anchor)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			err = nil
		}
		return snapshot, err
	}
	snapshot.scope = scope.Identity()
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
		return snapshot, err
	}
	snapshot.parent = parent.Identity()
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
		return snapshot, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	data, identity, err := fsboundary.ReadFile(file, admitted.Size)
	if err != nil {
		return snapshot, err
	}
	if !identity.SameState(admitted) {
		return snapshot, fmt.Errorf("%w: materialization manifest changed after admission", fsboundary.ErrGenerationConflict)
	}
	snapshot.data, snapshot.file = data, identity
	return snapshot, nil
}
