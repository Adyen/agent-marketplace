package backup

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type codexSetOutput struct {
	parent *fsboundary.Dir
	native *os.Root
	path   string
	owned  map[string]fsboundary.Identity
}

func (out *codexSetOutput) write(name string, mode os.FileMode, produce func(*os.File) error) error {
	var identity fsboundary.Identity
	if err := writeCapturedArchiveOutput(out.parent, filepath.Join(out.path, name), func(file *os.File) error {
		if err := produce(file); err != nil {
			return err
		}
		if err := file.Chmod(mode); err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		identity, err = fsboundary.FileInfoIdentity(info)
		return err
	}); err != nil {
		return err
	}
	out.owned[name] = identity
	return nil
}

func (out *codexSetOutput) publish(staged, final string) (err error) {
	if err := verifyArchiveParent(out.parent, out.path); err != nil {
		return err
	}
	snapshot, err := fsboundary.CaptureDestination(out.parent, staged, fsboundary.LinkPolicyRepository)
	if err != nil {
		return errors.Join(fsboundary.ErrGenerationConflict, err)
	}
	defer func() {
		if closeErr := snapshot.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	identity := snapshot.Identity()
	if expected, ok := out.owned[staged]; !ok || snapshot.Link() != nil || !snapshot.Exists() || !identity.SameState(expected) {
		return fsboundary.ErrGenerationConflict
	}
	if err := snapshot.ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
		return err
	}
	if _, err := out.native.Lstat(final); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return err
		}
		return fsboundary.ErrGenerationConflict
	}
	if err := out.native.Rename(staged, final); err != nil {
		return err
	}
	delete(out.owned, staged)
	out.owned[final] = identity
	return verifyArchiveParent(out.parent, out.path)
}

func (out *codexSetOutput) cleanup() (err error) {
	for name, expected := range out.owned {
		info, statErr := out.native.Lstat(name)
		if errors.Is(statErr, fs.ErrNotExist) {
			continue
		}
		if statErr != nil {
			err = errors.Join(err, statErr)
			continue
		}
		identity, statErr := fsboundary.FileInfoIdentity(info)
		if statErr != nil {
			err = errors.Join(err, statErr)
			continue
		}
		if identity.SameObject(expected) {
			if unlinkErr := out.parent.Unlink(name); unlinkErr != nil {
				err = errors.Join(err, unlinkErr)
			}
		}
	}
	return err
}
