package cache

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"golang.org/x/sys/unix"
)

var (
	ErrExactGitSnapshotUnavailable = errors.New("exact git snapshot unavailable")
	ErrExactGitSnapshotUntrusted   = errors.New("exact git snapshot untrusted")
	ErrGitOIDMismatch              = errors.New("git oid mismatch")
)

type GitOID struct {
	Algorithm string
	Hex       string
}

func ParseGitOID(value string) (GitOID, error) {
	algorithm, raw, ok := strings.Cut(value, ":")
	if !ok {
		return GitOID{}, fmt.Errorf("parse git oid: missing algorithm")
	}
	oid := GitOID{Algorithm: algorithm, Hex: raw}
	if err := oid.validate(); err != nil {
		return GitOID{}, err
	}
	return oid, nil
}

func (oid GitOID) String() string {
	return oid.Algorithm + ":" + oid.Hex
}

func (oid GitOID) validate() error {
	length := 0
	switch oid.Algorithm {
	case "sha1":
		length = 40
	case "sha256":
		length = 64
	default:
		return fmt.Errorf("unsupported git oid algorithm %q", oid.Algorithm)
	}
	if len(oid.Hex) != length {
		return fmt.Errorf("%s git oid must contain %d hexadecimal characters", oid.Algorithm, length)
	}
	for _, c := range oid.Hex {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("git oid must use lowercase hexadecimal")
		}
	}
	return nil
}

const exactGitLockTimeout = 30 * time.Second

func ResolveExactGitRef(canonicalSource, fetchURL, ref string) (GitOID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), exactGitLockTimeout)
	defer cancel()
	return ResolveExactGitRefWithContext(ctx, canonicalSource, fetchURL, ref)
}

func ResolveExactGitRefWithContext(ctx context.Context, canonicalSource, fetchURL, ref string) (GitOID, error) {
	if err := validateExactGitInputs(canonicalSource, fetchURL); err != nil {
		return GitOID{}, err
	}
	if err := validateGitRef(ref); err != nil {
		return GitOID{}, fmt.Errorf("validate git ref: %w", err)
	}

	var oid GitOID
	err := withExactGitLock(ctx, sourceKey(canonicalSource), func() error {
		store := exactGitObjectStorePath(canonicalSource)
		exists, err := validateExactGitDirectory(store)
		if err != nil {
			return fmt.Errorf("validate git object store: %w", err)
		}
		if !exists {
			if err := cloneExactGitObjectStore(fetchURL, store); err != nil {
				return err
			}
		}
		if err := sanitizeExactGitConfig(store); err != nil {
			return fmt.Errorf("sanitize git object store config: %w", err)
		}
		const resolvedRef = "refs/agent-marketplace/resolved"
		if err := runExactGit(store, "fetch", "--force", "--no-tags", "--no-write-fetch-head", "--", fetchURL, ref+":"+resolvedRef); err != nil {
			return fmt.Errorf("fetch mutable ref %q: %w", ref, err)
		}
		defer func() { _ = runExactGit(store, "update-ref", "-d", resolvedRef) }()
		format, err := exactGitOutput(store, "rev-parse", "--show-object-format")
		if err != nil {
			return fmt.Errorf("query git object format: %w", err)
		}
		raw, err := exactGitOutput(store, "rev-parse", "--verify", resolvedRef+"^{commit}")
		if err != nil {
			return fmt.Errorf("resolve fetched ref %q: %w", ref, err)
		}
		oid, err = ParseGitOID(format + ":" + raw)
		return err
	})
	if err != nil {
		return GitOID{}, err
	}
	slog.Debug("resolved exact git ref", "source", redactSecrets(canonicalSource), "ref", ref, "algorithm", oid.Algorithm)
	return oid, nil
}

func AcquireExactGit(canonicalSource, fetchURL string, oid GitOID) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), exactGitLockTimeout)
	defer cancel()
	return AcquireExactGitWithContext(ctx, canonicalSource, fetchURL, oid)
}

