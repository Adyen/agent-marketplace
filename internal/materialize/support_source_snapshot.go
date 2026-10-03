package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func copyScopedSupport(srcDir, dstDir string, compiled map[string]bool, prefix string, missingOK bool, write func(string, []byte, os.FileMode) error) error {
	if srcDir == "" {
		if missingOK {
			return nil
		}
		return fmt.Errorf("%w: empty support source", fs.ErrNotExist)
	}
	absolute, err := filepath.Abs(srcDir)
	if err != nil {
		return err
	}
	root, err := fsboundary.OpenDir(absolute)
	if missingOK && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	active := make(map[string]bool)
	var walk func(string, string) error
	walk = func(sourceRel, rel string) error {
		if active[sourceRel] {
			return fmt.Errorf("%w: cyclic support directory alias", fsboundary.ErrUnsafePath)
		}
		active[sourceRel] = true
		defer delete(active, sourceRel)
		entries, err := readSupportSourceDirectory(root, sourceRel)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			child := filepath.Join(rel, entry.Name())
			artifactRel := filepath.ToSlash(filepath.Join(prefix, child))
			if artifactRel == "SKILL.md" || compiled[artifactRel] {
				continue
			}
			// A root skill's source dir is a whole registry checkout, so
			// VCS internals must never become harness-visible support
			// files. Git cannot track a .git directory, so regular skills
			// are unaffected.
			if entry.Name() == ".git" {
				continue
			}
			sourceChild, isDir, err := supportSourceChild(root, sourceRel, entry)
			if err != nil {
				return err
			}
			destinationPath := filepath.Join(dstDir, child)
			if isDir {
				// A root skill's source dir is a whole registry checkout, so
				// VCS internals must never become harness-visible support
				// files. Git cannot track a .git directory, so regular skills
				// are unaffected.
				if err := os.MkdirAll(destinationPath, 0o755); err != nil {
					return err
				}
				if err := walk(sourceChild, child); err != nil {
					return err
				}
				continue
			}
			data, identity, err := readSupportSourceSnapshot(root, sourceChild)
			if err != nil {
				return err
			}
			if err := write(destinationPath, data, identity.Mode); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(".", ".")
}

func supportSourceChild(root *fsboundary.Dir, parent string, entry os.DirEntry) (string, bool, error) {
	rel := filepath.Join(parent, entry.Name())
	if entry.Type()&os.ModeSymlink == 0 {
		return rel, entry.IsDir(), nil
	}
	directory, err := root.OpenContainedRoot(rel)
	if err != nil {
		return rel, false, nil
	}
	targetRel, err := filepath.Rel(root.Path(), directory.Path())
	return targetRel, true, errors.Join(err, directory.Close())
}

func readSupportSourceSnapshot(root *fsboundary.Dir, rel string) ([]byte, fsboundary.Identity, error) {
	source, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, fsboundary.Identity{}, err
	}
	data, identity, err := source.ReadTarget(source.Identity().Size)
	return data, identity, errors.Join(err, source.Close())
}

func readSupportSourceDirectory(root *fsboundary.Dir, rel string) ([]os.DirEntry, error) {
	directory, err := root.OpenPath(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	native, err := directory.NativeRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = native.Close() }()
	file, err := native.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := file.ReadDir(-1)
	return entries, errors.Join(err, file.Close())
}

func writeSupportStage(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return errors.Join(err, file.Close())
	}
	_, err = file.Write(data)
	return errors.Join(err, file.Close())
}

func writeSupportSnapshot(path string, data []byte, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: support destination is a link", fsboundary.ErrUnsafePath)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: support destination is not regular", fsboundary.ErrUnsafeFile)
		}
		if info.Size() == int64(len(data)) {
			root, err := fsboundary.OpenDir(filepath.Dir(path))
			if err != nil {
				return err
			}
			defer func() { _ = root.Close() }()
			existing, identity, err := root.ReadFile(filepath.Base(path), int64(len(data)))
			if err == nil && bytes.Equal(existing, data) && identity.Mode.Perm() == mode.Perm() {
				return nil
			}
			if err != nil && !errors.Is(err, fsboundary.ErrTooLarge) {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWriteFile(path, data, mode)
}
