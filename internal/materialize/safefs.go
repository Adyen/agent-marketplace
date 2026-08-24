package materialize

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Adyen/agent-marketplace/internal/cache"
)

// confinedFS is the only filesystem adapter used for harness output. It holds
// an open directory handle for the canonical harness root and resolves every
// destination relative to that handle, so a path cannot be swapped between
// inspection and mutation.
//
// It enforces two distinct boundaries:
//
//   - Nothing outside the root can be reached. os.Root refuses any path that
//     leaves the opened directory, including via a symlink or "..".
//   - No directory component BELOW the root may be a symlink. os.Root by itself
//     permits an in-root symlink, because its target is still in-root — not
//     enough here, since a harness root is repository-controlled content. A
//     committed `skills -> commands` link would otherwise silently redirect
//     managed writes into a directory the plan never named.
type confinedFS struct {
	root *os.Root
	// abs is the harness root as the caller named it. Output paths handed to
	// this type are built from the same unresolved string, so relative-path
	// arithmetic has to use it rather than the canonical form.
	abs string
	// prefix is the part of abs that did not exist when the root was opened, so
	// a dry run can report on a machine that has never synced. It is empty in the
	// normal case, where root refers to abs itself.
	prefix           string
	trustedReadRoots []trustedReadRoot
	seq              atomic.Uint64

	// closed keeps the open-root accounting in openConfinedRoots correct when
	// Close is reached twice, e.g. explicitly and again through a defer.
	closed atomic.Bool

	// dirs caches one verified handle per root-relative directory, keyed by slash
	// path. A sync writes many artifacts into a handful of directories, and
	// re-proving every component for each of them is pure repeated openat/fstat:
	// see BenchmarkConfinedFSLstat and BenchmarkConfinedFSAtomicWrite.
	//
	// Caching is not a weakening. A handle is precisely what makes a write immune
	// to later path substitution, and every cached handle was verified against
	// the directory it was opened from when it was created. Entries are dropped
	// when this type removes the directory, so a path that is created again is
	// proven again.
	dirsMu sync.Mutex
	dirs   map[string]*os.Root
}

// trustedReadRoot is a read-only root that managed symlinks are allowed to point
// into — in practice the registry cache.
//
// paths holds every absolute spelling of that one directory, because the two
// sides of the comparison are produced independently: a symlink on disk was
// written from the configured path (cache.CacheDir()), while the same directory
// reached through resolved ancestors is spelled differently on a machine with a
// symlinked $HOME or under macOS's /var alias. Matching only one spelling
// silently reclassifies our own symlinks as user-owned, which makes sync stop
// pruning and stop replacing them.
type trustedReadRoot struct {
	root  *os.Root
	paths []string
}

func (t trustedReadRoot) relFor(candidate string) (string, bool) {
	for _, root := range t.paths {
		if rel, ok := relativeWithin(root, candidate); ok {
			return rel, true
		}
	}
	return "", false
}

func newTrustedReadRoot(root *os.Root, spellings ...string) trustedReadRoot {
	trusted := trustedReadRoot{root: root}
	for _, spelling := range spellings {
		if spelling == "" || slices.Contains(trusted.paths, spelling) {
			continue
		}
		trusted.paths = append(trusted.paths, spelling)
	}
	return trusted
}

// openConfinedRoots counts confinedFS values that have been opened and not yet
// closed. A control-flow path that returns without closing leaks a directory
// file descriptor for the rest of the process, which stays invisible until a
// large sync exhausts the descriptor table; tests assert this returns to zero
// after a run, including runs that refuse a target and exit early.
var openConfinedRoots atomic.Int64

func openConfinedFS(path string, create bool, trustedReadRoots ...string) (*confinedFS, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve output root %q: %w", path, err)
	}
	abs = filepath.Clean(abs)
	root, prefix, err := openDirectoryRoot(abs, create)
	if err != nil {
		return nil, err
	}
	f := &confinedFS{root: root, abs: abs, prefix: prefix}
	openConfinedRoots.Add(1)
	roots := append([]string{cache.CacheDir()}, trustedReadRoots...)
	seen := make(map[string]bool, len(roots))
	for _, trustedPath := range roots {
		trustedPath, err = filepath.Abs(trustedPath)
		if err != nil {
			continue
		}
		trustedPath = filepath.Clean(trustedPath)
		if seen[trustedPath] || trustedPath == abs {
			continue
		}
		seen[trustedPath] = true
		trusted, ok := openTrustedReadRoot(trustedPath)
		if ok {
			f.trustedReadRoots = append(f.trustedReadRoots, trusted)
		}
	}
	return f, nil
}