func AcquireExactGitWithContext(ctx context.Context, canonicalSource, fetchURL string, oid GitOID) (string, error) {
	if err := validateExactGitInputs(canonicalSource, fetchURL); err != nil {
		return "", err
	}
	if err := oid.validate(); err != nil {
		return "", err
	}

	path := exactGitSnapshotPath(canonicalSource, oid)
	err := withExactGitLock(ctx, snapshotKey(canonicalSource, oid), func() error {
		exists, err := validateExactGitDirectory(path)
		if err != nil {
			return fmt.Errorf("validate exact git snapshot: %w", err)
		}
		if exists {
			if err := verifyExactGitSnapshot(path, oid); err == nil {
				return nil
			}
			if err := quarantineExactGitSnapshot(path); err != nil {
				return err
			}
		}
		if err := ensureExactGitObject(ctx, canonicalSource, fetchURL, oid); err != nil {
			return err
		}
		return publishExactGitSnapshot(path, exactGitObjectStorePath(canonicalSource), oid)
	})
	if err != nil {
		return "", err
	}
	slog.Debug("acquired exact git snapshot", "source", redactSecrets(canonicalSource), "oid", oid.String(), "cache_path", path)
	return path, nil
}

func OpenExactGit(canonicalSource string, oid GitOID) (string, error) {
	return OpenExactGitReadOnly(canonicalSource, oid)
}

func OpenExactGitReadOnly(canonicalSource string, oid GitOID) (string, error) {
	if err := validateCanonicalSource(canonicalSource); err != nil {
		return "", err
	}
	if err := oid.validate(); err != nil {
		return "", err
	}
	path := exactGitSnapshotPath(canonicalSource, oid)
	if err := openExactGitPath(path, oid); err != nil {
		return "", err
	}
	return path, nil
}

func openExactGitPath(path string, oid GitOID) error {
	exists, err := validateExactGitDirectory(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrExactGitSnapshotUntrusted, err)
	}
	if !exists {
		return fmt.Errorf("%w: %s", ErrExactGitSnapshotUnavailable, oid.String())
	}
	return verifyExactGitSnapshot(path, oid)
}

func quarantineExactGitSnapshot(path string) error {
	quarantine := path + ".quarantine"
	if exists, err := validateExactGitDirectory(quarantine); err != nil {
		return fmt.Errorf("validate exact git quarantine: %w", err)
	} else if exists {
		if err := os.RemoveAll(quarantine); err != nil {
			return fmt.Errorf("remove prior exact git quarantine: %w", err)
		}
	}
	if err := os.Rename(path, quarantine); err != nil {
		return fmt.Errorf("quarantine untrusted exact git snapshot: %w", err)
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return fmt.Errorf("remove exact git quarantine: %w", err)
	}
	return nil
}

func cloneExactGitObjectStore(fetchURL, store string) error {
	parent := filepath.Dir(store)
	if err := prepareExactGitDirectory(parent); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".objects-stage-")
	if err != nil {
		return fmt.Errorf("create git object store staging directory: %w", err)
	}
	if err := os.Remove(stage); err != nil {
		return fmt.Errorf("prepare git object store staging path: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := runExactGit("", "clone", "--mirror", "--no-tags", "--", fetchURL, stage); err != nil {
		return fmt.Errorf("clone git object store: %w", err)
	}
	if err := sanitizeExactGitConfig(stage); err != nil {
		return fmt.Errorf("sanitize git object store config: %w", err)
	}
	if _, err := exactGitOutput(stage, "rev-parse", "--show-object-format"); err != nil {
		return fmt.Errorf("verify git object store format: %w", err)
	}
	if err := os.Rename(stage, store); err != nil {
		return fmt.Errorf("publish git object store: %w", err)
	}
	return nil
}

func ensureExactGitObject(ctx context.Context, canonicalSource, fetchURL string, oid GitOID) error {
	return withExactGitKeyLock(ctx, sourceKey(canonicalSource), func() error {
		store := exactGitObjectStorePath(canonicalSource)
		exists, err := validateExactGitDirectory(store)
		if err != nil {
			return fmt.Errorf("validate git object store: %w", err)
		}
		if !exists {
			if err := prepareExactGitDirectory(filepath.Dir(store)); err != nil {
				return err
			}
			if err := runExactGit("", "init", "--bare", "--object-format="+oid.Algorithm, store); err != nil {
				return fmt.Errorf("initialize git object store: %w", err)
			}
		}
		if err := sanitizeExactGitConfig(store); err != nil {
			return fmt.Errorf("sanitize git object store config: %w", err)
		}
		format, err := exactGitOutput(store, "rev-parse", "--show-object-format")
		if err != nil {
			return fmt.Errorf("query git object format: %w", err)
		}
		if format != oid.Algorithm {
			return fmt.Errorf("%w: repository uses %s, lock declares %s", ErrGitOIDMismatch, format, oid.Algorithm)
		}
		if _, err := exactGitOutput(store, "cat-file", "-e", oid.Hex+"^{commit}"); err == nil {
			return nil
		}
		if err := runExactGit(store, "fetch", "--force", "--no-tags", "--no-write-fetch-head", "--", fetchURL, oid.Hex); err != nil {
			return fmt.Errorf("fetch exact git object %s: %w", oid.String(), err)
		}
		got, err := exactGitOutput(store, "rev-parse", "--verify", oid.Hex+"^{commit}")
		if err != nil || got != oid.Hex {
			return fmt.Errorf("%w: fetched object does not resolve to %s", ErrGitOIDMismatch, oid.String())
		}
		return nil
	})
}

