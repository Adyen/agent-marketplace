// Package registryfs provides read-only access to a registry tree that cannot
// escape the registry root.
//
// Containment is enforced at the point of use instead of by validating a path
// and then reopening it by name. Every read resolves through an [os.Root], so
// each path component is opened relative to the previous directory descriptor
// and a component resolving outside the root fails inside the same syscall
// sequence that performs the read. A registry that rewrites its own tree
// between two operations therefore cannot substitute an escaping symlink for a
// file that already passed a check.
//
// Two policies are layered on top of containment:
//
//   - Only regular files and directories are readable. The type is taken from
//     the descriptor that will be read (fstat), not from an earlier lookup by
//     name. On Unix the descriptor is opened non-blocking, so a FIFO planted in
//     a registry cannot stall the process before the type check runs — a plain
//     open(2) of a FIFO with no writer blocks forever.
//   - Remote registries refuse symlinks outright, contained or not. Nothing in
//     a tree we cloned ourselves needs one, and refusing keeps the cache
//     comparable with the upstream commit.
//
// Local registries may use symlinks freely as long as they resolve inside the
// registry root: a developer registry that shares one scripts directory across
// skills is legitimate, reading /etc/shadow through a skill is not.
package registryfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FS is a confined, read-only view of a registry tree. It implements [fs.FS],
// [fs.StatFS], [fs.ReadDirFS], and [fs.ReadFileFS] so it can be handed to
// harness ingestion unchanged.
//
// The zero value is not usable; construct one with [New]. Callers must Close it
// to release the root descriptor.
type FS struct {
	root           *os.Root
	rootPath       string
	rejectSymlinks bool

	mu            sync.Mutex
	validationErr error
}

// New opens root for confined reading. When rejectSymlinks is true the tree is
// treated as remote content: symlinks are refused everywhere, including at the
// root itself.
func New(root string, rejectSymlinks bool) (*FS, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve registry root: %w", err)
	}
	if rejectSymlinks {
		// The root path comes from configuration rather than from registry
		// content, so following it is fine; reporting it keeps the remote
		// "no symlinks" policy honest about the tree we are about to read.
		info, err := os.Lstat(rootAbs)
		if err != nil {
			return nil, fmt.Errorf("stat registry root: %w", err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("registry root %q is a symlink; remote registries do not allow symlinks", rootAbs)
		}
	}
	opened, err := os.OpenRoot(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("open registry root: %w", err)
	}
	return &FS{root: opened, rootPath: rootAbs, rejectSymlinks: rejectSymlinks}, nil
}

// Close releases the root descriptor. Files already returned by Open stay
// usable after Close.
func (r *FS) Close() error {
	if r == nil || r.root == nil {
		return nil
	}
	return r.root.Close()
}

// Root returns the absolute path of the confinement root.
func (r *FS) Root() string {
	if r == nil {
		return ""
	}
	return r.rootPath
}

// ValidationError returns the first containment or file-type rejection observed
// by this FS, if any. Ingestion layers convert read failures into diagnostics
// and keep going, so callers use this to fail closed on a tree that tried to
// read outside itself.
func (r *FS) ValidationError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.validationErr
}

// Open implements [fs.FS].
func (r *FS) Open(name string) (fs.File, error) {
	file, info, err := r.open(name)
	if err != nil {
		return nil, err
	}
	return &openFile{File: file, fsys: r, name: name, info: info}, nil
}

// Stat implements [fs.StatFS]. It never opens the named file, so a FIFO or a
// device node is reported by type rather than blocking.
func (r *FS) Stat(name string) (fs.FileInfo, error) {
	if err := r.validName(name); err != nil {
		return nil, err
	}
	if r.rejectSymlinks {
		if err := r.refuseSymlinks(name); err != nil {
			return nil, err
		}
	}
	info, err := r.root.Stat(name)
	if err != nil {
		return nil, r.record(r.diagnose(name, err))
	}
	if err := r.record(checkType(name, info.Mode())); err != nil {
		return nil, err
	}
	return info, nil
}