// openDirectoryRoot opens abs as a confinement root, returning the handle and
// any trailing path suffix that does not exist yet.
//
// The PARENT of abs is resolved by the kernel in a single open. That resolution
// follows symlinks, which is deliberate: a symlinked $HOME, a symlinked checkout
// directory, and macOS's /var -> /private/var are ordinary deployment shapes, and
// refusing them left sync unable to write anything at all on such a machine.
// Directories above the harness root are the user's own filesystem, and
// repository content cannot plant them.
//
// The FINAL component is not resolved. It is the boundary this type enforces, it
// IS repository-controlled (`.factory`, `.windsurf`, `.devin` all sit in the
// checkout), and a symlink there — a committed `.windsurf -> .git` — redirects
// every managed write for that target at once. It is inspected, refused when it
// is a symlink, and the handle opened for it is proven to be the directory that
// was inspected.
func openDirectoryRoot(abs string, create bool) (*os.Root, string, error) {
	parent, leaf := filepath.Split(abs)
	parent = filepath.Clean(parent)
	if leaf == "" {
		// abs is a filesystem root, so it has no final component to refuse.
		root, err := os.OpenRoot(abs)
		if err != nil {
			return nil, "", fmt.Errorf("open output root %q: %w", abs, err)
		}
		return root, "", nil
	}
	if create {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, "", fmt.Errorf("create output root parent %q: %w", parent, err)
		}
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		if !create && os.IsNotExist(err) {
			// Nothing on this path exists yet, e.g. a --check run on a machine
			// that has never synced. Anchor on the deepest existing ancestor and
			// let every later operation resolve the rest through it, so the
			// missing components are component-checked once they do exist.
			upRoot, upPrefix, upErr := openDirectoryRoot(parent, false)
			if upErr != nil {
				return nil, "", upErr
			}
			return upRoot, filepath.Join(upPrefix, leaf), nil
		}
		return nil, "", fmt.Errorf("open output root parent %q: %w", parent, err)
	}
	info, statErr := parentRoot.Lstat(leaf)
	if os.IsNotExist(statErr) {
		if !create {
			return parentRoot, leaf, nil
		}
		if mkdirErr := parentRoot.Mkdir(leaf, 0o755); mkdirErr != nil && !os.IsExist(mkdirErr) {
			_ = parentRoot.Close()
			return nil, "", fmt.Errorf("create output root %q: %w", abs, mkdirErr)
		}
		info, statErr = parentRoot.Lstat(leaf)
	}
	if statErr != nil {
		_ = parentRoot.Close()
		return nil, "", fmt.Errorf("inspect output root %q: %w", abs, statErr)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		_ = parentRoot.Close()
		return nil, "", fmt.Errorf("output root %q is a symlink", abs)
	}
	root, openErr := openVerifiedChild(parentRoot, leaf, info)
	_ = parentRoot.Close()
	if openErr != nil {
		return nil, "", openErr
	}
	return root, "", nil
}

// openVerifiedChild opens component below parent and proves the returned handle
// refers to the very directory that was inspected, closing the TOCTOU window
// between the Lstat that classified it and the handle every later write uses.
func openVerifiedChild(parent *os.Root, component string, info os.FileInfo) (*os.Root, error) {
	label := filepath.Join(parent.Name(), component)
	if !info.IsDir() {
		return nil, fmt.Errorf("output path component %q is not a directory", label)
	}
	child, err := parent.OpenRoot(component)
	if err != nil {
		return nil, fmt.Errorf("open output path component %q: %w", label, err)
	}
	opened, statErr := child.Stat(".")
	if statErr != nil || !os.SameFile(info, opened) {
		_ = child.Close()
		if statErr == nil {
			statErr = errors.New("directory changed while opening")
		}
		return nil, fmt.Errorf("open output path component %q: %w", label, statErr)
	}
	return child, nil
}

// normalizeSystemPath maps macOS's /var alias onto its canonical /private/var
// form so a symlink target written in one form still matches a trusted read
// root recorded in the other.
func normalizeSystemPath(abs string) string {
	if runtime.GOOS == "darwin" && (abs == string(filepath.Separator)+"var" || strings.HasPrefix(abs, string(filepath.Separator)+"var"+string(filepath.Separator))) {
		return filepath.Join(string(filepath.Separator)+"private", abs)
	}
	return abs
}

