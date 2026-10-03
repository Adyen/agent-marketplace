package backup

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func readBackupSourceMetadata(root *fsboundary.Dir, rel string) (info fs.FileInfo, link string, err error) {
	rel = filepath.Clean(rel)
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", fsboundary.ErrUnsafePath
	}
	parentRel := filepath.Dir(rel)
	parent := root
	if parentRel != "." {
		parent, err = root.OpenPath(parentRel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, "", nil
			}
			return nil, "", err
		}
		defer func() {
			if closeErr := parent.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
	}
	native, err := parent.NativeRoot()
	if err != nil {
		return nil, "", err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	name := filepath.Base(rel)
	info, err = native.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		link, err = native.Readlink(name)
		if err != nil {
			return nil, "", err
		}
		after, statErr := native.Lstat(name)
		if statErr != nil {
			return nil, "", errors.Join(fsboundary.ErrGenerationConflict, statErr)
		}
		beforeID, idErr := fsboundary.FileInfoIdentity(info)
		if idErr != nil {
			return nil, "", idErr
		}
		afterID, idErr := fsboundary.FileInfoIdentity(after)
		if idErr != nil {
			return nil, "", idErr
		}
		if !beforeID.SameState(afterID) {
			return nil, "", fsboundary.ErrGenerationConflict
		}
	}
	return info, link, nil
}
