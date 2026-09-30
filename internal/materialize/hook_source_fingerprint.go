package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
)

// hookSourceFingerprintMaxBytes bounds the total source data hashed for one
// fingerprint, matching the 64 MiB safety ceiling used for canonical source
// archives. A source that cannot be read safely within it yields no match.
const hookSourceFingerprintMaxBytes = 64 << 20

// hookSourceFingerprintReadDirBatch bounds how many directory entries one
// ReadDir call may accumulate at once; enumeration keeps working in batches so
// a hostile directory cannot force an unbounded entry slice.
const hookSourceFingerprintReadDirBatch = 128

// HookSourceFingerprintScope is the scope vocabulary the version-1 fingerprint
// serializes.
const (
	HookScopeRepository = "repository"
	HookScopeMachine    = "machine"
)

// HookSourceFingerprintInput identifies one selected source hook. Only the
// digest of these inputs — never their contents — may reach ownership state.
type HookSourceFingerprintInput struct {
	// SourceIdentity is the stable source identity: a registry occurrence or
	// exact Marketplace resource revision, or the canonical configured source
	// identity for local directories.
	SourceIdentity string
	// Name is the installed resource name.
	Name string
	// Target is the harness target identifier.
	Target string
	// Scope is HookScopeRepository or HookScopeMachine.
	Scope string
	// SourceRoot is the absolute source-layout root the definition path is
	// resolved under. Callers never supply a definition absolute path: the
	// implementation opens SourceRoot once as a directory descriptor and
	// resolves DefinitionRel component-by-component beneath it with
	// O_NOFOLLOW, so no path outside the root can be hashed.
	SourceRoot string
	// DefinitionRel is the slash-separated, source-layout-relative path of the
	// original hook definition (for example "hooks/hooks.json").
	DefinitionRel string
}

// hookSourceFingerprintField appends one length-prefixed field so concatenated
// fields cannot be re-parsed ambiguously. The format is frozen by the shared
// golden vectors in testdata/hook-source-fingerprint-v1.json.
func hookSourceFingerprintField(buf, field []byte) []byte {
	buf = append(buf, []byte(strconv.Itoa(len(field)))...)
	buf = append(buf, ':')
	return append(buf, field...)
}

func hookSourceFingerprintFieldString(buf []byte, field string) []byte {
	return hookSourceFingerprintField(buf, []byte(field))
}

