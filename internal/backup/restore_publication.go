package backup

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func publishRestoredFile(parent *fsboundary.Dir, name string, reader io.Reader, mode os.FileMode) (err error) {
	file, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if file.Link() != nil {
		return fsboundary.ErrUnsafeFile
	}
	staged, stageName, err := parent.CreateTemp(".agent-marketplace-restore-*")
	if err != nil {
		return err
	}
	created, err := staged.Stat()
	if err != nil {
		return errors.Join(err, staged.Close())
	}
	defer func() {
		native, openErr := parent.NativeRoot()
		if openErr != nil {
			err = errors.Join(err, openErr)
			return
		}
		if current, statErr := native.Lstat(stageName); statErr == nil && os.SameFile(created, current) {
			if removeErr := parent.Unlink(stageName); removeErr != nil {
				err = errors.Join(err, removeErr)
			}
		}
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if _, err := io.Copy(staged, reader); err != nil {
		return errors.Join(err, staged.Close())
	}
	if err := staged.Chmod(mode); err != nil {
		return errors.Join(err, staged.Close())
	}
	info, err := staged.Stat()
	if err != nil {
		return errors.Join(err, staged.Close())
	}
	identity, err := fsboundary.FileInfoIdentity(info)
	if closeErr := staged.Close(); err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	snapshot, err := fsboundary.CaptureDestination(parent, stageName, fsboundary.LinkPolicyRepository)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := snapshot.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if snapshot.Link() != nil || !snapshot.Identity().SameState(identity) {
		return fsboundary.ErrGenerationConflict
	}
	if err := snapshot.ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
		return err
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
	return native.Rename(stageName, name)
}
