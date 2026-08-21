package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/Adyen/agent-marketplace/internal/registryfs"
)

// sourceTree is materialization's only door to registry bytes.
//
// Compiled artifacts arrive as in-memory bytes, but support files (a skill's
// scripts/, a hook's helper executables) are copied straight from the registry
// into the cache or the generated output. Reading them by path with os.ReadFile
// let a registry hand over any file the user could read — a symlink to an SSH
// key was copied verbatim — and let it stall a sync with a FIFO. Every such read
// now resolves through registryfs, which confines it to the registry root at the
// point of use and refuses anything that is not a regular file or directory.
type sourceTree struct {
	fsys *registryfs.FS
	base string // slash path of the resource source directory inside fsys
	dir  string // absolute source directory, for messages
}

// openSourceTree confines reads for sourceDir under the policy of the registry
// the resource was discovered in: reads stay inside that registry's root, and a
// remote registry's symlink is refused here even though it is contained, because
// the eager validation that ran at discovery time cannot speak for the bytes this
// copy is about to read. When the registry root is unknown, sourceDir itself
// becomes the confinement root, which is narrower and never wider.
func openSourceTree(source registryfs.Policy, sourceDir string) (*sourceTree, error) {
	fsys, base, err := source.Subtree(sourceDir)
	if err != nil {
		return nil, err
	}
	return &sourceTree{fsys: fsys, base: base, dir: sourceDir}, nil
}

func (t *sourceTree) Close() {
	if t == nil {
		return
	}
	_ = t.fsys.Close()
}

func (t *sourceTree) name(rel string) string {
	if rel == "" || rel == "." {
		return t.base
	}
	if t.base == "." {
		return rel
	}
	return t.base + "/" + rel
}

func (t *sourceTree) ReadDir(rel string) ([]fs.DirEntry, error) {
	return t.fsys.ReadDir(t.name(rel))
}

// ReadDirLenient lists a directory and reports refused entries separately, for
// callers that install files one by one and can skip one without abandoning the
// others.
func (t *sourceTree) ReadDirLenient(rel string) ([]fs.DirEntry, []error, error) {
	return t.fsys.ReadDirLenient(t.name(rel))
}

// IsDir resolves an entry, following contained symlinks. A symlinked support
// directory inside the same registry is legitimate; one pointing outside fails
// here rather than being copied.
func (t *sourceTree) IsDir(rel string) (bool, error) {
	info, err := t.fsys.Stat(t.name(rel))
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func (t *sourceTree) ReadFile(rel string) ([]byte, fs.FileMode, error) {
	return t.fsys.ReadFileMode(t.name(rel))
}

// childRel joins a directory-relative path for messages and compiled-artifact
// lookups, which are keyed relative to the resource root.
func childRel(rel, name string) string {
	if rel == "" || rel == "." {
		return name
	}
	return path.Join(rel, name)
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

func supportReadError(dir, rel string, err error) error {
	return fmt.Errorf("read registry support file %s in %s: %w", rel, dir, err)
}