// HookSourceFingerprint computes the spec's version-1 source-hook fingerprint:
// sha256 over the literal "hook-source/v1\x00" followed by length-prefixed
// source identity, installed name, target, scope, the source-layout-relative
// definition path, the definition's exact bytes, and then every other regular
// file below the definition's directory tree in lexically sorted slash
// relative-path order, each contributing its path, permission bits ("%04o"),
// and exact bytes.
//
// All filesystem access is rooted at a single descriptor for SourceRoot and
// resolved with openat(O_NOFOLLOW): a symlinked ancestor or final component, a
// non-regular special file, or a swapped entry fails closed instead of being
// followed. Any unavailable field, unreadable or oversized file set,
// traversal-suspect relative path, or symlink anywhere under the definition
// directory returns an error and no fingerprint, never a partial match.
func HookSourceFingerprint(in HookSourceFingerprintInput) (string, error) {
	if in.SourceIdentity == "" || in.Name == "" || in.Target == "" {
		return "", errors.New("hook source fingerprint: source identity, name and target are required")
	}
	if in.Scope != HookScopeRepository && in.Scope != HookScopeMachine {
		return "", fmt.Errorf("hook source fingerprint: scope %q is not repository or machine", in.Scope)
	}
	if in.SourceRoot == "" {
		return "", errors.New("hook source fingerprint: source root is required")
	}
	rel := in.DefinitionRel
	if rel == "" || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) ||
		rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || strings.HasSuffix(rel, "/..") ||
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))) != rel {
		return "", fmt.Errorf("hook source fingerprint: definition path %q is not a clean source-relative path", rel)
	}

	rootFD, err := unix.Open(in.SourceRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", fmt.Errorf("hook source fingerprint: open source root: %w", err)
	}
	defer func() { _ = unix.Close(rootFD) }()

	// Resolve the definition directory component-by-component beneath the
	// root descriptor; any symlinked ancestor refuses here.
	var total int64
	defDirFD := rootFD
	components := strings.Split(filepath.ToSlash(filepath.Dir(filepath.FromSlash(rel))), "/")
	if len(components) != 1 || components[0] != "." {
		for _, c := range components {
			next, err := openFingerprintDir(defDirFD, c)
			if defDirFD != rootFD {
				_ = unix.Close(defDirFD)
			}
			if err != nil {
				return "", err
			}
			defDirFD = next
		}
	}
	if defDirFD != rootFD {
		defer func() { _ = unix.Close(defDirFD) }()
	}

	defName := filepath.Base(filepath.FromSlash(rel))
	defBytes, err := readFingerprintEntry(defDirFD, defName, rel, &total)
	if err != nil {
		return "", err
	}

	type supportFile struct {
		rel  string
		mode string
		data []byte
	}
	var support []supportFile
	if err := walkFingerprintDir(defDirFD, func(relPath string) error {
		if relPath == filepath.ToSlash(defName) {
			return nil
		}
		data, perm, err := readFingerprintEntryAt(defDirFD, relPath, &total)
		if err != nil {
			return err
		}
		support = append(support, supportFile{
			rel:  relPath,
			mode: fmt.Sprintf("%04o", perm),
			data: data,
		})
		return nil
	}); err != nil {
		return "", err
	}
	sort.Slice(support, func(i, j int) bool { return support[i].rel < support[j].rel })

	buf := []byte("hook-source/v1\x00")
	buf = hookSourceFingerprintFieldString(buf, in.SourceIdentity)
	buf = hookSourceFingerprintFieldString(buf, in.Name)
	buf = hookSourceFingerprintFieldString(buf, in.Target)
	buf = hookSourceFingerprintFieldString(buf, in.Scope)
	buf = hookSourceFingerprintFieldString(buf, rel)
	buf = hookSourceFingerprintField(buf, defBytes)
	for _, s := range support {
		buf = hookSourceFingerprintFieldString(buf, s.rel)
		buf = hookSourceFingerprintFieldString(buf, s.mode)
		buf = hookSourceFingerprintField(buf, s.data)
	}
	sum := sha256.Sum256(buf)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// hookSourceMirrorDigestPattern is the only content-digest shape the mirror
// fingerprint accepts: an absent or unverified digest can never satisfy.
var hookSourceMirrorDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// HookSourceMirrorFingerprintInput identifies one selected mirror-acquired
// Marketplace hook entirely by verified resolution metadata. Mirror
// acquisition retains no original source tree — the resolver Path is the
// verified artifact archive — so this fingerprint hashes no file bytes and
// must never be derived from the archive path.
type HookSourceMirrorFingerprintInput struct {
	// InstanceID is the Marketplace instance identifier the pin resolves
	// under.
	InstanceID string
	// RegistryID is the Marketplace registry the resource revision belongs
	// to.
	RegistryID string
	// Kind is the resource kind ("hook").
	Kind string
	// ResourceName is the Marketplace resource name.
	ResourceName string
	// Digest is the pin's VERIFIED resource-revision content digest
	// ("sha256:<64 hex>") — the same digest the resolver checked the decoded
	// artifact's content against before marking it verified.
	Digest string
	// InstalledName is the installed resource name.
	InstalledName string
	// Target is the harness target identifier.
	Target string
	// Scope is HookScopeRepository or HookScopeMachine.
	Scope string
}

