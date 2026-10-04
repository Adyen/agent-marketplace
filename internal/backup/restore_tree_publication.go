package backup

import (
	"errors"
	"io/fs"
	"os"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func restoreCapturedSymlink(parent *fsboundary.Dir, name, target string) (err error) {
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := native.Lstat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err == nil {
		identity, identityErr := fsboundary.FileInfoIdentity(info)
		if identityErr != nil {
			return identityErr
		}
		if info.Mode().IsRegular() {
			opened, observed, openErr := parent.OpenFile(name)
			if openErr != nil {
				return openErr
			}
			defer func() {
				if closeErr := opened.Close(); closeErr != nil {
					err = errors.Join(err, closeErr)
				}
			}()
			if !observed.SameState(identity) {
				return fsboundary.ErrGenerationConflict
			}
		} else if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return fsboundary.ErrUnsafeFile
		}
	}
	if err := native.RemoveAll(name); err != nil {
		return err
	}
	return native.Symlink(target, name)
}
