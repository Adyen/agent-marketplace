package ownership

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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
	defer func() {
		if err == nil {
			return
		}
		data = nil
		switch {
		case errors.Is(err, fsboundary.ErrUnsafeFile), errors.Is(err, fsboundary.ErrUnsafePath):
			err = errors.Join(ErrUnsafeAggregateFile, err)
		case errors.Is(err, fsboundary.ErrGenerationConflict):
			err = errors.Join(ErrGenerationConflict, err)
		}
	}()
	if !fs.ValidPath(rel) || rel == "." {
		return nil, fmt.Errorf("%w: aggregate path must remain relative", ErrUnsafeAggregateFile)
	}
	anchor, err := fsboundary.OpenDir(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := anchor.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	rootRel, err := filepath.Rel(anchor.Path(), s.canonicalRoot)
	if err != nil {
		return nil, err
	}
	root, err := anchor.OpenPath(rootRel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
			return nil, &os.PathError{Op: "read aggregate", Path: filepath.Join(s.canonicalRoot, rel), Err: fs.ErrNotExist}
		}
		return nil, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if err := s.bindRootIdentity(root.Identity()); err != nil {
		return nil, err
	}
	file, err := fsboundary.CaptureDestination(root, filepath.FromSlash(rel), fsboundary.LinkPolicyRepository)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile) && !errors.Is(err, fsboundary.ErrGenerationConflict) {
			return nil, &os.PathError{Op: "read aggregate", Path: filepath.Join(s.canonicalRoot, rel), Err: fs.ErrNotExist}
		}
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return nil, ErrUnsafeAggregateFile
	}
	if !file.Exists() {
		return nil, &os.PathError{Op: "read aggregate", Path: filepath.Join(s.canonicalRoot, rel), Err: fs.ErrNotExist}
	}
	data, _, err = file.ReadTarget(file.Identity().Size)
	return data, err
}
