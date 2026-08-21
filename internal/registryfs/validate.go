package registryfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// skipValidationDirs lists directory names an eager tree validation does not
// descend into. `.git` is metadata this tool created by cloning; it is never
// ingested, it dominates the size of a real registry, and walking it turns a
// policy check into a full-repository stat storm. Nothing inside it can reach
// generated output, because every read goes through the confined FS anyway.
var skipValidationDirs = map[string]struct{}{
	".git": {},
}

// ValidateTree eagerly reports the first entry in root that this package would
// refuse to read. It exists for remote registries, where the whole tree is
// untrusted content and the operator deserves the diagnostic before ingestion
// decides which files it happens to open.
//
// Results are deliberately not cached across calls: a registry cache directory
// is rewritten in place by clone/pull, so a cached verdict could outlive the
// bytes it described. The walk skips `.git` (see skipValidationDirs) and costs
// one readdir per directory; BenchmarkValidateTree tracks that cost.
func ValidateTree(root string, rejectSymlinks bool) error {
	fsys, err := New(root, rejectSymlinks)
	if err != nil {
		return err
	}
	defer func() { _ = fsys.Close() }()
	return fsys.validateTree(".")
}

func (r *FS) validateTree(dir string) error {
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
		if _, skip := skipValidationDirs[entry.Name()]; skip {
			continue
		}
		if err := r.validateTree(join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ValidateSelection reports whether a single path inside root is readable under
// this package's policy, and whether it exists at all. A missing path is not an
// error: discovery reports a misconfigured selection with a better message than
// a containment check can, and the caller needs to know so it does not walk a
// tree that is not there.
func ValidateSelection(root, name string, rejectSymlinks bool) (exists bool, err error) {
	fsys, err := New(root, rejectSymlinks)
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

// ValidatePath is ValidateSelection without the existence answer.
func ValidatePath(root, name string, rejectSymlinks bool) error {
	_, err := ValidateSelection(root, name, rejectSymlinks)
	return err
}

// Subtree opens a confined view for reading dir, and returns the name of dir
// inside that view.
//
// When registryRoot contains dir, the registry root is the confinement root, so
// a symlink pointing at another resource in the same registry keeps working.
// When registryRoot is unknown or unrelated (a synthesized entry, a test
// fixture, a caller with no registry provenance), dir itself becomes the root:
// less is readable, never more, so a caller that cannot prove provenance still
// cannot read through an escaping symlink.
func Subtree(registryRoot, dir string, rejectSymlinks bool) (*FS, string, error) {
	if root, rel, ok := subtreeRel(registryRoot, dir); ok {
		fsys, err := New(root, rejectSymlinks)
		if err == nil {
			return fsys, rel, nil
		}
	}
	fsys, err := New(dir, rejectSymlinks)
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

// OpenResourceFile opens a single registry file for reading, confined to
// registryRoot (or to the file's own directory when registryRoot is unknown).
// When path names a directory, its SKILL.md is opened instead, matching how a
// resource path is addressed elsewhere in the CLI.
//
// The caller closes the returned file. The confinement root is released
// immediately; an already-open descriptor stays valid.
func OpenResourceFile(registryRoot, path string) (fs.File, error) {
	if path == "" {
		return nil, fmt.Errorf("empty registry path")
	}
	dir, name := filepath.Dir(path), filepath.Base(path)
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		dir, name = path, "SKILL.md"
	}
	fsys, rel, err := Subtree(registryRoot, dir, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fsys.Close() }()
	return fsys.Open(join(rel, filepath.ToSlash(name)))
}

// ReadResourceFile reads a single registry file under the same rules as
// OpenResourceFile.
func ReadResourceFile(registryRoot, path string) ([]byte, string, error) {
	if path == "" {
		return nil, "", fmt.Errorf("empty registry path")
	}
	dir, name := filepath.Dir(path), filepath.Base(path)
	sourceFile := path
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		dir, name = path, "SKILL.md"
		sourceFile = filepath.Join(path, "SKILL.md")
	}
	fsys, rel, err := Subtree(registryRoot, dir, false)
	if err != nil {
		return nil, sourceFile, err
	}
	defer func() { _ = fsys.Close() }()
	data, err := fsys.ReadFile(join(rel, filepath.ToSlash(name)))
	return data, sourceFile, err
}
