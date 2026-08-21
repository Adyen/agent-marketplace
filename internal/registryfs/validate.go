package registryfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// cloneMetadataDir is the one directory an eager walk of a clone root does not
// descend into. `.git` at the top of a clone is metadata this tool wrote by
// cloning; it is never ingested, it dominates the size of a real registry, and
// walking it turns a policy check into a full-object-store stat storm.
//
// The exemption is deliberately scoped to the top of a clone. A directory named
// `.git` deeper in the tree is registry-authored content — a registry can create
// one anywhere — so it is walked like any other directory.
const cloneMetadataDir = ".git"

// ValidateTree eagerly reports the first entry in p.Root that this package would
// refuse to read. It exists for remote registries, where the whole tree is
// untrusted content and the operator deserves the diagnostic before ingestion
// decides which files it happens to open.
//
// It is a diagnostic, not the containment mechanism: every later read re-applies
// the policy at the point of use, so a tree that passes here and rewrites itself
// afterwards is still refused when its bytes are actually read.
//
// Results are deliberately not cached across calls: a registry cache directory
// is rewritten in place by clone/pull, so a cached verdict could outlive the
// bytes it described. The walk costs one readdir per directory;
// BenchmarkValidateTree tracks that cost.
func (p Policy) ValidateTree() error {
	return p.validateTree(false)
}

// ValidateCloneTree is ValidateTree for a tree whose root is a clone this tool
// made, where the top-level `.git` directory is skipped (see cloneMetadataDir).
func (p Policy) ValidateCloneTree() error {
	return p.validateTree(true)
}

func (p Policy) validateTree(skipCloneMetadata bool) error {
	fsys, err := p.FS()
	if err != nil {
		return err
	}
	defer func() { _ = fsys.Close() }()
	return fsys.validateTree(".", skipCloneMetadata)
}

func (r *FS) validateTree(dir string, skipCloneMetadata bool) error {
	entries, err := r.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		// A symlink to a directory reports IsDir() == false, so recursion never
		// follows a link: containment is already decided, and not following also
		// makes the walk loop-free.
		if !entry.IsDir() {
			continue
		}
		if skipCloneMetadata && dir == "." && entry.Name() == cloneMetadataDir {
			continue
		}
		if err := r.validateTree(join(dir, entry.Name()), skipCloneMetadata); err != nil {
			return err
		}
	}
	return nil
}

// ValidateSelection reports whether a single path inside p.Root is readable
// under this policy, and whether it exists at all. A missing path is not an
// error: discovery reports a misconfigured selection with a better message than
// a containment check can, and the caller needs to know so it does not walk a
// tree that is not there.
func (p Policy) ValidateSelection(name string) (exists bool, err error) {
	fsys, err := p.FS()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = fsys.Close() }()
	if name == "" {
		name = "."
	}
	if _, err := fsys.Stat(filepath.ToSlash(name)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Subtree opens a confined view for reading dir, and returns the name of dir
// inside that view.
//
// When p.Root contains dir, the registry root is the confinement root, so a
// symlink pointing at another resource in the same registry keeps working. When
// p.Root is unknown or unrelated (a synthesized entry, a test fixture, a caller
// with no registry provenance), dir itself becomes the root: less is readable,
// never more, so a caller that cannot prove provenance still cannot read through
// an escaping symlink.
func (p Policy) Subtree(dir string) (*FS, string, error) {
	if root, rel, ok := subtreeRel(p.Root, dir); ok {
		fsys, err := p.At(root).FS()
		if err != nil {
			// Falling back to dir here would widen the root on the one path where
			// provenance is known, which is the opposite of what the fallback is
			// for. Refuse instead.
			return nil, "", err
		}
		return fsys, rel, nil
	}
	// Unknown provenance narrows confinement to dir, which only holds while dir
	// is a real directory: os.OpenRoot resolves the root path it is handed, so
	// rooting at a symlink would confine reads to the link's *target* — a tree
	// chosen by whoever planted the link, and wider than the resource directory
	// this fallback exists to fence in. Refuse regardless of RejectSymlinks: the
	// objection is the widening, not the link.
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, "", err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("registry path %q is a symlink and no registry root vouches for it; cannot confine reads to a link target", dir)
	}
	fsys, err := p.At(dir).FS()
	if err != nil {
		return nil, "", err
	}
	return fsys, ".", nil
}

func subtreeRel(registryRoot, dir string) (root, rel string, ok bool) {
	if registryRoot == "" || dir == "" {
		return "", "", false
	}
	if rel, ok := containedRel(registryRoot, dir); ok {
		return registryRoot, rel, true
	}
	// A configured root and a discovered path can name the same directory
	// through different symlinked prefixes (/var vs /private/var on macOS).
	rootReal, rootErr := filepath.EvalSymlinks(registryRoot)
	dirReal, dirErr := filepath.EvalSymlinks(dir)
	if rootErr != nil || dirErr != nil {
		return "", "", false
	}
	if rel, ok := containedRel(rootReal, dirReal); ok {
		return rootReal, rel, true
	}
	return "", "", false
}

func containedRel(root, dir string) (string, bool) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// OpenResourceFile opens a single registry file for reading, confined to p.Root
// (or to the file's own directory when p.Root is unknown). When path names a
// directory, its SKILL.md is opened instead, matching how a resource path is
// addressed elsewhere in the CLI.
//
// The caller closes the returned file. The confinement root is released
// immediately; an already-open descriptor stays valid.
func (p Policy) OpenResourceFile(path string) (fs.File, error) {
	if path == "" {
		return nil, fmt.Errorf("empty registry path")
	}
	dir, name := resourceFileParts(path)
	fsys, rel, err := p.Subtree(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fsys.Close() }()
	return fsys.Open(join(rel, filepath.ToSlash(name)))
}

// ReadResourceFile reads a single registry file under the same rules as
// OpenResourceFile, and reports the file it resolved to so a caller can name it
// in an error.
func (p Policy) ReadResourceFile(path string) ([]byte, string, error) {
	if path == "" {
		return nil, "", fmt.Errorf("empty registry path")
	}
	dir, name := resourceFileParts(path)
	sourceFile := filepath.Join(dir, name)
	fsys, rel, err := p.Subtree(dir)
	if err != nil {
		return nil, sourceFile, err
	}
	defer func() { _ = fsys.Close() }()
	data, err := fsys.ReadFile(join(rel, filepath.ToSlash(name)))
	return data, sourceFile, err
}

// resourceFileParts splits a resource path into the directory to confine reads
// to and the file to read inside it. A directory is addressed by its SKILL.md.
//
// The IsDir probe is an unconfined Lstat, which is safe because it follows
// nothing and reads no content: a symlinked path reports as a link, so it is
// treated as a file and refused by the confined read that follows.
func resourceFileParts(path string) (dir, name string) {
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		return path, "SKILL.md"
	}
	return filepath.Dir(path), filepath.Base(path)
}