func openTrustedReadRoot(abs string) (trustedReadRoot, bool) {
	root, prefix, err := openDirectoryRoot(abs, false)
	if err != nil {
		return trustedReadRoot{}, false
	}
	// Best effort: a resolved spelling only widens what is recognized as ours.
	resolved, _ := filepath.EvalSymlinks(abs)
	if prefix != "" {
		// The cache has not been populated yet. There is nothing to read
		// through, but a link naming it is still ours, so keep the spellings.
		_ = root.Close()
		return newTrustedReadRoot(nil, abs, normalizeSystemPath(abs), resolved), true
	}
	return newTrustedReadRoot(root, abs, normalizeSystemPath(abs), resolved), true
}

func (f *confinedFS) Close() error {
	var err error
	if f.closed.CompareAndSwap(false, true) {
		openConfinedRoots.Add(-1)
	}
	f.dirsMu.Lock()
	for key, root := range f.dirs {
		err = errors.Join(err, root.Close())
		delete(f.dirs, key)
	}
	f.dirsMu.Unlock()
	if f.root != nil {
		err = errors.Join(err, f.root.Close())
	}
	for _, trusted := range f.trustedReadRoots {
		if trusted.root != nil {
			err = errors.Join(err, trusted.root.Close())
		}
	}
	return err
}

func (f *confinedFS) absPath(rel string) string {
	if f.prefix != "" {
		prefix := filepath.ToSlash(filepath.Clean(f.prefix))
		if rel == prefix {
			rel = "."
		} else if strings.HasPrefix(rel, prefix+"/") {
			rel = strings.TrimPrefix(rel, prefix+"/")
		}
	}
	return filepath.Join(f.abs, filepath.FromSlash(rel))
}

func (f *confinedFS) rel(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve output path %q: %w", p, err)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(f.abs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("output path %q is outside root %q", p, f.abs)
	}
	if f.prefix != "" {
		rel = filepath.Join(filepath.FromSlash(f.prefix), rel)
	}
	return filepath.ToSlash(rel), nil
}

// Contains reports whether p is a path this root may mutate. It answers the
// lexical question only, so callers can refuse a path from untrusted input
// (a committed materialization manifest) with their own diagnostic instead of
// discovering it as an opaque I/O error mid-write.
func (f *confinedFS) Contains(p string) bool {
	_, err := f.rel(p)
	return err == nil
}

// dirRoot resolves a slash-separated directory path below the root to an open
// handle, refusing every symlinked component on the way down. Handles are owned
// by the cache, so callers must not close what they get back.
func (f *confinedFS) dirRoot(relDir string, create bool, perm fs.FileMode) (*os.Root, error) {
	if perm == 0 {
		perm = 0o755
	}
	relDir = path.Clean(relDir)
	if relDir == "" || relDir == "." {
		return f.root, nil
	}
	f.dirsMu.Lock()
	defer f.dirsMu.Unlock()
	if cached, ok := f.dirs[relDir]; ok {
		return cached, nil
	}
	components := strings.Split(relDir, "/")
	current, walked := f.root, ""
	// Resume from the deepest already-proven prefix instead of re-walking from
	// the root, which is what keeps a directory's components proven once per run
	// rather than once per artifact written into it.
	for i := len(components) - 1; i > 0; i-- {
		prefix := strings.Join(components[:i], "/")
		if cached, ok := f.dirs[prefix]; ok {
			current, walked, components = cached, prefix, components[i:]
			break
		}
	}
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		next, err := openDirChild(current, component, create, perm)
		if err != nil {
			return nil, err
		}
		walked = path.Join(walked, component)
		if f.dirs == nil {
			f.dirs = make(map[string]*os.Root)
		}
		f.dirs[walked] = next
		current = next
	}
	return current, nil
}

// openDirChild opens one directory component, refusing a symlink there.
func openDirChild(parent *os.Root, component string, create bool, perm fs.FileMode) (*os.Root, error) {
	info, err := parent.Lstat(component)
	if os.IsNotExist(err) && create {
		if mkdirErr := parent.Mkdir(component, perm.Perm()); mkdirErr != nil && !os.IsExist(mkdirErr) {
			return nil, mkdirErr
		}
		info, err = parent.Lstat(component)
	}
	if err != nil {
		// Returned unwrapped: callers classify missing directories with
		// os.IsNotExist, which does not unwrap fmt.Errorf.
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("output path component %q is a symlink", filepath.Join(parent.Name(), component))
	}
	return openVerifiedChild(parent, component, info)
}

