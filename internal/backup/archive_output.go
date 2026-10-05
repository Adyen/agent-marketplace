package backup

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func withArchiveOutput(path string, write func(*os.File) error) (err error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := fsboundary.OpenDir(filepath.Dir(absolute))
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
	name := filepath.Base(absolute)
	file, err := native.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	var created os.FileInfo
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, closeErr)
			} else {
				err = closeErr
			}
		}
		if err != nil {
			current, statErr := native.Lstat(name)
			if statErr == nil && created != nil && os.SameFile(created, current) {
				if removeErr := native.Remove(name); removeErr != nil {
					err = errors.Join(err, removeErr)
				}
			} else if statErr != nil && !os.IsNotExist(statErr) {
				err = errors.Join(err, statErr)
			}
		}
	}()
	created, err = file.Stat()
	if err != nil {
		return err
	}
	if err := write(file); err != nil {
		return err
	}
	written, err := file.Stat()
	if err != nil {
		return err
	}
	identity, err := fsboundary.FileInfoIdentity(written)
	if err != nil {
		return err
	}
	if !identity.Mode.IsRegular() || identity.Links != 1 {
		return fsboundary.ErrUnsafeFile
	}
	current, err := native.Lstat(name)
	if err != nil {
		return errors.Join(fsboundary.ErrGenerationConflict, err)
	}
	currentID, err := fsboundary.FileInfoIdentity(current)
	if err != nil {
		return err
	}
	if !identity.SameState(currentID) {
		return fsboundary.ErrGenerationConflict
	}
	fresh, err := fsboundary.OpenDir(filepath.Dir(absolute))
	if err != nil {
		return errors.Join(fsboundary.ErrGenerationConflict, err)
	}
	unchanged := fresh.Identity().SameObject(parent.Identity())
	if closeErr := fresh.Close(); closeErr != nil {
		return closeErr
	}
	if !unchanged {
		return fsboundary.ErrGenerationConflict
	}
	return nil
}
