package backup

import (
	"errors"
	"fmt"
	"io"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func removeCapturedBackups(parent *fsboundary.Dir, path string, names []string) error {
	identities := make([]fsboundary.Identity, len(names))
	for i, name := range names {
		identity, err := inspectRetentionMember(parent, name, nil)
		if err != nil {
			return err
		}
		identities[i] = identity
	}
	for i, name := range names {
		if err := verifyArchiveParent(parent, path); err != nil {
			return err
		}
		if _, err := inspectRetentionMember(parent, name, &identities[i]); err != nil {
			return fmt.Errorf("backup: retention member %s in %s: %w", name, path, err)
		}
		if err := parent.Unlink(name); err != nil {
			return err
		}
	}
	return nil
}

func inspectRetentionMember(parent *fsboundary.Dir, name string, expected *fsboundary.Identity) (identity fsboundary.Identity, err error) {
	file, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return identity, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return identity, fsboundary.ErrUnsafeFile
	}
	if !file.Exists() {
		return identity, fsboundary.ErrGenerationConflict
	}
	identity = file.Identity()
	if expected != nil && !identity.SameState(*expected) {
		return identity, fsboundary.ErrGenerationConflict
	}
	err = file.ReadTargetWith(func(io.Reader) error { return nil })
	return identity, err
}