// ReadDir implements [fs.ReadDirFS]. A directory containing an entry this
// package refuses to read — an escaping symlink, a symlink in a remote tree, or
// a special file — fails as a whole: a hostile entry beside a wanted one is a
// tree we decline to ingest, not a file to silently skip.
func (r *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	file, info, err := r.open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	if !info.IsDir() {
		return nil, r.record(fmt.Errorf("registry path %q is not a directory", name))
	}
	entries, err := file.ReadDir(-1)
	if err != nil {
		return nil, r.record(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := r.checkEntry(name, entry); err != nil {
			return nil, r.record(err)
		}
	}
	return entries, nil
}

// ReadDirLenient lists a directory, dropping the entries this package refuses
// instead of failing the whole listing, and returns one error per dropped entry.
//
// It exists for callers that materialize a directory's files individually and
// can report a skipped file without abandoning the rest — a hook whose helper
// script is legitimate should still be installed when an unrelated entry beside
// it is refused. Nothing is relaxed: a dropped entry is never read.
func (r *FS) ReadDirLenient(name string) ([]fs.DirEntry, []error, error) {
	file, info, err := r.open(name)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()
	if !info.IsDir() {
		return nil, nil, r.record(fmt.Errorf("registry path %q is not a directory", name))
	}
	entries, err := file.ReadDir(-1)
	if err != nil {
		return nil, nil, r.record(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	kept := make([]fs.DirEntry, 0, len(entries))
	var refused []error
	for _, entry := range entries {
		if err := r.checkEntry(name, entry); err != nil {
			refused = append(refused, r.record(err))
			continue
		}
		kept = append(kept, entry)
	}
	return kept, refused, nil
}

// ReadFile implements [fs.ReadFileFS].
func (r *FS) ReadFile(name string) ([]byte, error) {
	data, _, err := r.ReadFileMode(name)
	return data, err
}

// ReadFileMode reads a regular file and reports the mode of the very descriptor
// that was read, so a caller reproducing permissions cannot be handed the mode
// of a different file that replaced this one after the read.
func (r *FS) ReadFileMode(name string) ([]byte, fs.FileMode, error) {
	file, info, err := r.open(name)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = file.Close() }()
	if info.IsDir() {
		return nil, 0, r.record(fmt.Errorf("registry path %q is a directory, not a file", name))
	}
	buf := make([]byte, 0, info.Size())
	data, err := readAll(file, buf)
	if err != nil {
		return nil, 0, r.record(err)
	}
	return data, info.Mode(), nil
}

// Sub returns a view of the subtree at dir. The returned FS keeps the original
// confinement root: a symlink inside dir that points elsewhere in the same
// registry stays readable, which is what distinguishes a registry root from a
// per-resource sandbox.
func (r *FS) Sub(dir string) (fs.FS, error) {
	if dir == "." {
		return r, nil
	}
	if err := r.validName(dir); err != nil {
		return nil, err
	}
	return &subFS{parent: r, base: dir}, nil
}

func (r *FS) open(name string) (*os.File, fs.FileInfo, error) {
	if err := r.validName(name); err != nil {
		return nil, nil, err
	}
	if r.rejectSymlinks {
		if err := r.refuseSymlinks(name); err != nil {
			return nil, nil, err
		}
	}
	// openNonBlock keeps a FIFO from blocking this open; the type check below
	// then refuses it. Without the flag the process hangs before any check runs.
	file, err := r.root.OpenFile(name, os.O_RDONLY|openNonBlock, 0)
	if err != nil {
		return nil, nil, r.record(r.diagnose(name, err))
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, r.record(err)
	}
	if err := checkType(name, info.Mode()); err != nil {
		_ = file.Close()
		return nil, nil, r.record(err)
	}
	return file, info, nil
}

func (r *FS) validName(name string) error {
	if !fs.ValidPath(name) {
		return r.record(&fs.PathError{Op: "open", Path: name, Err: errors.New("invalid registry path")})
	}
	return nil
}

// refuseSymlinks enforces the remote-tree policy. Every component is examined
// with a confined Lstat, which cannot itself escape, so the worst a concurrent
// rewrite can do is swap one contained entry for another — containment and the
// file-type check are still enforced by the open that follows.
//
// This runs on every remote read, so it walks the prefixes without materializing
// them (see forEachPrefix): the check is unavoidable, the garbage is not.
func (r *FS) refuseSymlinks(name string) error {
	return forEachPrefix(name, func(prefix string) error {
		info, err := r.root.Lstat(prefix)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return err
			}
			return r.record(r.diagnose(prefix, err))
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return r.record(fmt.Errorf("registry path %q is a symlink; remote registries do not allow symlinks", prefix))
		}
		return nil
	})
}

func (r *FS) checkEntry(dir string, entry fs.DirEntry) error {
	child := join(dir, entry.Name())
	switch {
	case entry.IsDir() || entry.Type().IsRegular():
		return nil
	case entry.Type()&fs.ModeSymlink != 0:
		if r.rejectSymlinks {
			return fmt.Errorf("registry path %q is a symlink; remote registries do not allow symlinks", child)
		}
		info, err := r.root.Stat(child)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// A dangling link that still resolves inside the root discloses
				// nothing; an escaping one fails above with an escape error.
				return nil
			}
			return r.diagnose(child, err)
		}
		return checkType(child, info.Mode())
	default:
		return checkType(child, entry.Type())
	}
}