// forgetDir drops cached handles for a removed directory and everything under
// it, so a path that is created again is proven again rather than written
// through a handle to the directory that used to be there.
func (f *confinedFS) forgetDir(rel string) {
	rel = path.Clean(rel)
	if rel == "" || rel == "." {
		return
	}
	f.dirsMu.Lock()
	defer f.dirsMu.Unlock()
	for key, root := range f.dirs {
		if key == rel || strings.HasPrefix(key, rel+"/") {
			_ = root.Close()
			delete(f.dirs, key)
		}
	}
}

// resolveRel splits an already-root-relative path into an open handle for its
// parent directory and the leaf name inside it.
func (f *confinedFS) resolveRel(rel string, create bool) (*os.Root, string, error) {
	dirRel, base := path.Split(path.Clean(rel))
	dirRel = strings.TrimSuffix(dirRel, "/")
	if base == "" {
		base = "."
	}
	dir, err := f.dirRoot(dirRel, create, 0o755)
	if err != nil {
		return nil, "", err
	}
	return dir, base, nil
}

func (f *confinedFS) resolve(p string, create bool) (*os.Root, string, error) {
	rel, err := f.rel(p)
	if err != nil {
		return nil, "", err
	}
	return f.resolveRel(rel, create)
}

func (f *confinedFS) Lstat(p string) (os.FileInfo, error) {
	dir, base, err := f.resolve(p, false)
	if err != nil {
		return nil, err
	}
	return dir.Lstat(base)
}

// The *Rel accessors below take a path that is already root-relative, for the
// recursive digest walk that carries its own relative cursor. They go through
// the same component-checked descent as every other accessor, so hashing an
// output cannot traverse a planted directory symlink either.

func (f *confinedFS) lstatRel(rel string) (os.FileInfo, error) {
	dir, base, err := f.resolveRel(rel, false)
	if err != nil {
		return nil, err
	}
	return dir.Lstat(base)
}

func (f *confinedFS) readFileRel(rel string) ([]byte, error) {
	dir, base, err := f.resolveRel(rel, false)
	if err != nil {
		return nil, err
	}
	return dir.ReadFile(base)
}

func (f *confinedFS) readDirRel(rel string) ([]os.DirEntry, error) {
	dir, err := f.dirRoot(rel, false, 0)
	if err != nil {
		return nil, err
	}
	file, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	entries, err := file.ReadDir(-1)
	// os.ReadDir — which every caller here was written against — returns
	// entries sorted by name, while File.ReadDir returns them in directory
	// order. Callers derive report rows, prune decisions, and diagnostics from
	// this slice, so unsorted output makes all three vary between machines and
	// between runs on the same machine.
	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})
	return entries, err
}

func (f *confinedFS) readlinkRel(rel string) (string, error) {
	dir, base, err := f.resolveRel(rel, false)
	if err != nil {
		return "", err
	}
	return dir.Readlink(base)
}

func (f *confinedFS) Stat(p string) (os.FileInfo, error) {
	dir, base, err := f.resolve(p, false)
	if err != nil {
		return nil, err
	}
	return dir.Stat(base)
}

func (f *confinedFS) ReadFile(p string) ([]byte, error) {
	dir, base, err := f.resolve(p, false)
	if err != nil {
		return nil, err
	}
	return dir.ReadFile(base)
}

// errMergeSourceMissing reports that there is nothing at a merge destination to
// merge with: no file, or a symlink that resolves to no file at all. Callers
// merge from empty content and write a fresh regular file over the link.
var errMergeSourceMissing = errors.New("merge source does not exist")

// externalMergeSourceError is a merge destination that is a symlink to a file
// which does exist but is outside every root this process may read from.
//
// It is refused rather than replaced. Reading it would let repository-controlled
// content seed the merged output with attacker-chosen entries; silently replacing
// it would destroy a deliberate redirection (a user pointing mcp.json at their
// dotfiles) whose content we can see is real.
type externalMergeSourceError struct {
	path   string
	target string
}

func (e *externalMergeSourceError) Error() string {
	return fmt.Sprintf("merge source symlink %q resolves to %q, outside the harness root and outside the registry cache", e.path, e.target)
}

