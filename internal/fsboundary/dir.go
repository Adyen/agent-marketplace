package fsboundary

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Dir is a captured directory: an open descriptor plus the identity and
// canonical path recorded at capture time. Every operation on a Dir is
// relative to the descriptor, so renaming or replacing the directory's
// path afterwards cannot redirect the operation; it stays bound to the
// captured object.
type Dir struct {
	file *os.File
	path string
	id   Identity
}

// OpenDir captures the directory at path as an authorized root. The path
// must be absolute; it is canonicalized once (symlinks in the root path are
// honored at capture time only) and the canonical directory is opened and
// pinned by identity. A missing path reports fs.ErrNotExist; a non-directory
// root is ErrUnsafePath.
func OpenDir(path string) (*Dir, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: root must be an absolute path: %s", ErrUnsafePath, path)
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("resolve root %s: %w", path, err)
	}
	file, err := openDirectoryPath(canonical)
	if err != nil {
		return nil, err
	}
	id, err := identityOf(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect root %s: %w", canonical, err)
	}
	if !id.Mode.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("%w: root is not a directory: %s", ErrUnsafePath, canonical)
	}
	return &Dir{file: file, path: canonical, id: id}, nil
}

// Path returns the canonical path captured at open time. It is recorded for
// diagnostics and containment decisions; no operation resolves it again.
func (d *Dir) Path() string { return d.path }

// Identity returns the directory identity captured at open time.
func (d *Dir) Identity() Identity { return d.id }

// Close releases the captured descriptor.
func (d *Dir) Close() error { return d.file.Close() }

// Sync flushes the captured directory's metadata to stable storage where the
// platform can do so.
func (d *Dir) Sync() error { return syncDir(d.file) }

func (d *Dir) Stat() (os.FileInfo, error) { return d.file.Stat() }

func (d *Dir) Chmod(mode os.FileMode) error {
	if err := d.file.Chmod(mode); err != nil {
		return fmt.Errorf("chmod captured directory %s: %w", d.path, err)
	}
	return nil
}

// errLeafSymlink marks a no-follow leaf open that stopped at a symlink. It
// is always wrapped together with a stable public refusal; destination
// capture uses it to apply the caller's link policy instead of refusing.
var errLeafSymlink = errors.New("fsboundary: leaf is a symlink")

// splitRelative validates a root-relative path and returns its components.
// Absolute paths, escapes, and empty or dot components are unsafe, as is any
// component the platform refuses by name.
func splitRelative(rel string) ([]string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("%w: path must be relative: %s", ErrUnsafePath, rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: path escapes its root: %s", ErrUnsafePath, rel)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("%w: path escapes its root: %s", ErrUnsafePath, rel)
		}
		if err := checkComponentName(part); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

// validLeafName rejects anything that is not a single relative path
// component.
func validLeafName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: invalid leaf name %q", ErrUnsafePath, name)
	}
	return checkComponentName(name)
}

// validateLeafIdentity enforces the shared leaf contract: a regular file
// with exactly one link.
func validateLeafIdentity(id Identity, name string) error {
	if !id.Mode.IsRegular() {
		return fmt.Errorf("%w: leaf is not a regular file: %s", ErrUnsafeFile, name)
	}
	if id.Links != 1 {
		return fmt.Errorf("%w: leaf has %d links: %s", ErrUnsafeFile, id.Links, name)
	}
	return nil
}

// openRelative walks rel below d and opens its leaf. Intermediate components
// must be real directories; the leaf must be a singly linked regular file.
func (d *Dir) openRelative(rel string) (*os.File, Identity, error) {
	parts, err := splitRelative(rel)
	if err != nil {
		return nil, Identity{}, err
	}
	current := d.file
	for _, part := range parts[:len(parts)-1] {
		next, err := openComponent(current, part)
		if err != nil {
			if current != d.file {
				_ = current.Close()
			}
			return nil, Identity{}, err
		}
		if current != d.file {
			_ = current.Close()
		}
		current = next
	}
	if current != d.file {
		defer func() { _ = current.Close() }()
	}
	return openLeaf(current, parts[len(parts)-1])
}

// OpenFile opens rel below the captured directory without following links,
// validates that the opened object is a singly linked regular file, and
// returns the open file with its captured identity. Reads on the returned
// file block normally.
func (d *Dir) OpenFile(rel string) (*os.File, Identity, error) {
	return d.openRelative(rel)
}

// FileIdentity captures the identity of an already opened file through its
// descriptor.
func FileIdentity(file *os.File) (Identity, error) {
	return identityOf(file)
}
