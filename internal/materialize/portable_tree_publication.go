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

func publishPortableTree(src, dst string, observed os.FileInfo, ownershipDigest string, boundary *destinationBoundary, inspection ...func(string) (os.FileInfo, error)) (err error) {
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
	staged, stagedName, err := captureNewPortableTreeStage(parent, native)
	if err != nil {
		return err
	}
	createdID := staged.Identity()
	directories := []portableStageDirectory{{parent: parent, name: stagedName, directory: staged}}
	defer func() {
		if cleanupErr := cleanupPortableTreeStage(native, stagedName, createdID, directories...); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		for i := len(directories) - 1; i >= 0; i-- {
			if closeErr := directories[i].directory.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	openedStage, err := staged.Stat()
	if err != nil {
		return err
	}
	openedStageID, err := fsboundary.FileInfoIdentity(openedStage)
	if err != nil {
		return err
	}
	inspect := native.Lstat
	if len(inspection) > 0 {
		inspect = inspection[0]
	}
	created, err := inspect(stagedName)
	if err != nil {
		return err
	}
	namedID, err := fsboundary.FileInfoIdentity(created)
	if err != nil {
		return fmt.Errorf("%w: staged tree identity unavailable: %w", fsboundary.ErrGenerationConflict, err)
	}
	if !openedStageID.SameState(namedID) {
		return fsboundary.ErrGenerationConflict
	}
	if err := staged.Chmod(0o700); err != nil {
		return err
	}
	if err := copyPortableTreeContents(source, staged, ".", &directories); err != nil {
		return err
	}
	if err := staged.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	stagedInfo, err := staged.Stat()
	if err != nil {
		return err
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

type portableStageDirectory struct {
	parent    *fsboundary.Dir
	name      string
	directory *fsboundary.Dir
}

func captureNewPortableTreeStage(parent *fsboundary.Dir, native *os.Root) (*fsboundary.Dir, string, error) {
	reservation, name, err := parent.CreateTemp(".agent-marketplace-portable-tree-*")
	if err != nil {
		return nil, "", err
	}
	reserved, statErr := reservation.Stat()
	closeErr := reservation.Close()
	if statErr != nil || closeErr != nil {
		return nil, name, errors.Join(statErr, closeErr)
	}
	current, err := native.Lstat(name)
	if err != nil {
		return nil, name, err
	}
	if !os.SameFile(reserved, current) {
		return nil, name, fsboundary.ErrGenerationConflict
	}
	if err := parent.Unlink(name); err != nil {
		return nil, name, err
	}
	if err := parent.Mkdir(name, 0o700); err != nil {
		return nil, name, err
	}
	stage, err := parent.OpenPath(name)
	if err != nil {
		return nil, name, err
	}
	if stage.Identity().UID != uint32(os.Geteuid()) || stage.Identity().Mode.Perm()&0o077 != 0 {
		return nil, name, errors.Join(fsboundary.ErrGenerationConflict, stage.Close())
	}
	return stage, name, nil
}

func cleanupPortableTreeStage(native interface {
	Lstat(string) (os.FileInfo, error)
	RemoveAll(string) error
}, stagedName string, createdID fsboundary.Identity, directories ...portableStageDirectory) error {
	current, err := native.Lstat(stagedName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect portable staged tree %s: %w", stagedName, err)
	}
	identity, err := fsboundary.FileInfoIdentity(current)
	if err != nil {
		return fmt.Errorf("identify portable staged tree %s: %w", stagedName, err)
	}
	if !identity.SameObject(createdID) {
		return nil
	}
	if len(directories) > 0 {
		root := directories[0].directory
		if !root.Identity().SameObject(createdID) {
			return fsboundary.ErrGenerationConflict
		}
		if err := restorePortableStageDirectories(directories); err != nil {
			return err
		}
		current, err := native.Lstat(stagedName)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reinspect portable staged tree %s: %w", stagedName, err)
		}
		identity, err := fsboundary.FileInfoIdentity(current)
		if err != nil {
			return err
		}
		if !identity.SameObject(createdID) {
			return nil
		}
	}
	if err := native.RemoveAll(stagedName); err != nil {
		return fmt.Errorf("remove portable staged tree %s: %w", stagedName, err)
	}
	return nil
}

func restorePortableStageDirectories(directories []portableStageDirectory) error {
	owned := make(map[*fsboundary.Dir]map[string]*fsboundary.Dir)
	for _, directory := range directories {
		if owned[directory.parent] == nil {
			owned[directory.parent] = make(map[string]*fsboundary.Dir)
		}
		owned[directory.parent][directory.name] = directory.directory
	}
	var restore func(*fsboundary.Dir) error
	restore = func(directory *fsboundary.Dir) (err error) {
		if err := directory.Chmod(0o700); err != nil {
			return err
		}
		native, err := directory.NativeRoot()
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := native.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
		entries, err := fs.ReadDir(native.FS(), ".")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			current, err := native.Lstat(entry.Name())
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !current.IsDir() {
				continue
			}
			child := owned[directory][entry.Name()]
			if child == nil {
				return fsboundary.ErrGenerationConflict
			}
			identity, err := fsboundary.FileInfoIdentity(current)
			if err != nil {
				return err
			}
			if !identity.SameObject(child.Identity()) {
				return fsboundary.ErrGenerationConflict
			}
			if err := restore(child); err != nil {
				return err
			}
		}
		return nil
	}
	return restore(directories[0].directory)
}

func copyPortableTreeContents(source fs.FS, destination *fsboundary.Dir, dir string, directories *[]portableStageDirectory) (err error) {
	native, err := destination.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
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
			if err := destination.Mkdir(entry.Name(), 0o700); err != nil {
				return err
			}
			child, err := destination.OpenPath(entry.Name())
			if err != nil {
				return err
			}
			if child.Identity().UID != uint32(os.Geteuid()) || child.Identity().Mode.Perm()&0o077 != 0 {
				return errors.Join(fsboundary.ErrGenerationConflict, child.Close())
			}
			*directories = append(*directories, portableStageDirectory{parent: destination, name: entry.Name(), directory: child})
			if err := child.Chmod(0o700); err != nil {
				return err
			}
			if err := copyPortableTreeContents(source, child, name, directories); err != nil {
				return err
			}
			if err := child.Chmod(info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return errors.Join(fsboundary.ErrUnsafeFile, file.Close())
		}
		output, err := native.OpenFile(entry.Name(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return errors.Join(err, file.Close())
		}
		_, copyErr := io.Copy(output, file)
		if copyErr == nil {
			copyErr = output.Chmod(info.Mode().Perm())
		}
		closeErr := errors.Join(output.Close(), file.Close())
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	}
	return nil
}