func (f *confinedFS) readMergeSource(p string) ([]byte, error) {
	info, err := f.Lstat(p)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return f.ReadFile(p)
	}

	// An in-root symlink is read through the root handle; anything pointing out
	// of it is only readable when it lands in a trusted cache root, so a
	// planted link to an arbitrary file cannot seed a merge.
	if data, readErr := f.ReadFile(p); readErr == nil {
		return data, nil
	}
	target, err := f.Readlink(p)
	if err != nil {
		return nil, err
	}
	candidate := filepath.Clean(target)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(filepath.Dir(p), candidate)
	}
	for _, trusted := range f.trustedReadRoots {
		if trusted.root == nil {
			continue
		}
		trustedRel, ok := trusted.relFor(candidate)
		if !ok {
			continue
		}
		data, readErr := trusted.root.ReadFile(trustedRel)
		if readErr != nil {
			return nil, readErr
		}
		return data, nil
	}
	// Metadata only, and only to classify: a link with a real file behind it is
	// the user's and is refused, a link with nothing behind it is dead weight
	// this run replaces. Nothing is read through the resolved path, and the
	// write that follows renames over the LINK, so the file at the far end of a
	// misclassified link is still never touched.
	if _, statErr := os.Stat(candidate); statErr != nil {
		return nil, errMergeSourceMissing
	}
	return nil, &externalMergeSourceError{path: p, target: candidate}
}

func relativeWithin(root, target string) (string, bool) {
	rel, err := filepath.Rel(root, filepath.Clean(target))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (f *confinedFS) ReadDir(p string) ([]os.DirEntry, error) {
	rel, err := f.rel(p)
	if err != nil {
		return nil, err
	}
	return f.readDirRel(rel)
}

func (f *confinedFS) Readlink(p string) (string, error) {
	dir, base, err := f.resolve(p, false)
	if err != nil {
		return "", err
	}
	return dir.Readlink(base)
}

func (f *confinedFS) ExistingSymlinkTarget(p string) (string, bool) {
	info, err := f.Lstat(p)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return "", false
	}
	target, err := f.Readlink(p)
	return target, err == nil
}

func (f *confinedFS) IsManagedSymlink(p string) bool {
	target, ok := f.ExistingSymlinkTarget(p)
	if !ok {
		return false
	}
	candidate := filepath.Clean(target)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(filepath.Dir(p), candidate)
	}
	for _, trusted := range f.trustedReadRoots {
		if _, ok := trusted.relFor(candidate); ok {
			return true
		}
	}
	return false
}

func (f *confinedFS) MkdirAll(p string, perm fs.FileMode) error {
	rel, err := f.rel(p)
	if err != nil {
		return err
	}
	if _, err := f.dirRoot(rel, true, perm); err != nil {
		return err
	}
	return nil
}

// EnsureRoot creates the confinement root itself. Callers materialize into
// subdirectories of it, and an absent root would otherwise only surface as a
// per-plan failure.
func (f *confinedFS) EnsureRoot() error {
	if f.prefix == "" {
		return nil
	}
	if _, err := f.dirRoot(filepath.ToSlash(f.prefix), true, 0o755); err != nil {
		return err
	}
	return nil
}

func (f *confinedFS) EnsureDir(p string, dryRun bool) error {
	info, err := f.Lstat(p)
	if err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			err := fmt.Errorf("output path %q is a symlink", p)
			logMaterializeOutputFailure("ensure-directory", p, err)
			return err
		}
		if !info.IsDir() {
			err := fmt.Errorf("output path %q is not a directory", p)
			logMaterializeOutputFailure("ensure-directory", p, err)
			return err
		}
		return nil
	}
	if !os.IsNotExist(err) {
		logMaterializeOutputFailure("inspect-directory", p, err)
		return err
	}
	if dryRun {
		return nil
	}
	if err := f.MkdirAll(p, 0o755); err != nil {
		logMaterializeOutputFailure("create-directory", p, err)
		return err
	}
	return nil
}

func (f *confinedFS) Remove(p string) error {
	dir, base, err := f.resolve(p, false)
	if err != nil {
		return err
	}
	if base == "." {
		return fmt.Errorf("refusing to remove output root %q", f.abs)
	}
	if err := dir.Remove(base); err != nil {
		return err
	}
	f.forgetRemoved(p)
	return nil
}

