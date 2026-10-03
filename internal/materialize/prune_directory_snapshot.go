package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func capturePruneDirectory(dir string, boundary *destinationBoundary) (root *fsboundary.Dir, err error) {
	root, err = capturePortableParent(filepath.Join(dir, ".placeholder"), boundary, false)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fs.ErrNotExist
	}
	defer func() {
		if err != nil {
			if closeErr := root.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
			root = nil
		}
	}()
	if boundary != nil && boundary.linkPublicationHook != nil {
		boundary.linkPublicationHook(dir)
	}
	return root, verifyPruneDirectory(dir, boundary, root)
}

func verifyPruneDirectory(dir string, boundary *destinationBoundary, root *fsboundary.Dir) error {
	fresh, err := capturePortableParent(filepath.Join(dir, ".placeholder"), boundary, false)
	if err != nil {
		return fmt.Errorf("%w: prune directory changed: %w", fsboundary.ErrGenerationConflict, err)
	}
	if fresh == nil {
		return fsboundary.ErrGenerationConflict
	}
	unchanged := fresh.Identity().SameObject(root.Identity())
	if err := fresh.Close(); err != nil {
		return err
	}
	if !unchanged {
		return fsboundary.ErrGenerationConflict
	}
	return nil
}

type pruneDirectoryEntry struct {
	name           string
	observed       os.FileInfo
	linkText       string
	managedCommand bool
}

func (e pruneDirectoryEntry) Name() string { return e.name }

func readPruneDirectory(dir string, boundary *destinationBoundary, keep func(string) bool) (entries []pruneDirectoryEntry, err error) {
	root, err := capturePruneDirectory(dir, boundary)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	names, err := readSupportSourceDirectory(root, ".")
	if err != nil {
		return nil, err
	}
	sort.Slice(names, func(i, j int) bool { return names[i].Name() < names[j].Name() })
	if boundary != nil && boundary.pruneEnumerationHook != nil {
		boundary.pruneEnumerationHook(dir)
	}
	native, err := root.NativeRoot()
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	entries = make([]pruneDirectoryEntry, 0, len(names))
	for _, name := range names {
		if keep != nil && keep(name.Name()) {
			continue
		}
		info, err := native.Lstat(name.Name())
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entry := pruneDirectoryEntry{name: name.Name(), observed: info}
		if info.Mode()&os.ModeSymlink != 0 {
			entry.linkText, err = native.Readlink(name.Name())
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			fresh, err := native.Lstat(name.Name())
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			before, err := fsboundary.FileInfoIdentity(info)
			if err != nil {
				return nil, err
			}
			after, err := fsboundary.FileInfoIdentity(fresh)
			if err != nil {
				return nil, err
			}
			if !before.SameState(after) {
				return nil, fsboundary.ErrGenerationConflict
			}
		} else if info.Mode().IsRegular() {
			if boundary != nil && boundary.pruneMarkerReadHook != nil {
				boundary.pruneMarkerReadHook(filepath.Join(dir, name.Name()))
			}
			data, readErr := readNativeCommandSnapshot(root, name.Name(), info, true)
			if fsboundary.IsSourceRefusal(readErr) {
				return nil, readErr
			}
			entry.managedCommand = readErr == nil && isManagedCommandContent(data)
		}
		entries = append(entries, entry)
	}
	if err := verifyPruneDirectory(dir, boundary, root); err != nil {
		return nil, err
	}
	return entries, nil
}