func publishExactGitSnapshot(path, store string, oid GitOID) error {
	parent := filepath.Dir(path)
	if err := prepareExactGitDirectory(parent); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".snapshot-stage-")
	if err != nil {
		return fmt.Errorf("create exact git staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := runExactGit(stage, "init", "--object-format="+oid.Algorithm); err != nil {
		return fmt.Errorf("initialize exact git staging repository: %w", err)
	}
	if err := sanitizeExactGitConfig(stage); err != nil {
		return fmt.Errorf("sanitize exact git staging config: %w", err)
	}
	if err := runExactGit(stage, "fetch", "--force", "--no-tags", "--no-write-fetch-head", "--", store, oid.Hex); err != nil {
		return fmt.Errorf("copy exact git object into snapshot: %w", err)
	}
	if err := runExactGit(stage, "checkout", "--detach", "--force", oid.Hex); err != nil {
		return fmt.Errorf("checkout exact git object: %w", err)
	}
	if err := verifyExactGitSnapshot(stage, oid); err != nil {
		return err
	}
	if err := os.Rename(stage, path); err != nil {
		return fmt.Errorf("publish exact git snapshot: %w", err)
	}
	return nil
}

func verifyExactGitSnapshot(path string, oid GitOID) error {
	if err := validateExactGitConfig(path); err != nil {
		return fmt.Errorf("%w: %v", ErrExactGitSnapshotUntrusted, err)
	}
	format, err := exactGitOutput(path, "rev-parse", "--show-object-format")
	if err != nil {
		return fmt.Errorf("%w: query object format: %v", ErrExactGitSnapshotUntrusted, err)
	}
	if format != oid.Algorithm {
		return fmt.Errorf("%w: %w: repository uses %s, lock declares %s", ErrExactGitSnapshotUntrusted, ErrGitOIDMismatch, format, oid.Algorithm)
	}
	head, err := exactGitOutput(path, "rev-parse", "--verify", "HEAD")
	if err != nil || head != oid.Hex {
		return fmt.Errorf("%w: %w: HEAD is %q, want %q", ErrExactGitSnapshotUntrusted, ErrGitOIDMismatch, head, oid.Hex)
	}
	top, err := exactGitOutput(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("%w: query worktree root: %v", ErrExactGitSnapshotUntrusted, err)
	}
	pathAbs, pathErr := filepath.EvalSymlinks(path)
	topAbs, topErr := filepath.EvalSymlinks(top)
	if pathErr != nil || topErr != nil || pathAbs != topAbs {
		return fmt.Errorf("%w: worktree root differs from snapshot path", ErrExactGitSnapshotUntrusted)
	}
	if err := verifyExactGitTrackedTree(path, oid.Algorithm); err != nil {
		return fmt.Errorf("%w: worktree differs from exact commit: %v", ErrExactGitSnapshotUntrusted, err)
	}
	if err := validateExactGitTree(path); err != nil {
		return fmt.Errorf("%w: %v", ErrExactGitSnapshotUntrusted, err)
	}
	return nil
}

type exactGitTreeEntry struct {
	mode string
	oid  string
}

