package materialize

import (
	"crypto/rand"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/Adyen/agent-marketplace/internal/cache"
)

// confinedFS is the only filesystem adapter used for harness output. It keeps
// an open directory handle and resolves every destination relative to that
// handle, so a path cannot escape between inspection and mutation.
type confinedFS struct {
	root   *os.Root
	abs    string
	prefix string
	seq    atomic.Uint64
}

func openConfinedFS(path string, create bool) (*confinedFS, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve output root %q: %w", path, err)
	}
	abs = filepath.Clean(abs)
	parent := filepath.Dir(abs)
	base := filepath.Base(abs)
	_, statErr := os.Lstat(abs)
	missing := os.IsNotExist(statErr)
	if statErr != nil && !missing {
		return nil, fmt.Errorf("inspect output root %q: %w", path, statErr)
	}
	if missing && !create {
		parentRoot, openErr := os.OpenRoot(parent)
		if openErr != nil {
			return nil, os.ErrNotExist
		}
		return &confinedFS{root: parentRoot, abs: abs, prefix: base}, nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create output root parent %q: %w", parent, err)
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return nil, fmt.Errorf("open output root parent %q: %w", parent, err)
	}
	defer func() { _ = parentRoot.Close() }()
	if create {
		if err := parentRoot.MkdirAll(base, 0o755); err != nil {
			return nil, fmt.Errorf("create output root %q: %w", path, err)
		}
	}
	root, err := parentRoot.OpenRoot(base)
	if err != nil {
		return nil, fmt.Errorf("open output root %q: %w", path, err)
	}
	return &confinedFS{root: root, abs: abs}, nil
}

func (f *confinedFS) Close() error { return f.root.Close() }

func (f *confinedFS) absPath(rel string) string {
	base := f.abs
	if f.prefix != "" {
		base = filepath.Dir(f.abs)
	}
	return filepath.Join(base, filepath.FromSlash(rel))
}

func (f *confinedFS) rel(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve output path %q: %w", path, err)
	}
	rel, err := filepath.Rel(f.abs, filepath.Clean(abs))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("output path %q is outside root %q", path, f.abs)
	}
	rel = filepath.ToSlash(rel)
	if f.prefix != "" {
		rel = filepath.ToSlash(filepath.Join(filepath.FromSlash(f.prefix), filepath.FromSlash(rel)))
	}
	return rel, nil
}

func (f *confinedFS) Lstat(path string) (os.FileInfo, error) {
	rel, err := f.rel(path)
	if err != nil {
		return nil, err
	}
	return f.root.Lstat(rel)
}

func (f *confinedFS) Stat(path string) (os.FileInfo, error) {
	rel, err := f.rel(path)
	if err != nil {
		return nil, err
	}
	return f.root.Stat(rel)
}

func (f *confinedFS) ReadFile(path string) ([]byte, error) {
	rel, err := f.rel(path)
	if err != nil {
		return nil, err
	}
	return f.root.ReadFile(rel)
}

func (f *confinedFS) ReadDir(path string) ([]os.DirEntry, error) {
	rel, err := f.rel(path)
	if err != nil {
		return nil, err
	}
	file, err := f.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return file.ReadDir(-1)
}

func (f *confinedFS) Readlink(path string) (string, error) {
	rel, err := f.rel(path)
	if err != nil {
		return "", err
	}
	return f.root.Readlink(rel)
}

func (f *confinedFS) ExistingSymlinkTarget(path string) (string, bool) {
	info, err := f.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return "", false
	}
	target, err := f.Readlink(path)
	return target, err == nil
}

func (f *confinedFS) IsManagedSymlink(path string) bool {
	target, ok := f.ExistingSymlinkTarget(path)
	return ok && strings.HasPrefix(target, cache.CacheDir()+string(os.PathSeparator))
}

func (f *confinedFS) MkdirAll(path string, perm fs.FileMode) error {
	rel, err := f.rel(path)
	if err != nil {
		return err
	}
	return f.root.MkdirAll(rel, perm.Perm())
}

func (f *confinedFS) EnsureDir(path string, dryRun bool) error {
	info, err := f.Lstat(path)
	if err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			err := fmt.Errorf("output path %q is a symlink", path)
			logMaterializeOutputFailure("ensure-directory", path, err)
			return err
		}
		if !info.IsDir() {
			err := fmt.Errorf("output path %q is not a directory", path)
			logMaterializeOutputFailure("ensure-directory", path, err)
			return err
		}
		return nil
	}
	if !os.IsNotExist(err) {
		logMaterializeOutputFailure("inspect-directory", path, err)
		return err
	}
	if dryRun {
		return nil
	}
	if err := f.MkdirAll(path, 0o755); err != nil {
		logMaterializeOutputFailure("create-directory", path, err)
		return err
	}
	return nil
}

func (f *confinedFS) Remove(path string) error {
	rel, err := f.rel(path)
	if err != nil {
		return err
	}
	return f.root.Remove(rel)
}

func (f *confinedFS) RemoveAll(path string) error {
	rel, err := f.rel(path)
	if err != nil {
		return err
	}
	return f.root.RemoveAll(rel)
}

func (f *confinedFS) Symlink(target, path string) error {
	rel, err := f.rel(path)
	if err != nil {
		return err
	}
	return f.root.Symlink(target, rel)
}

func (f *confinedFS) AtomicSymlink(target, path string) error {
	rel, err := f.rel(path)
	if err != nil {
		return err
	}
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	if parent != "." {
		if err := f.root.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	tmpRel, err := f.tempRel(parent)
	if err != nil {
		return err
	}
	if err := f.root.Symlink(target, tmpRel); err != nil {
		return err
	}
	if err := f.root.Rename(tmpRel, rel); err != nil {
		_ = f.root.Remove(tmpRel)
		return err
	}
	return nil
}

func (f *confinedFS) AtomicWrite(path string, data []byte, mode fs.FileMode) error {
	rel, err := f.rel(path)
	if err != nil {
		return err
	}
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel)))
	if parent != "." {
		if err := f.root.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	for range 32 {
		tmpRel, err := f.tempRel(parent)
		if err != nil {
			return err
		}
		tmp, err := f.root.OpenFile(tmpRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return err
		}
		writeErr := writeAndClose(tmp, data)
		if writeErr != nil {
			_ = f.root.Remove(tmpRel)
			return writeErr
		}
		if err := f.root.Rename(tmpRel, rel); err != nil {
			_ = f.root.Remove(tmpRel)
			return err
		}
		return nil
	}
	return fmt.Errorf("could not allocate a unique temporary output path")
}

func (f *confinedFS) tempRel(parent string) (string, error) {
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate temporary output path: %w", err)
	}
	name := fmt.Sprintf(".agent-marketplace-%x-%d", token, f.seq.Add(1))
	if parent == "." {
		return name, nil
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(parent), name)), nil
}

func writeAndClose(file *os.File, data []byte) error {
	if n, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	} else if n != len(data) {
		_ = file.Close()
		return fmt.Errorf("short write: wrote %d of %d bytes", n, len(data))
	}
	return file.Close()
}

func (f *confinedFS) Walk(path string, fn fs.WalkDirFunc) error {
	rel, err := f.rel(path)
	if err != nil {
		return err
	}
	return fs.WalkDir(f.root.FS(), rel, fn)
}
