package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func copyScopedSupport(srcDir, dstDir string, compiled map[string]bool, prefix string, missingOK bool, write func(string, []byte, os.FileMode, *destinationBoundary) error) (err error) {
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
	dstDir, err = filepath.Abs(dstDir)
	if err != nil {
		return err
	}
	var output *fsboundary.Dir
	defer func() {
		if output != nil {
			err = errors.Join(err, output.Close())
		}
	}()
	var boundary *destinationBoundary
	var walk func(string) error
	walk = func(rel string) error {
		entries, err := readSupportSourceDirectory(root, rel)
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
			if boundary == nil {
				var captureErr error
				output, captureErr = capturePortableParent(filepath.Join(dstDir, ".placeholder"), nil, true)
				if captureErr != nil {
					return captureErr
				}
				boundary = &destinationBoundary{capturedRoot: output, capturedRootPath: dstDir}
				slog.Debug("support output admitted", "path", dstDir, "phase", "support-output")
			}
			destinationPath := filepath.Join(dstDir, child)
			if entry.IsDir() {
				// A root skill's source dir is a whole registry checkout, so
				// VCS internals must never become harness-visible support
				// files. Git cannot track a .git directory, so regular skills
				// are unaffected.
				if err := boundary.directory(destinationPath, true); err != nil {
					return err
				}
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			data, identity, err := readSupportSourceSnapshot(root, child)
			if err != nil {
				return err
			}
			if err := write(destinationPath, data, identity.Mode, boundary); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(".")
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

func writeSupportStage(path string, data []byte, mode os.FileMode, boundary *destinationBoundary) (err error) {
	parent, err := boundary.captureDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, native.Close()) }()
	file, err := native.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return errors.Join(err, file.Close())
	}
	_, err = file.Write(data)
	return errors.Join(err, file.Close())
}

func writeSupportSnapshot(path string, data []byte, mode os.FileMode, boundary *destinationBoundary) (err error) {
	parent, err := boundary.captureDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, native.Close()) }()
	info, err := native.Lstat(filepath.Base(path))
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: support destination is a link", fsboundary.ErrUnsafePath)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: support destination is not regular", fsboundary.ErrUnsafeFile)
		}
		if info.Size() == int64(len(data)) {
			existing, identity, err := parent.ReadFile(filepath.Base(path), int64(len(data)))
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
	return publishPortableBytes(path, data, mode, info, boundary)
}
