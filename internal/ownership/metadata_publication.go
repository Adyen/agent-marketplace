package ownership

import (
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (s *Store) captureMetadataParent() (*fsboundary.Dir, error) {
	return s.captureStateDirectory(filepath.Join(ownershipDirName, s.rootKey))
}

func (s *Store) captureStateDirectory(relative string) (parent *fsboundary.Dir, err error) {
	if !fs.ValidPath(filepath.ToSlash(relative)) || relative == "." {
		return nil, fsboundary.ErrUnsafePath
	}
	return captureAbsoluteDirectory(filepath.Join(s.canonicalStateRoot, relative))
}

func captureAbsoluteDirectory(absolute string) (parent *fsboundary.Dir, err error) {
	anchor, err := fsboundary.OpenDir(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := anchor.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			if parent != nil {
				_ = parent.Close()
				parent = nil
			}
		}
	}()
	rel, err := filepath.Rel(anchor.Path(), absolute)
	if err != nil {
		return nil, err
	}
	return anchor.OpenPath(rel)
}

func (s *Store) prepareStateDirectory(relative string) (err error) {
	if !fs.ValidPath(filepath.ToSlash(relative)) || relative == "." {
		return fsboundary.ErrUnsafePath
	}
	return preparePrivateDirectory(filepath.Join(s.canonicalStateRoot, relative))
}

func preparePrivateDirectory(absolute string) (err error) {
	current, err := fsboundary.OpenDir(string(filepath.Separator))
	if err != nil {
		return err
	}
	defer func() {
		if current != nil {
			if closeErr := current.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	for _, name := range strings.Split(strings.TrimPrefix(absolute, string(filepath.Separator)), string(filepath.Separator)) {
		next, openErr := current.OpenPath(name)
		if errors.Is(openErr, fs.ErrNotExist) && !errors.Is(openErr, fsboundary.ErrUnsafePath) {
			if createErr := current.Mkdir(name, 0o700); createErr != nil && !errors.Is(createErr, fs.ErrExist) {
				return createErr
			}
			next, openErr = current.OpenPath(name)
		}
		if openErr != nil {
			return openErr
		}
		previous := current
		current = next
		if closeErr := previous.Close(); closeErr != nil {
			return closeErr
		}
	}
	fresh, err := captureAbsoluteDirectory(absolute)
	if err != nil {
		return err
	}
	unchanged := fresh.Identity().SameObject(current.Identity())
	if err := fresh.Close(); err != nil {
		return err
	}
	if !unchanged {
		return fsboundary.ErrGenerationConflict
	}
	return nil
}

func (s *Store) verifyMetadataParent(parent *fsboundary.Dir) error {
	return s.verifyStateDirectory(parent, filepath.Join(ownershipDirName, s.rootKey))
}

func (s *Store) verifyStateDirectory(parent *fsboundary.Dir, relative string) error {
	fresh, err := s.captureStateDirectory(relative)
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
	return nil
}

func (s *Store) writeMetadata(name string, data []byte) error {
	_, err := s.writeObservedMetadata(name, data)
	return err
}

func (s *Store) writeObservedMetadata(name string, data []byte) (pin metadataPin, err error) {
	parent, err := s.captureMetadataParent()
	if err != nil {
		return pin, err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	pin, _, err = writeObservedMetadataFile(parent, name, data, func() error { return s.verifyMetadataParent(parent) })
	return pin, err
}

func writeObservedMetadataFile(parent *fsboundary.Dir, name string, data []byte, validate func() error) (pin metadataPin, crossed bool, err error) {
	destination, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyRepository)
	if err != nil {
		return pin, false, err
	}
	defer func() {
		if closeErr := destination.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if destination.Link() != nil {
		return pin, false, fsboundary.ErrUnsafeFile
	}
	native, err := parent.NativeRoot()
	if err != nil {
		return pin, false, err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	staged, stagedName, err := parent.CreateTemp(".tmp-" + name + "-*")
	if err != nil {
		return pin, false, err
	}
	info, err := staged.Stat()
	if err != nil {
		if closeErr := staged.Close(); closeErr != nil {
			return pin, false, errors.Join(err, closeErr)
		}
		return pin, false, err
	}
	created, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		if closeErr := staged.Close(); closeErr != nil {
			return pin, false, errors.Join(err, closeErr)
		}
		return pin, false, err
	}
	defer func() {
		if staged != nil {
			if closeErr := staged.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		info, statErr := native.Lstat(stagedName)
		if errors.Is(statErr, fs.ErrNotExist) {
			return
		}
		if statErr != nil {
			err = errors.Join(err, statErr)
			return
		}
		identity, inspectErr := fsboundary.FileInfoIdentity(info)
		if inspectErr != nil {
			err = errors.Join(err, inspectErr)
			return
		}
		if created.SameObject(identity) {
			if removeErr := parent.Unlink(stagedName); removeErr != nil {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	if _, err := staged.Write(data); err != nil {
		return pin, false, err
	}
	if err := staged.Chmod(0o600); err != nil {
		return pin, false, err
	}
	info, err = staged.Stat()
	if err != nil {
		return pin, false, err
	}
	produced, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return pin, false, err
	}
	closeErr := staged.Close()
	staged = nil
	if closeErr != nil {
		return pin, false, closeErr
	}
	if err := validate(); err != nil {
		return pin, false, err
	}
	output, err := fsboundary.CaptureDestination(parent, stagedName, fsboundary.LinkPolicyRepository)
	if err != nil {
		return pin, false, err
	}
	defer func() {
		if closeErr := output.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if output.Link() != nil || !output.Identity().SameState(produced) {
		return pin, false, fsboundary.ErrGenerationConflict
	}
	if err := output.ReadTargetWith(func(io.Reader) error { return nil }); err != nil {
		return pin, false, err
	}
	if err := destination.Publish(parent, stagedName); err != nil {
		return pin, publishCrossedRename(err), err
	}
	info, err = native.Lstat(name)
	if err != nil {
		return pin, true, err
	}
	pin.file, err = fsboundary.FileInfoIdentity(info)
	if err != nil {
		return pin, true, err
	}
	if !publishedMetadataMatches(produced, pin.file) {
		return pin, true, fsboundary.ErrGenerationConflict
	}
	pin.parent = parent.Identity()
	return pin, true, nil
}

// publishedMetadataMatches reports whether the leaf observed after rename is
// still the staged object with its staged content state. Change time is
// excluded because a rename may update it; an in-place edit after publication
// changes size or modification time and must not become the recorded pin.
func publishedMetadataMatches(produced, published fsboundary.Identity) bool {
	return published.SameObject(produced) &&
		published.Mode.IsRegular() &&
		published.Links == 1 &&
		published.Size == produced.Size &&
		published.ModTimeNS == produced.ModTimeNS &&
		published.Mode == produced.Mode &&
		published.UID == produced.UID
}

// publishCrossedRename reports whether a failed Publish had already renamed
// the staged file into place: Publish marks post-rename directory-sync
// failures as durability-uncertain, while earlier refusals leave the name
// untouched.
func publishCrossedRename(err error) bool {
	return errors.Is(err, fsboundary.ErrDurabilityUncertain)
}
