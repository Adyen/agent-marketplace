package materialize

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func publishPortableFile(src, dst string, observed os.FileInfo, boundary *destinationBoundary) error {
	source, err := fsboundary.OpenSourceFS(filepath.Dir(src))
	if err != nil {
		return err
	}
	file, err := source.Open(filepath.Base(src))
	if err != nil {
		return errors.Join(err, source.Close())
	}
	info, statErr := file.Stat()
	data, readErr := io.ReadAll(file)
	closeErr := errors.Join(file.Close(), source.Close())
	if statErr != nil || readErr != nil || closeErr != nil {
		return errors.Join(statErr, readErr, closeErr)
	}
	return publishPortableBytes(dst, data, info.Mode().Perm(), observed, boundary)
}

func publishPortableBytes(dst string, data []byte, mode os.FileMode, observed os.FileInfo, boundary *destinationBoundary) (err error) {
	capture := func(create bool) (*fsboundary.Dir, error) { return capturePortableParent(dst, boundary, create) }
	parent, err := capture(true)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	name := filepath.Base(dst)
	var expected fsboundary.Identity
	if observed != nil {
		expected, err = fsboundary.FileInfoIdentity(observed)
		if err != nil {
			return err
		}
	}
	verify := func() error {
		current, err := native.Lstat(name)
		if observed == nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: portable destination appeared", fsboundary.ErrGenerationConflict)
		}
		if err != nil {
			return fmt.Errorf("%w: portable destination unavailable: %w", fsboundary.ErrGenerationConflict, err)
		}
		identity, err := fsboundary.FileInfoIdentity(current)
		if err != nil {
			return err
		}
		if !identity.SameState(expected) {
			return fmt.Errorf("%w: portable destination changed", fsboundary.ErrGenerationConflict)
		}
		if identity.Mode.IsRegular() && identity.Links != 1 || !identity.Mode.IsRegular() && identity.Mode&os.ModeSymlink == 0 {
			return fsboundary.ErrUnsafeFile
		}
		return nil
	}
	if err := verify(); err != nil {
		return err
	}
	if observed != nil && observed.Mode().IsRegular() {
		dest, captureErr := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
		if captureErr != nil {
			return captureErr
		}
		defer func() {
			if closeErr := dest.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
		if dest.Link() != nil || !dest.Identity().SameState(expected) {
			return fmt.Errorf("%w: portable opened destination changed", fsboundary.ErrGenerationConflict)
		}
		if err := dest.ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
			return err
		}
	}
	stagedFile, stagedName, err := parent.CreateTemp(".agent-marketplace-portable-*")
	if err != nil {
		return err
	}
	created, err := stagedFile.Stat()
	if err != nil {
		return errors.Join(err, stagedFile.Close())
	}
	defer func() {
		if current, statErr := native.Lstat(stagedName); statErr == nil && os.SameFile(created, current) {
			_ = parent.Unlink(stagedName)
		}
	}()
	if err := stagedFile.Chmod(mode.Perm()); err != nil {
		return errors.Join(err, stagedFile.Close())
	}
	if _, err := stagedFile.Write(data); err != nil {
		return errors.Join(err, stagedFile.Close())
	}
	stagedInfo, statErr := stagedFile.Stat()
	if closeErr := stagedFile.Close(); statErr != nil || closeErr != nil {
		return errors.Join(statErr, closeErr)
	}
	stagedID, err := fsboundary.FileInfoIdentity(stagedInfo)
	if err != nil {
		return err
	}
	if boundary != nil && boundary.linkPublicationHook != nil {
		boundary.linkPublicationHook(dst)
	}
	fresh, err := capture(false)
	if err != nil {
		return fmt.Errorf("%w: portable parent unavailable: %w", fsboundary.ErrGenerationConflict, err)
	}
	unchanged := fresh != nil && fresh.Identity().SameObject(parent.Identity())
	if fresh != nil {
		if closeErr := fresh.Close(); closeErr != nil {
			return closeErr
		}
	}
	if !unchanged {
		return fmt.Errorf("%w: portable parent replaced", fsboundary.ErrGenerationConflict)
	}
	if err := verify(); err != nil {
		return err
	}
	stagedInfo, err = native.Lstat(stagedName)
	if err != nil {
		return err
	}
	currentStage, err := fsboundary.FileInfoIdentity(stagedInfo)
	if err != nil {
		return err
	}
	if !currentStage.SameState(stagedID) {
		return fmt.Errorf("%w: staged portable file changed", fsboundary.ErrGenerationConflict)
	}
	return native.Rename(stagedName, name)
}
