package backup

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func publishRestoredFile(parent *fsboundary.Dir, name string, reader io.Reader, mode os.FileMode, validators ...func() error) (err error) {
	file, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, closeErr)
			} else {
				err = closeErr
			}
		}
	}()
	if file.Link() != nil {
		return fsboundary.ErrUnsafeFile
	}
	staged, stageName, err := parent.CreateTemp(".agent-marketplace-restore-*")
	if err != nil {
		return err
	}
	var created os.FileInfo
	stagedClosed := false
	defer func() {
		if !stagedClosed {
			if closeErr := staged.Close(); closeErr != nil {
				if err != nil {
					err = errors.Join(err, closeErr)
				} else {
					err = closeErr
				}
			}
		}
	}()
	defer func() {
		native, openErr := parent.NativeRoot()
		if openErr != nil {
			err = errors.Join(err, openErr)
			return
		}
		if current, statErr := native.Lstat(stageName); statErr == nil && created != nil && os.SameFile(created, current) {
			if removeErr := parent.Unlink(stageName); removeErr != nil {
				err = errors.Join(err, removeErr)
			}
		}
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	created, err = staged.Stat()
	if err != nil {
		return err
	}
	// The archived mode can revoke our own read access; apply it only after the
	// payload is written and the staged name is validated against the still-open
	// descriptor.
	if _, err := io.Copy(staged, reader); err != nil {
		return err
	}
	info, err := staged.Stat()
	if err != nil {
		return err
	}
	written, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return err
	}
	snapshot, err := fsboundary.CaptureDestination(parent, stageName, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := snapshot.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, closeErr)
			} else {
				err = closeErr
			}
		}
	}()
	if snapshot.Link() != nil || !snapshot.Identity().SameState(written) {
		return fsboundary.ErrGenerationConflict
	}
	if err := snapshot.ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
		return err
	}
	if err := staged.Chmod(mode); err != nil {
		return err
	}
	info, err = staged.Stat()
	if err != nil {
		return err
	}
	identity, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return err
	}
	if !identity.SameObject(written) || identity.Mode.Perm() != mode.Perm() {
		return fsboundary.ErrGenerationConflict
	}
	stagedClosed = true
	if closeErr := staged.Close(); closeErr != nil {
		return closeErr
	}
	if file.Exists() {
		if err := file.ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
			return err
		}
	}
	fresh, err := file.StagingDir()
	if err != nil {
		return err
	}
	unchanged := fresh.Identity().SameObject(parent.Identity())
	if closeErr := fresh.Close(); closeErr != nil {
		return closeErr
	}
	if !unchanged {
		return fsboundary.ErrGenerationConflict
	}
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if !file.Exists() {
		if _, statErr := native.Lstat(name); !errors.Is(statErr, fs.ErrNotExist) {
			if statErr != nil {
				return statErr
			}
			return fsboundary.ErrGenerationConflict
		}
	}
	for _, validate := range validators {
		if err := validate(); err != nil {
			return err
		}
	}
	slog.Debug("Backup restored payload ready for publication", "path", filepath.Join(parent.Path(), stageName), "destination", name, "phase", "restore-file-publication")
	current, statErr := native.Lstat(stageName)
	if statErr != nil {
		return errors.Join(fsboundary.ErrGenerationConflict, statErr)
	}
	named, identityErr := fsboundary.FileInfoIdentity(current)
	if identityErr != nil {
		return identityErr
	}
	if !named.SameState(identity) {
		return fsboundary.ErrGenerationConflict
	}
	return native.Rename(stageName, name)
}
