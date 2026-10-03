package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func capturePruneDirectory(dir string, boundary *destinationBoundary) (root *fsboundary.Dir, err error) {
	capture := func() (*fsboundary.Dir, error) {
		return capturePortableParent(filepath.Join(dir, ".placeholder"), boundary, false)
	}
	root, err = capture()
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fs.ErrNotExist
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, root.Close())
			root = nil
		}
	}()
	if boundary != nil && boundary.linkPublicationHook != nil {
		boundary.linkPublicationHook(dir)
	}
	fresh, err := capture()
	if err != nil {
		return root, fmt.Errorf("%w: prune directory changed: %w", fsboundary.ErrGenerationConflict, err)
	}
	if fresh == nil {
		return root, fsboundary.ErrGenerationConflict
	}
	unchanged := fresh.Identity().SameObject(root.Identity())
	if err := fresh.Close(); err != nil {
		return root, err
	}
	if !unchanged {
		return root, fsboundary.ErrGenerationConflict
	}
	return root, nil
}

func readPruneDirectory(dir string, boundary *destinationBoundary) (entries []os.DirEntry, err error) {
	root, err := capturePruneDirectory(dir, boundary)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return readSupportSourceDirectory(root, ".")
}
