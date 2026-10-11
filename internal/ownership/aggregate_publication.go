package ownership

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (s *Store) captureAggregateParent(path string) (parent *fsboundary.Dir, err error) {
	canonical, err := CanonicalRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(s.canonicalRoot, canonical)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("%w: aggregate parent outside canonical root", ErrUnsafeAggregateFile)
	}
	anchor, err := fsboundary.OpenDir(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := anchor.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			if parent != nil {
				_ = parent.Close()
				parent = nil
			}
		}
	}()
	rel, err = filepath.Rel(anchor.Path(), canonical)
	if err != nil {
		return nil, err
	}
	return anchor.OpenPath(rel)
}