func sanitizeExactGitConfig(repo string) error {
	if err := os.Chmod(repo, 0o700); err != nil {
		return fmt.Errorf("secure repository directory: %w", err)
	}
	output, err := exactGitBytes(repo, "config", "--local", "--name-only", "--list")
	if err != nil {
		return fmt.Errorf("inspect repository-local config: %w", err)
	}
	hasOrigin := false
	invalidKey := ""
	for _, key := range strings.Fields(string(output)) {
		lower := strings.ToLower(key)
		switch lower {
		case "core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.precomposeunicode", "extensions.objectformat":
		case "remote.origin.url", "remote.origin.fetch", "remote.origin.mirror", "remote.origin.tagopt":
			hasOrigin = true
		default:
			if invalidKey == "" {
				invalidKey = key
			}
		}
	}
	if hasOrigin {
		if err := runExactGit(repo, "config", "--local", "--remove-section", "remote.origin"); err != nil {
			return fmt.Errorf("remove repository origin: %w", err)
		}
	}
	if err := removeExactGitMetadata(repo, "FETCH_HEAD"); err != nil {
		return err
	}
	if invalidKey != "" {
		return fmt.Errorf("repository-local config key %q is not allowed", invalidKey)
	}
	configPath, err := exactGitOutput(repo, "rev-parse", "--git-path", "config")
	if err != nil {
		return fmt.Errorf("locate repository-local config: %w", err)
	}
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(repo, configPath)
	}
	info, err := os.Lstat(configPath)
	if err != nil {
		return fmt.Errorf("inspect repository-local config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("repository-local config is not a regular file")
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("secure repository-local config: %w", err)
	}
	return validateExactGitConfig(repo)
}

func removeExactGitMetadata(repo, name string) error {
	path, err := exactGitOutput(repo, "rev-parse", "--git-path", name)
	if err != nil {
		return fmt.Errorf("locate repository metadata %s: %w", name, err)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	rel, err := filepath.Rel(repo, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("repository metadata %s escapes repository", name)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove repository metadata %s: %w", name, err)
	}
	return nil
}

func validateExactGitConfig(repo string) error {
	output, err := exactGitBytes(repo, "config", "--local", "--name-only", "--list")
	if err != nil {
		return fmt.Errorf("inspect repository-local config: %w", err)
	}
	for _, key := range strings.Fields(string(output)) {
		switch strings.ToLower(key) {
		case "core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.precomposeunicode", "extensions.objectformat":
		default:
			return fmt.Errorf("repository-local config key %q is not allowed", key)
		}
	}
	configPath, err := exactGitOutput(repo, "rev-parse", "--git-path", "config")
	if err != nil {
		return fmt.Errorf("locate repository-local config: %w", err)
	}
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(repo, configPath)
	}
	info, err := os.Lstat(configPath)
	if err != nil {
		return fmt.Errorf("inspect repository-local config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("repository-local config is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("repository-local config mode %o is not private", info.Mode().Perm())
	}
	return nil
}

func verifyExactGitTrackedTree(root, algorithm string) error {
	output, err := exactGitBytes(root, "ls-tree", "-rz", "--full-tree", "-r", "HEAD")
	if err != nil {
		return err
	}
	expected := make(map[string]exactGitTreeEntry)
	for _, record := range strings.Split(string(output), "\x00") {
		if record == "" {
			continue
		}
		header, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || fields[1] != "blob" || name == "" {
			return errors.New("unsupported or malformed tracked tree entry")
		}
		expected[filepath.FromSlash(name)] = exactGitTreeEntry{mode: fields[0], oid: fields[2]}
	}
	seen := make(map[string]struct{}, len(expected))
	err = filepath.WalkDir(root, func(candidate string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, candidate)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		if rel == "." || entry.IsDir() {
			return nil
		}
		want, ok := expected[rel]
		if !ok {
			return fmt.Errorf("unexpected path %q", rel)
		}
		info, err := os.Lstat(candidate)
		if err != nil {
			return err
		}
		var data []byte
		switch want.mode {
		case "100644", "100755":
			if !info.Mode().IsRegular() || (want.mode == "100755") != (info.Mode().Perm()&0o111 != 0) {
				return fmt.Errorf("mode mismatch at %q", rel)
			}
			fd, err := unix.Open(candidate, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				return err
			}
			file := os.NewFile(uintptr(fd), candidate)
			data, err = io.ReadAll(file)
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		case "120000":
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("mode mismatch at %q", rel)
			}
			target, err := os.Readlink(candidate)
			if err != nil {
				return err
			}
			data = []byte(target)
		default:
			return fmt.Errorf("unsupported mode %q at %q", want.mode, rel)
		}
		if gitBlobOID(algorithm, data) != want.oid {
			return fmt.Errorf("content mismatch at %q", rel)
		}
		seen[rel] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(expected) {
		return errors.New("tracked path is missing")
	}
	return nil
}

func gitBlobOID(algorithm string, data []byte) string {
	header := []byte("blob " + strconv.Itoa(len(data)) + "\x00")
	if algorithm == "sha256" {
		h := sha256.New()
		_, _ = h.Write(header)
		_, _ = h.Write(data)
		return hex.EncodeToString(h.Sum(nil))
	}
	h := sha1.New()
	_, _ = h.Write(header)
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func validateExactGitTree(root string) error {
	rootAbs, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(rootAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("symlink %q cannot be resolved", path)
		}
		resolvedAbs, err := filepath.Abs(resolved)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootAbs, resolvedAbs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("symlink %q escapes snapshot", path)
		}
		return nil
	})
}

