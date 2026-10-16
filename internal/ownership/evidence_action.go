package ownership

import (
	"errors"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (c OrphanCandidate) ValidateEvidence() (err error) {
	evidence := c.evidence
	if evidence.store == nil {
		return fsboundary.ErrUnsafePath
	}
	parent, err := evidence.store.captureStateDirectory(evidence.directory)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	_, exists, current, err := readObservedMetadataFile(parent, evidence.metadataName)
	if err != nil {
		return err
	}
	if !exists || !current.parent.SameObject(evidence.observed.parent) || !current.file.SameState(evidence.observed.file) {
		return fsboundary.ErrGenerationConflict
	}
	return evidence.store.verifyStateDirectory(parent, evidence.directory)
}
