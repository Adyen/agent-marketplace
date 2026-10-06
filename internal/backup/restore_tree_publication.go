package backup

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

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
	var identity fsboundary.Identity
	if err == nil {
		var identityErr error
		identity, identityErr = fsboundary.FileInfoIdentity(info)
		if identityErr != nil {
			return identityErr
		}
		switch {
		case info.Mode().IsRegular():
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
		case info.IsDir(), info.Mode()&os.ModeSymlink != 0:
			// Directories and links at the leaf are replaced by the restored link.
		default:
			return fsboundary.ErrUnsafeFile
		}
	}
	slog.Debug("Backup symbolic destination admitted for replacement", "path", filepath.Join(parent.Path(), name), "phase", "restore-symbolic-replace")
	current, statErr := native.Lstat(name)
	if info == nil {
		if !errors.Is(statErr, fs.ErrNotExist) {
			return errors.Join(fsboundary.ErrGenerationConflict, statErr)
		}
	} else {
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
	}
	if err := native.RemoveAll(name); err != nil {
		return err
	}
	return native.Symlink(target, name)
}