func AcquireExactGitNamespaceLease() (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), exactGitLockTimeout)
	defer cancel()
	return AcquireExactGitNamespaceLeaseWithContext(ctx)
}

func AcquireExactGitNamespaceLeaseWithContext(ctx context.Context) (func(), error) {
	return acquireExactGitFileLock(ctx, "namespace", unix.LOCK_SH)
}

func withExactGitLock(ctx context.Context, key string, fn func() error) error {
	return withExactGitFileLock(ctx, "namespace", unix.LOCK_SH, func() error {
		return withExactGitKeyLock(ctx, key, fn)
	})
}

func withExactGitKeyLock(ctx context.Context, key string, fn func() error) error {
	return withExactGitFileLock(ctx, key, unix.LOCK_EX, fn)
}

func withExactGitExclusiveLock(ctx context.Context, fn func() error) error {
	return withExactGitFileLock(ctx, "namespace", unix.LOCK_EX, fn)
}

func withExactGitFileLock(ctx context.Context, key string, operation int, fn func() error) error {
	release, err := acquireExactGitFileLock(ctx, key, operation)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

var ErrExactGitLockTimeout = errors.New("exact git cache lock timeout")

func acquireExactGitFileLock(ctx context.Context, key string, operation int) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, exactGitLockTimeout)
		defer cancel()
	}
	lockDir := filepath.Join(exactGitRoot(), "locks")
	if err := prepareExactGitDirectory(lockDir); err != nil {
		return nil, err
	}
	path := filepath.Join(lockDir, key+".lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open exact git lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect exact git lock: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = file.Close()
		return nil, errors.New("exact git lock is not a regular file")
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure exact git lock: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, errors.Join(ErrExactGitLockTimeout, err)
		}
		err := unix.Flock(fd, operation|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			_ = file.Close()
			return nil, fmt.Errorf("lock exact git cache: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			_ = file.Close()
			return nil, errors.Join(ErrExactGitLockTimeout, ctx.Err())
		case <-timer.C:
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = unix.Flock(fd, unix.LOCK_UN)
			_ = file.Close()
		})
	}, nil
}

func prepareExactGitDirectory(path string) error {
	if _, err := validateExactGitDirectory(path); err != nil {
		return fmt.Errorf("validate exact git cache path: %w", err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create exact git cache path: %w", err)
	}
	root := exactGitRoot()
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("exact git cache path %q escapes root", path)
	}
	for current := path; ; current = filepath.Dir(current) {
		if err := os.Chmod(current, 0o700); err != nil {
			return fmt.Errorf("secure exact git cache path: %w", err)
		}
		if current == root {
			break
		}
	}
	_, err = validateExactGitDirectory(path)
	return err
}

func validateExactGitDirectory(path string) (bool, error) {
	cacheRoot := CacheDir()
	boundary := cacheRoot
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		boundary = home
	}
	if err := ValidateDirectoryAncestry(boundary, cacheRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := ValidateDirectoryAncestry(cacheRoot, path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("path %q is not a trusted directory", path)
	}
	return true, nil
}

func validateExactGitInputs(canonicalSource, fetchURL string) error {
	if err := validateCanonicalSource(canonicalSource); err != nil {
		return err
	}
	if err := validateGitURL(fetchURL); err != nil {
		return fmt.Errorf("validate fetch url: %w", err)
	}
	return nil
}

func validateCanonicalSource(source string) error {
	if strings.TrimSpace(source) == "" || source != strings.TrimSpace(source) {
		return errors.New("canonical git source is required and must be trimmed")
	}
	for _, c := range source {
		if c < 0x20 || c == 0x7f {
			return errors.New("canonical git source contains a control character")
		}
	}
	return nil
}

func exactGitBytes(repoDir string, args ...string) ([]byte, error) {
	args = append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}, args...)
	output, err := execGit(repoDir, args)
	if err != nil {
		message := redactSecrets(strings.TrimSpace(string(output)))
		if message != "" {
			message = strings.SplitN(message, "\n", 2)[0]
			return nil, fmt.Errorf("git %s: %w: %s", strings.Join(redactArgs(args), " "), err, truncateGitError(message))
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(redactArgs(args), " "), err)
	}
	return output, nil
}