// diagnose turns an os.Root failure into an operator-actionable error. os.Root
// reports an escape with an unexported error value, so the reason is recovered
// by re-walking the components with confined Lstat/Readlink calls. This runs
// only on the failure path; a successful read pays nothing for it.
func (r *FS) diagnose(name string, err error) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return err
	}
	if component, target, escapes := r.escapingComponent(name); escapes {
		switch {
		case target == "":
			return fmt.Errorf("registry path %q resolves outside the registry root (at %q)", name, component)
		case isAbsoluteLink(target):
			// An absolute target cannot be followed from a directory descriptor;
			// honouring it would mean resolving the path by name again, which is
			// the check-then-use pattern this package exists to remove. Registry
			// symlinks must therefore be relative.
			return fmt.Errorf("registry path %q is an absolute symlink (%q -> %q); registry symlinks must be relative and stay inside the registry root", name, component, target)
		default:
			return fmt.Errorf("registry path %q resolves outside the registry root (%q -> %q)", name, component, target)
		}
	}
	return fmt.Errorf("registry path %q could not be read within the registry root: %w", name, err)
}

func (r *FS) escapingComponent(name string) (component, target string, escapes bool) {
	errStop := errors.New("stop")
	_ = forEachPrefix(name, func(prefix string) error {
		info, err := r.root.Lstat(prefix)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return errStop
			}
			// Lstat is confined as well, so a non-ENOENT failure here means the
			// escape happens at or before this component.
			component, escapes = prefix, true
			return errStop
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			return nil
		}
		link, err := r.root.Readlink(prefix)
		if err != nil {
			component, escapes = prefix, true
			return errStop
		}
		if linkEscapes(prefix, link) {
			component, target, escapes = prefix, link, true
			return errStop
		}
		return nil
	})
	return component, target, escapes
}

func (r *FS) record(err error) error {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return err
	}
	r.mu.Lock()
	if r.validationErr == nil {
		r.validationErr = err
	}
	r.mu.Unlock()
	return err
}

// linkEscapes reports whether a symlink at name pointing at link leaves the
// root. An absolute target counts as escaping even when it happens to name a
// path inside the root: os.Root refuses it, because following it requires
// resolving a path by name instead of walking descriptors.
func linkEscapes(name, link string) bool {
	if link == "" {
		return true
	}
	if isAbsoluteLink(link) {
		return true
	}
	resolved := path.Join(path.Dir(filepath.ToSlash(name)), filepath.ToSlash(link))
	return resolved == ".." || strings.HasPrefix(resolved, "../")
}

func isAbsoluteLink(link string) bool {
	return filepath.IsAbs(link) || strings.HasPrefix(link, "/")
}

func checkType(name string, mode fs.FileMode) error {
	if mode.IsDir() || mode.IsRegular() {
		return nil
	}
	return fmt.Errorf("registry path %q has unsupported file type %s; only regular files and directories are allowed", name, mode.Type())
}

// forEachPrefix calls fn with every path prefix of name, shortest first,
// excluding ".", and stops at the first error. It slices name instead of building
// a list because it runs per read under the remote policy.
func forEachPrefix(name string, fn func(prefix string) error) error {
	if name == "." {
		return nil
	}
	for i := 0; i < len(name); i++ {
		if name[i] != '/' {
			continue
		}
		if err := fn(name[:i]); err != nil {
			return err
		}
	}
	return fn(name)
}

func join(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

// readAll reads f into buf, which is sized from the descriptor's own stat.
func readAll(f io.Reader, buf []byte) ([]byte, error) {
	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}
		n, err := f.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf, nil
			}
			return buf, err
		}
	}
}

type openFile struct {
	*os.File
	fsys *FS
	name string
	info fs.FileInfo
}

func (f *openFile) Stat() (fs.FileInfo, error) { return f.info, nil }

// ReadDir applies the same entry policy as FS.ReadDir so a tree walked through
// fs.WalkDir is checked identically to one read directly.
func (f *openFile) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.File.ReadDir(n)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := f.fsys.checkEntry(f.name, entry); err != nil {
			return nil, f.fsys.record(err)
		}
	}
	return entries, nil
}

// subFS exposes a subtree while keeping the parent's confinement root.
type subFS struct {
	parent *FS
	base   string
}

func (s *subFS) resolve(name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: "open", Path: name, Err: errors.New("invalid registry path")}
	}
	if name == "." {
		return s.base, nil
	}
	return s.base + "/" + name, nil
}

func (s *subFS) Open(name string) (fs.File, error) {
	full, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	return s.parent.Open(full)
}

func (s *subFS) Stat(name string) (fs.FileInfo, error) {
	full, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	return s.parent.Stat(full)
}

func (s *subFS) ReadDir(name string) ([]fs.DirEntry, error) {
	full, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	return s.parent.ReadDir(full)
}

func (s *subFS) ReadFile(name string) ([]byte, error) {
	full, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	return s.parent.ReadFile(full)
}

func (s *subFS) Sub(dir string) (fs.FS, error) {
	full, err := s.resolve(dir)
	if err != nil {
		return nil, err
	}
	return s.parent.Sub(full)
}
