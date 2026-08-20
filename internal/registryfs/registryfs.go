package registryfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type FS struct {
	rootAbs        string
	rootReal       string
	rejectSymlinks bool
	mu             sync.Mutex
	validationErr  error
}

func New(root string, rejectSymlinks bool) (*FS, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve registry root: %w", err)
	}
	rootEntry, err := os.Lstat(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("stat registry root: %w", err)
	}
	if rejectSymlinks && rootEntry.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("registry root is a symlink; remote registries do not allow symlinks")
	}
	rootInfo, err := os.Stat(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("stat registry root: %w", err)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("registry root is not a directory")
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("resolve registry root symlinks: %w", err)
	}
	return &FS{rootAbs: rootAbs, rootReal: rootReal, rejectSymlinks: rejectSymlinks}, nil
}

func ValidateTree(root string, rejectSymlinks bool) error {
	secure, err := New(root, rejectSymlinks)
	if err != nil {
		return err
	}
	return fs.WalkDir(secure, ".", func(_ string, _ fs.DirEntry, err error) error {
		return err
	})
}

func ValidatePath(root, name string, rejectSymlinks bool) error {
	secure, err := New(root, rejectSymlinks)
	if err != nil {
		return err
	}
	if name == "" {
		name = "."
	}
	_, _, err = secure.validate(filepath.ToSlash(name))
	return err
}

func (r *FS) ValidationError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.validationErr
}

func (r *FS) Open(name string) (fs.File, error) {
	full, _, err := r.validate(name)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	return &fileWrapper{File: file, registry: r, name: name}, nil
}

func (r *FS) Stat(name string) (fs.FileInfo, error) {
	_, info, err := r.validate(name)
	return info, err
}

func (r *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	file, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	readDir, ok := file.(fs.ReadDirFile)
	if !ok {
		return nil, fmt.Errorf("registry path %q is not a readable directory", name)
	}
	return readDir.ReadDir(-1)
}

func (r *FS) validate(name string) (full string, result fs.FileInfo, err error) {
	defer func() {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			r.mu.Lock()
			if r.validationErr == nil {
				r.validationErr = err
			}
			r.mu.Unlock()
		}
	}()

	if !fs.ValidPath(name) {
		return "", nil, fmt.Errorf("invalid registry path %q", name)
	}

	full = filepath.Join(r.rootAbs, filepath.FromSlash(name))
	if !within(r.rootAbs, full) {
		return "", nil, fmt.Errorf("registry path %q escapes the registry root", name)
	}

	if name == "." {
		info, err := os.Stat(full)
		if err != nil {
			return "", nil, err
		}
		return full, info, checkType(name, info)
	}

	current := r.rootAbs
	for _, component := range strings.Split(filepath.FromSlash(name), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", nil, err
		}
		if err := r.checkEntry(name, current, info); err != nil {
			return "", nil, err
		}
	}

	info, err := os.Stat(full)
	if err != nil {
		return "", nil, err
	}
	return full, info, checkType(name, info)
}

func (r *FS) checkEntry(name, current string, info fs.FileInfo) error {
	if info.Mode()&fs.ModeSymlink != 0 {
		if r.rejectSymlinks {
			return fmt.Errorf("registry path %q is a symlink; remote registries do not allow symlinks", name)
		}
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil {
			return fmt.Errorf("registry path %q has an unresolved symlink: %w", name, err)
		}
		if !within(r.rootReal, resolved) {
			return fmt.Errorf("registry path %q resolves outside the registry root", name)
		}
		targetInfo, err := os.Stat(current)
		if err != nil {
			return fmt.Errorf("registry path %q has an unresolved symlink: %w", name, err)
		}
		return checkType(name, targetInfo)
	}
	return checkType(name, info)
}

func checkType(name string, info fs.FileInfo) error {
	if info.IsDir() || info.Mode().IsRegular() {
		return nil
	}
	return fmt.Errorf("registry path %q has unsupported file type %s; only regular files and directories are allowed", name, info.Mode().Type())
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

type fileWrapper struct {
	*os.File
	registry *FS
	name     string
}

func (f *fileWrapper) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.File.ReadDir(n)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		child := filepath.ToSlash(filepath.Join(filepath.FromSlash(f.name), entry.Name()))
		if f.name == "." {
			child = entry.Name()
		}
		if _, _, err := f.registry.validate(child); err != nil {
			return nil, err
		}
	}
	return entries, nil
}
