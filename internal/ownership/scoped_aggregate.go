package ownership

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (s *Store) readAbsoluteAggregate(path string) ([]byte, error) {
	parent, err := CanonicalRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(s.canonicalRoot, filepath.Join(parent, filepath.Base(path)))
	if err != nil {
		return nil, err
	}
	return s.ReadAggregate(filepath.ToSlash(rel))
}

// bindRootIdentity requires a captured canonical root to be the directory
// object admitted for this Store. A root that did not exist at Open is bound
// on its first capture; any later replacement refuses as a generation conflict.
func (s *Store) bindRootIdentity(captured fsboundary.Identity) error {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	if !s.rootBound {
		s.rootIdentity, s.rootBound = captured, true
		return nil
	}
	if !s.rootIdentity.SameObject(captured) {
		return fmt.Errorf("%w: canonical root %s replaced since admission", fsboundary.ErrGenerationConflict, s.canonicalRoot)
	}
	return nil
}

func (s *Store) ReadAggregate(rel string) (data []byte, err error) {
	data, _, _, err = s.ReadAggregateSnapshot(rel)
	return data, err
}

func (s *Store) ReadAggregateSnapshot(rel string) (data []byte, generation dependencytxn.Generation, mode fs.FileMode, err error) {
	defer func() {
		if err == nil {
			return
		}
		data = nil
		generation = dependencytxn.Generation{}
		mode = 0
		switch {
		case errors.Is(err, fsboundary.ErrUnsafeFile), errors.Is(err, fsboundary.ErrUnsafePath):
			err = errors.Join(ErrUnsafeAggregateFile, err)
		case errors.Is(err, fsboundary.ErrGenerationConflict):
			err = errors.Join(ErrGenerationConflict, err)
		}
	}()
	if !fs.ValidPath(rel) || rel == "." {
		return nil, dependencytxn.Generation{}, 0, fmt.Errorf("%w: aggregate path must remain relative", ErrUnsafeAggregateFile)
	}
	anchor, err := fsboundary.OpenDir(string(filepath.Separator))
	if err != nil {
		return nil, dependencytxn.Generation{}, 0, err
	}
	defer func() {
		if closeErr := anchor.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	rootRel, err := filepath.Rel(anchor.Path(), s.canonicalRoot)
	if err != nil {
		return nil, dependencytxn.Generation{}, 0, err
	}
	root, err := anchor.OpenPath(rootRel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return nil, dependencytxn.Generation{}, 0, &os.PathError{Op: "read aggregate", Path: filepath.Join(s.canonicalRoot, rel), Err: fs.ErrNotExist}
		}
		return nil, dependencytxn.Generation{}, 0, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if err := s.bindRootIdentity(root.Identity()); err != nil {
		return nil, dependencytxn.Generation{}, 0, err
	}
	file, err := fsboundary.CaptureDestination(root, filepath.FromSlash(rel), fsboundary.LinkPolicyRepository)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile) && !errors.Is(err, fsboundary.ErrGenerationConflict) {
			return nil, dependencytxn.Generation{}, 0, &os.PathError{Op: "read aggregate", Path: filepath.Join(s.canonicalRoot, rel), Err: fs.ErrNotExist}
		}
		return nil, dependencytxn.Generation{}, 0, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return nil, dependencytxn.Generation{}, 0, ErrUnsafeAggregateFile
	}
	if !file.Exists() {
		return nil, dependencytxn.Generation{}, 0, &os.PathError{Op: "read aggregate", Path: filepath.Join(s.canonicalRoot, rel), Err: fs.ErrNotExist}
	}
	data, identity, err := file.ReadTarget(file.Identity().Size)
	if err != nil {
		return nil, dependencytxn.Generation{}, 0, err
	}
	return data, dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(data)}, identity.Mode.Perm(), nil
}
