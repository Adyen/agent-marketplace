package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

type portableRemovalEntry struct {
	portableStageDirectory
	identity     fsboundary.Identity
	originalMode os.FileMode
	changed      bool
}

func removeVerifiedPortableOutput(parent *fsboundary.Dir, native *os.Root, name string, observed os.FileInfo, wantDigest string, barriers ...func()) (err error) {
	if observed == nil {
		return fsboundary.ErrGenerationConflict
	}
	expected, err := fsboundary.FileInfoIdentity(observed)
	if err != nil {
		return err
	}
	var entries []portableRemovalEntry
	defer func() {
		for i := len(entries) - 1; i >= 0; i-- {
			entry := &entries[i]
			if entry.directory == nil {
				continue
			}
			if err != nil && entry.changed {
				current, statErr := entry.directory.Stat()
				if statErr != nil {
					err = errors.Join(err, statErr)
				} else if current.Mode() == entry.originalMode|0o700 {
					if restoreErr := entry.directory.Chmod(entry.originalMode); restoreErr != nil {
						err = errors.Join(err, restoreErr)
					}
				}
			}
			if closeErr := entry.directory.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	if err := capturePortableRemovalEntries(parent, name, &entries); err != nil {
		return err
	}
	if !entries[0].identity.SameState(expected) {
		return fsboundary.ErrGenerationConflict
	}
	if expected.Mode.IsDir() && wantDigest == "" {
		return fsboundary.ErrGenerationConflict
	}
	if wantDigest != "" {
		h := sha256.New()
		if err := writePortableDigestEntry(h, parent, name, ""); err != nil {
			return err
		}
		if "sha256:"+hex.EncodeToString(h.Sum(nil)) != wantDigest {
			return fsboundary.ErrGenerationConflict
		}
	}
	if len(barriers) > 0 {
		barriers[0]()
	}
	if err := verifyPortableRemovalEntries(entries); err != nil {
		return err
	}
	for i := range entries {
		entry := &entries[i]
		if entry.directory == nil || entry.identity.Mode.Perm()&0o700 == 0o700 {
			continue
		}
		if err := verifyPortableRemovalEntries(entries); err != nil {
			return err
		}
		if err := entry.directory.Chmod(entry.originalMode | 0o700); err != nil {
			return err
		}
		entry.changed = true
		info, err := entry.directory.Stat()
		if err != nil {
			return err
		}
		identity, err := fsboundary.FileInfoIdentity(info)
		if err != nil {
			return err
		}
		accepted := entry.identity
		accepted.Mode, accepted.ChangeTimeNS = identity.Mode, identity.ChangeTimeNS
		if !accepted.SameState(identity) || identity.Mode != entry.originalMode|0o700 {
			return fsboundary.ErrGenerationConflict
		}
		entry.identity = accepted
	}
	if len(barriers) > 1 {
		barriers[1]()
	}
	if err := verifyPortableRemovalEntries(entries); err != nil {
		return err
	}
	return native.RemoveAll(name)
}

func capturePortableRemovalEntries(parent *fsboundary.Dir, name string, entries *[]portableRemovalEntry) (err error) {
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
	if err != nil {
		return fmt.Errorf("%w: owned removal entry unavailable: %w", fsboundary.ErrGenerationConflict, err)
	}
	identity, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return err
	}
	entry := portableRemovalEntry{portableStageDirectory: portableStageDirectory{parent: parent, name: name}, identity: identity, originalMode: identity.Mode}
	if !info.IsDir() {
		if !identity.Mode.IsRegular() && identity.Mode&os.ModeSymlink == 0 {
			return fsboundary.ErrUnsafeFile
		}
		*entries = append(*entries, entry)
		return nil
	}
	directory, err := parent.OpenPath(name)
	if err != nil {
		return err
	}
	entry.directory = directory
	*entries = append(*entries, entry)
	if !directory.Identity().SameState(identity) {
		return fsboundary.ErrGenerationConflict
	}
	children, err := directory.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := children.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	names, err := fs.ReadDir(children.FS(), ".")
	if err != nil {
		return err
	}
	for _, child := range names {
		if err := capturePortableRemovalEntries(directory, child.Name(), entries); err != nil {
			return err
		}
	}
	return nil
}

func verifyPortableRemovalEntries(entries []portableRemovalEntry) (err error) {
	for _, entry := range entries {
		native, err := entry.parent.NativeRoot()
		if err != nil {
			return err
		}
		info, statErr := native.Lstat(entry.name)
		closeErr := native.Close()
		if statErr != nil || closeErr != nil {
			return fmt.Errorf("%w: owned removal generation unavailable: %w", fsboundary.ErrGenerationConflict, errors.Join(statErr, closeErr))
		}
		identity, err := fsboundary.FileInfoIdentity(info)
		if err != nil {
			return err
		}
		if !identity.SameState(entry.identity) {
			return fsboundary.ErrGenerationConflict
		}
	}
	return nil
}

func removeOwnedPortablePath(path string, observed os.FileInfo, wantDigest string, boundary *destinationBoundary) (err error) {
	parent, err := capturePortableParent(path, boundary, false)
	if err != nil {
		if observed == nil && errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrUnsafeFile) {
			return nil
		}
		return err
	}
	if parent == nil {
		if observed == nil {
			return nil
		}
		return fsboundary.ErrGenerationConflict
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
	if boundary != nil && boundary.linkPublicationHook != nil {
		boundary.linkPublicationHook(path)
	}
	fresh, err := capturePortableParent(path, boundary, false)
	if err != nil {
		return err
	}
	if fresh == nil {
		return fsboundary.ErrGenerationConflict
	}
	unchanged := fresh.Identity().SameObject(parent.Identity())
	if err := fresh.Close(); err != nil {
		return err
	}
	if !unchanged {
		return fsboundary.ErrGenerationConflict
	}
	if observed == nil {
		if _, err := native.Lstat(filepath.Base(path)); errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		return fsboundary.ErrGenerationConflict
	}
	return removeVerifiedPortableOutput(parent, native, filepath.Base(path), observed, wantDigest)
}
