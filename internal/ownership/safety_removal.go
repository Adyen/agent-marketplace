package ownership

import (
	"errors"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type safetyLeafRemoval struct {
	name     string
	identity fsboundary.Identity
	exists   bool
}

func (s *Store) removeSafetyLeaf(parent *fsboundary.Dir, relative string, removal safetyLeafRemoval) (err error) {
	if err := s.verifyStateDirectory(parent, relative); err != nil {
		return err
	}
	file, err := fsboundary.CaptureDestination(parent, removal.name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil || file.Exists() != removal.exists || (removal.exists && !file.Identity().SameState(removal.identity)) {
		return fsboundary.ErrGenerationConflict
	}
	return file.RemoveLeaf()
}
