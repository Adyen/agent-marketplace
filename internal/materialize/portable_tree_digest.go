package materialize

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func writePortableDigestEntry(h io.Writer, root *fsboundary.Dir, name, rel string) (err error) {
	native, err := root.NativeRoot()
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
		return err
	}
	before, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return err
	}
	writeHeader := func(kind string) error {
		_, err := fmt.Fprintf(h, "%s\x00%s\x00%#o\x00", kind, filepath.ToSlash(rel), before.Mode.Perm())
		return err
	}
	switch {
	case before.Mode.IsRegular():
		return writePortableRegularDigestAt(h, root, name, rel, info)
	case before.Mode.IsDir():
		directory, openErr := root.OpenPath(name)
		if openErr != nil {
			return openErr
		}
		defer func() {
			if closeErr := directory.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
		if !directory.Identity().SameState(before) {
			return fmt.Errorf("%w: portable directory changed before enumeration", fsboundary.ErrGenerationConflict)
		}
		childRoot, openErr := directory.NativeRoot()
		if openErr != nil {
			return openErr
		}
		defer func() {
			if closeErr := childRoot.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
		file, openErr := childRoot.Open(".")
		if openErr != nil {
			return openErr
		}
		if err := writeHeader("dir"); err != nil {
			return errors.Join(err, file.Close())
		}
		entries, readErr := file.ReadDir(-1)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if err := writePortableDigestEntry(h, directory, entry.Name(), filepath.Join(rel, entry.Name())); err != nil {
				return err
			}
		}
		after, statErr := childRoot.Stat(".")
		if statErr != nil {
			return statErr
		}
		identity, statErr := fsboundary.FileInfoIdentity(after)
		if statErr != nil {
			return statErr
		}
		if !identity.SameState(before) {
			return fmt.Errorf("%w: portable directory changed while digesting", fsboundary.ErrGenerationConflict)
		}
	case before.Mode&os.ModeSymlink != 0:
		if err := writeHeader("symlink"); err != nil {
			return err
		}
		target, readErr := native.Readlink(name)
		if readErr != nil {
			return readErr
		}
		if _, err := fmt.Fprintf(h, "%d\x00%s", len(target), target); err != nil {
			return err
		}
	default:
		if err := writeHeader("other"); err != nil {
			return err
		}
	}
	after, err := native.Lstat(name)
	if err != nil {
		return fmt.Errorf("%w: portable entry disappeared after digest: %w", fsboundary.ErrGenerationConflict, err)
	}
	identity, err := fsboundary.FileInfoIdentity(after)
	if err != nil {
		return err
	}
	if !identity.SameState(before) {
		return fmt.Errorf("%w: portable entry changed after digest", fsboundary.ErrGenerationConflict)
	}
	return nil
}