func exactGitOutput(repoDir string, args ...string) (string, error) {
	output, err := exactGitBytes(repoDir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func runExactGit(repoDir string, args ...string) error {
	args = append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}, args...)
	output, err := execGit(repoDir, args)
	if err == nil {
		return nil
	}
	message := redactSecrets(strings.TrimSpace(string(output)))
	if message != "" {
		message = strings.SplitN(message, "\n", 2)[0]
		return fmt.Errorf("git %s: %w: %s", strings.Join(redactArgs(args), " "), err, truncateGitError(message))
	}
	return fmt.Errorf("git %s: %w", strings.Join(redactArgs(args), " "), err)
}

func exactGitRoot() string {
	return filepath.Join(CacheDir(), exactGitCacheDirName)
}

func exactGitObjectStorePath(canonicalSource string) string {
	return filepath.Join(exactGitRoot(), "objects", sourceKey(canonicalSource)+".git")
}

func exactGitSnapshotPath(canonicalSource string, oid GitOID) string {
	return filepath.Join(exactGitRoot(), "snapshots", snapshotKey(canonicalSource, oid))
}

func sourceKey(canonicalSource string) string {
	sum := sha256.Sum256([]byte(canonicalSource))
	return hex.EncodeToString(sum[:])
}

func snapshotKey(canonicalSource string, oid GitOID) string {
	sum := sha256.Sum256([]byte(canonicalSource + "\x00" + oid.String()))
	return hex.EncodeToString(sum[:])
}

func garbageCollectExactGit(dryRun bool, locks []lockfile.Document) (result GCResult, resultErr error) {
	started := time.Now()
	reachable := make(map[string]struct{})
	slog.Debug("exact git cache garbage collection started", "dry_run", dryRun, "lock_layers", len(locks))
	defer func() {
		attrs := []any{"dry_run", dryRun, "reachable", len(reachable), "removed", len(result.Removed), "duration_ms", time.Since(started).Milliseconds()}
		if resultErr != nil {
			attrs = append(attrs, "error", resultErr)
			slog.Error("exact git cache garbage collection failed", attrs...)
			return
		}
		slog.Debug("exact git cache garbage collection completed", attrs...)
	}()
	for _, doc := range locks {
		for _, registry := range doc.Registries {
			oid, err := ParseGitOID(registry.Commit.String())
			if err != nil {
				return result, err
			}
			reachable[snapshotKey(registry.URL, oid)] = struct{}{}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), exactGitLockTimeout)
	defer cancel()
	err := withExactGitExclusiveLock(ctx, func() error {
		roots := []struct {
			path      string
			stage     string
			snapshots bool
		}{
			{path: filepath.Join(exactGitRoot(), "objects"), stage: ".objects-stage-"},
			{path: filepath.Join(exactGitRoot(), "snapshots"), stage: ".snapshot-stage-", snapshots: true},
		}
		for _, root := range roots {
			exists, err := validateExactGitDirectory(root.path)
			if err != nil {
				return fmt.Errorf("validate exact git GC root %q: %w", root.path, err)
			}
			if !exists {
				continue
			}
			entries, err := os.ReadDir(root.path)
			if err != nil {
				return fmt.Errorf("read exact git GC root %q: %w", root.path, err)
			}
			for _, entry := range entries {
				_, retained := reachable[entry.Name()]
				removable := strings.HasPrefix(entry.Name(), root.stage) || root.snapshots && len(entry.Name()) == sha256.Size*2 && isHex(entry.Name()) && !retained
				if !removable {
					continue
				}
				candidate := filepath.Join(root.path, entry.Name())
				info, err := os.Lstat(candidate)
				if err != nil {
					return fmt.Errorf("inspect exact git GC candidate %q: %w", candidate, err)
				}
				if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
					return fmt.Errorf("exact git GC candidate %q is not a trusted directory", candidate)
				}
				if err := ValidateDirectoryAncestry(exactGitRoot(), candidate); err != nil {
					return fmt.Errorf("validate exact git GC candidate %q: %w", candidate, err)
				}
				if !dryRun {
					if err := os.RemoveAll(candidate); err != nil {
						return fmt.Errorf("remove exact git GC candidate %q: %w", candidate, err)
					}
				}
				result.Removed = append(result.Removed, candidate)
			}
		}
		return nil
	})
	slices.Sort(result.Removed)
	return result, err
}