// HookSourceMirrorFingerprint computes the spec's domain-separated mirror
// fingerprint: sha256 over the literal "hook-source/mirror/v1\x00" followed
// by length-prefixed instance ID, registry ID, kind, resource name, verified
// content digest, installed name, target, and scope. The version prefix is
// disjoint from hook-source/v1, so no mirror fingerprint can match a
// source-tree fingerprint over the same fields.
//
// Every field is required and the digest must be a well-formed sha256 digest:
// an absent, stale, or unverified digest fails closed and yields no match.
// The format is frozen by the shared golden vectors in
// testdata/hook-source-fingerprint-mirror-v1.json.
func HookSourceMirrorFingerprint(in HookSourceMirrorFingerprintInput) (string, error) {
	if in.InstanceID == "" || in.RegistryID == "" || in.Kind == "" || in.ResourceName == "" ||
		in.InstalledName == "" || in.Target == "" {
		return "", errors.New("hook source mirror fingerprint: instance ID, registry ID, kind, resource name, installed name and target are required")
	}
	if !hookSourceMirrorDigestPattern.MatchString(in.Digest) {
		return "", fmt.Errorf("hook source mirror fingerprint: content digest %q is not a verified sha256 digest", in.Digest)
	}
	if in.Scope != HookScopeRepository && in.Scope != HookScopeMachine {
		return "", fmt.Errorf("hook source mirror fingerprint: scope %q is not repository or machine", in.Scope)
	}
	buf := []byte("hook-source/mirror/v1\x00")
	for _, field := range []string{
		in.InstanceID, in.RegistryID, in.Kind, in.ResourceName,
		in.Digest, in.InstalledName, in.Target, in.Scope,
	} {
		buf = hookSourceFingerprintFieldString(buf, field)
	}
	sum := sha256.Sum256(buf)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// openFingerprintDir opens one directory component beneath dirfd, refusing a
// symlink or non-directory entry.
func openFingerprintDir(dirfd int, name string) (int, error) {
	fd, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("hook source fingerprint: open directory %s: %w", name, err)
	}
	return fd, nil
}

// openFingerprintFile opens one file component beneath dirfd with O_NOFOLLOW
// and O_NONBLOCK (so a FIFO cannot block the open), then validates via fstat
// that the descriptor is a regular file and returns its permission bits.
func openFingerprintFile(dirfd int, name string) (*os.File, os.FileMode, error) {
	fd, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("hook source fingerprint: open %s: %w", name, err)
	}
	f := os.NewFile(uintptr(fd), name)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = f.Close()
		return nil, 0, fmt.Errorf("hook source fingerprint: stat %s: %w", name, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = f.Close()
		return nil, 0, fmt.Errorf("hook source fingerprint: %s is not a regular file", name)
	}
	return f, os.FileMode(st.Mode & 0o777), nil
}

// readFingerprintFD reads all bytes from one already-validated regular-file
// descriptor while enforcing the cumulative source-size ceiling. A file that
// grows past the bound during the read is refused.
func readFingerprintFD(f *os.File, relPath string, total *int64) ([]byte, error) {
	remaining := hookSourceFingerprintMaxBytes - *total
	if remaining < 0 {
		return nil, fmt.Errorf("hook source fingerprint: source files exceed %d bytes", hookSourceFingerprintMaxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, remaining+1))
	if err != nil {
		return nil, fmt.Errorf("hook source fingerprint: read %s: %w", relPath, err)
	}
	*total += int64(len(data))
	if *total > hookSourceFingerprintMaxBytes {
		return nil, fmt.Errorf("hook source fingerprint: source files exceed %d bytes", hookSourceFingerprintMaxBytes)
	}
	return data, nil
}