func (f *confinedFS) RemoveAll(p string) error {
	dir, base, err := f.resolve(p, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if base == "." {
		return fmt.Errorf("refusing to remove output root %q", f.abs)
	}
	if err := dir.RemoveAll(base); err != nil {
		return err
	}
	f.forgetRemoved(p)
	return nil
}

// forgetRemoved invalidates any cached handle for a path this type just deleted.
func (f *confinedFS) forgetRemoved(p string) {
	if rel, err := f.rel(p); err == nil {
		f.forgetDir(rel)
	}
}

func (f *confinedFS) Symlink(target, p string) error {
	dir, base, err := f.resolve(p, false)
	if err != nil {
		return err
	}
	return dir.Symlink(target, base)
}

func (f *confinedFS) AtomicSymlink(target, p string) error {
	dir, base, err := f.resolve(p, true)
	if err != nil {
		return err
	}
	tmp, err := f.tempName()
	if err != nil {
		return err
	}
	if err := dir.Symlink(target, tmp); err != nil {
		return err
	}
	if err := dir.Rename(tmp, base); err != nil {
		_ = dir.Remove(tmp)
		return err
	}
	return nil
}

func (f *confinedFS) AtomicWrite(p string, data []byte, mode fs.FileMode) error {
	dir, base, err := f.resolve(p, true)
	if err != nil {
		return err
	}
	return f.atomicWriteAt(dir, base, data, mode, false)
}

// AtomicWriteReconciling writes data at p, but only when the bytes there differ,
// and keeps the permission bits the destination already has. It reports whether
// it wrote.
//
// It exists for the shared harness configs this package merges into rather than
// generates — mcp.json, hooks.json, plugin.json, settings.json — which are the
// files that carry MCP credentials:
//
//   - Mode is preserved exactly (fchmod on the temp file, so umask cannot widen
//     it either). A 0600 credential file that came back 0644 after a sync was a
//     silent permission downgrade of the user's secrets, applied on every run.
//   - An unchanged file is not rewritten at all. Replacing the inode on a no-op
//     sync churned mtime, invalidated anything watching the file, and briefly
//     exposed a second copy of the credentials; and every sync is a no-op for
//     these files once the desired entries are in place.
//
// fallback is used only when nothing is there yet.
func (f *confinedFS) AtomicWriteReconciling(p string, data []byte, fallback fs.FileMode) (bool, error) {
	dir, base, err := f.resolve(p, true)
	if err != nil {
		return false, err
	}
	mode, exact := fallback.Perm(), false
	if info, statErr := dir.Lstat(base); statErr == nil && info.Mode().IsRegular() {
		mode, exact = info.Mode().Perm(), true
		if current, readErr := dir.ReadFile(base); readErr == nil && bytes.Equal(current, data) {
			return false, nil
		}
	}
	return true, f.atomicWriteAt(dir, base, data, mode, exact)
}

// atomicWriteAt writes data to base inside the already-verified dir handle via a
// temporary file and a rename. When exact is set the temporary file's mode is
// fchmod'ed to mode, which the O_CREAT perm argument alone cannot guarantee
// because the process umask masks it.
func (f *confinedFS) atomicWriteAt(dir *os.Root, base string, data []byte, mode fs.FileMode, exact bool) error {
	for range 32 {
		tmpName, err := f.tempName()
		if err != nil {
			return err
		}
		tmp, err := dir.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return err
		}
		if exact {
			if chmodErr := tmp.Chmod(mode.Perm()); chmodErr != nil {
				_ = tmp.Close()
				_ = dir.Remove(tmpName)
				return chmodErr
			}
		}
		if writeErr := writeAndClose(tmp, data); writeErr != nil {
			_ = dir.Remove(tmpName)
			return writeErr
		}
		if err := dir.Rename(tmpName, base); err != nil {
			_ = dir.Remove(tmpName)
			return err
		}
		return nil
	}
	return fmt.Errorf("could not allocate a unique temporary output path")
}

func (f *confinedFS) tempName() (string, error) {
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate temporary output path: %w", err)
	}
	return fmt.Sprintf(".agent-marketplace-%x-%d", token, f.seq.Add(1)), nil
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

// Walk validates the directory chain down to p and then walks it. fs.WalkDir
// never follows a symlink itself, so verifying the descent to the walk root is
// what keeps the traversal inside the canonical harness root.
func (f *confinedFS) Walk(p string, fn fs.WalkDirFunc) error {
	rel, err := f.rel(p)
	if err != nil {
		return err
	}
	if _, err := f.dirRoot(rel, false, 0); err != nil {
		return err
	}
	return fs.WalkDir(f.root.FS(), rel, fn)
}
