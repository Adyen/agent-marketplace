package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func capturePortableParent(dst string, boundary *destinationBoundary, create bool) (parent *fsboundary.Dir, err error) {
	parentPath := filepath.Dir(dst)
	if boundary != nil {
		return boundary.captureDirectory(parentPath, create)
	}
	absolute, err := filepath.Abs(parentPath)
	if err != nil {
		return nil, err
	}
	root, err := fsboundary.OpenDir(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			if parent != nil {
				_ = parent.Close()
				parent = nil
			}
		}
	}()
	if create {
		return root.EnsurePath(filepath.Base(absolute))
	}
	return root.OpenPath(filepath.Base(absolute))
}

func publishPortableTree(src, dst string, observed os.FileInfo, ownershipDigest string, boundary *destinationBoundary) (err error) {
	parent, err := capturePortableParent(dst, boundary, true)
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
		info, err := native.Lstat(name)
		if observed == nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: portable tree appeared", fsboundary.ErrGenerationConflict)
		}
		if err != nil {
			return fmt.Errorf("%w: portable tree unavailable: %w", fsboundary.ErrGenerationConflict, err)
		}
		identity, err := fsboundary.FileInfoIdentity(info)
		if err != nil {
			return err
		}
		if !identity.SameState(expected) {
			return fmt.Errorf("%w: portable tree changed", fsboundary.ErrGenerationConflict)
		}
		if ownershipDigest != "" {
			h := sha256.New()
			if err := writePortableDigestEntry(h, parent, name, ""); err != nil {
				return err
			}
			if "sha256:"+hex.EncodeToString(h.Sum(nil)) != ownershipDigest {
				return fmt.Errorf("%w: portable tree ownership changed", fsboundary.ErrGenerationConflict)
			}
		}
		return nil
	}
	if err := verify(); err != nil {
		return err
	}
	source, err := fsboundary.OpenSourceFS(src)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := source.Stat(".")
	if err != nil {
		return err
	}
	reservation, stagedName, err := parent.CreateTemp(".agent-marketplace-portable-tree-*")
	if err != nil {
		return err
	}
	if err := reservation.Close(); err != nil {
		_ = parent.Unlink(stagedName)
		return err
	}
	if err := parent.Unlink(stagedName); err != nil {
		return err
	}
	if err := parent.Mkdir(stagedName, info.Mode().Perm()); err != nil {
		return err
	}
	created, err := native.Lstat(stagedName)
	if err != nil {
		return err
	}
	createdID, err := fsboundary.FileInfoIdentity(created)
	if err != nil {
		return err
	}
	defer func() {
		current, inspectErr := native.Lstat(stagedName)
		if inspectErr == nil {
			identity, identityErr := fsboundary.FileInfoIdentity(current)
			if identityErr == nil && identity.SameObject(createdID) {
				_ = native.RemoveAll(stagedName)
			}
		}
	}()
	staged, err := native.OpenRoot(stagedName)
	if err != nil {
		return err
	}
	openedStage, stageStatErr := staged.Stat(".")
	if stageStatErr != nil {
		return errors.Join(stageStatErr, staged.Close())
	}
	openedStageID, stageStatErr := fsboundary.FileInfoIdentity(openedStage)
	if stageStatErr != nil {
		return errors.Join(stageStatErr, staged.Close())
	}
	if !openedStageID.SameState(createdID) {
		return errors.Join(fsboundary.ErrGenerationConflict, staged.Close())
	}
	copyErr := copyPortableTreeContents(source, staged, ".")
	stagedInfo, statErr := staged.Stat(".")
	closeErr := staged.Close()
	if copyErr != nil || statErr != nil || closeErr != nil {
		return errors.Join(copyErr, statErr, closeErr)
	}
	stagedID, err := fsboundary.FileInfoIdentity(stagedInfo)
	if err != nil {
		return err
	}
	if boundary != nil && boundary.linkPublicationHook != nil {
		boundary.linkPublicationHook(dst)
	}
	fresh, err := capturePortableParent(dst, boundary, false)
	if err != nil {
		return fmt.Errorf("%w: portable tree parent unavailable: %w", fsboundary.ErrGenerationConflict, err)
	}
	unchanged := fresh != nil && fresh.Identity().SameObject(parent.Identity())
	if fresh != nil {
		if closeErr := fresh.Close(); closeErr != nil {
			return closeErr
		}
	}
	if !unchanged {
		return fmt.Errorf("%w: portable tree parent replaced", fsboundary.ErrGenerationConflict)
	}
	if err := verify(); err != nil {
		return err
	}
	currentStage, err := native.Lstat(stagedName)
	if err != nil {
		return err
	}
	currentStageID, err := fsboundary.FileInfoIdentity(currentStage)
	if err != nil {
		return err
	}
	if !currentStageID.SameState(stagedID) {
		return fmt.Errorf("%w: portable staged tree replaced", fsboundary.ErrGenerationConflict)
	}
	if observed != nil {
		if err := native.RemoveAll(name); err != nil {
			return err
		}
	}
	return native.Rename(stagedName, name)
}

func copyPortableTreeContents(source fs.FS, destination *os.Root, dir string) error {
	entries, err := fs.ReadDir(source, dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := path.Join(dir, entry.Name())
		file, err := source.Open(name)
		if err != nil {
			return err
		}
		info, statErr := file.Stat()
		if statErr != nil {
			return errors.Join(statErr, file.Close())
		}
		if info.IsDir() {
			if err := file.Close(); err != nil {
				return err
			}
			if err := destination.Mkdir(filepath.FromSlash(name), info.Mode().Perm()); err != nil {
				return err
			}
			if err := copyPortableTreeContents(source, destination, name); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return errors.Join(fsboundary.ErrUnsafeFile, file.Close())
		}
		output, err := destination.OpenFile(filepath.FromSlash(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return errors.Join(err, file.Close())
		}
		if err := output.Chmod(info.Mode().Perm()); err != nil {
			return errors.Join(err, output.Close(), file.Close())
		}
		_, copyErr := io.Copy(output, file)
		closeErr := errors.Join(output.Close(), file.Close())
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	}
	return nil
}