// readFingerprintEntry opens and reads the definition file directly beneath
// defDirFD.
func readFingerprintEntry(defDirFD int, name, relPath string, total *int64) ([]byte, error) {
	f, _, err := openFingerprintFile(defDirFD, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return readFingerprintFD(f, relPath, total)
}

// readFingerprintEntryAt opens and reads one support file at a slash-separated
// relative path beneath defDirFD, resolving each ancestor directory with
// O_NOFOLLOW so a swapped component cannot escape. It returns the file bytes
// and permission bits taken from the opened descriptor's fstat.
func readFingerprintEntryAt(defDirFD int, relPath string, total *int64) ([]byte, os.FileMode, error) {
	components := strings.Split(relPath, "/")
	dirfd := defDirFD
	for _, c := range components[:len(components)-1] {
		next, err := openFingerprintDir(dirfd, c)
		if dirfd != defDirFD {
			_ = unix.Close(dirfd)
		}
		if err != nil {
			return nil, 0, err
		}
		dirfd = next
	}
	if dirfd != defDirFD {
		defer func() { _ = unix.Close(dirfd) }()
	}
	f, perm, err := openFingerprintFile(dirfd, components[len(components)-1])
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	data, err := readFingerprintFD(f, relPath, total)
	if err != nil {
		return nil, 0, err
	}
	return data, perm, nil
}

// walkFingerprintDir recursively enumerates the tree beneath defDirFD using
// descriptor-relative opens only. Symlinks and non-regular non-directory
// entries fail the walk. fn runs once per regular file with its slash
// relative path. Directory contents are read in bounded batches and the total
// number of regular files is capped at the existing canonical archive file
// budget; a tree exceeding it fails closed rather than enumerating without
// bound.
func walkFingerprintDir(defDirFD int, fn func(relPath string) error) error {
	maxFiles := artifact.DefaultArchiveLimits().MaxTotalFiles
	var files int64
	// walk reads dirfd's entries through a duplicated descriptor so dirfd
	// itself stays valid for the openat calls below; the caller keeps
	// ownership of dirfd.
	var walk func(dirfd int, prefix string) error
	walk = func(dirfd int, prefix string) error {
		readFD, err := unix.Dup(dirfd)
		if err != nil {
			return fmt.Errorf("hook source fingerprint: dup directory %s: %w", prefix, err)
		}
		dir := os.NewFile(uintptr(readFD), prefix)
		// Entries cannot be processed inside the read loop because recursion
		// into a subdirectory reuses dirfd while dir still holds a duplicated
		// read position on it. Keep the per-directory accumulation bounded by
		// the same file budget so a single directory cannot grow the slice
		// without limit; entries beyond the budget fail before being stored.
		var entries []os.DirEntry
		for {
			batch, err := dir.ReadDir(hookSourceFingerprintReadDirBatch)
			if len(batch) > 0 {
				if int64(len(entries))+int64(len(batch)) > maxFiles {
					_ = dir.Close()
					return fmt.Errorf("hook source fingerprint: source file count exceeds %d", maxFiles)
				}
				entries = append(entries, batch...)
			}
			if err != nil {
				_ = dir.Close()
				if errors.Is(err, io.EOF) {
					break
				}
				return fmt.Errorf("hook source fingerprint: read directory %s: %w", prefix, err)
			}
		}
		_ = dir.Close()
		for _, e := range entries {
			relPath := e.Name()
			if prefix != "" {
				relPath = prefix + "/" + e.Name()
			}
			typ := e.Type()
			if typ&os.ModeSymlink != 0 {
				return fmt.Errorf("hook source fingerprint: %s is a symlink; refusing to follow", relPath)
			}
			if typ.IsDir() {
				sub, err := openFingerprintDir(dirfd, e.Name())
				if err != nil {
					return err
				}
				err = walk(sub, relPath)
				_ = unix.Close(sub)
				if err != nil {
					return err
				}
				continue
			}
			if !typ.IsRegular() {
				return fmt.Errorf("hook source fingerprint: %s is not a regular file", relPath)
			}
			files++
			if files > maxFiles {
				return fmt.Errorf("hook source fingerprint: source file count exceeds %d", maxFiles)
			}
			if err := fn(relPath); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(defDirFD, "")
}
