package cache

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const cacheDirPerm = 0o755

// Temporary and retired entries are named so that they can never be mistaken
// for a published entry: a published entry is always an encoded ref (base32, or
// refDigestPrefix followed by hex), and neither form starts with a dot.
const (
	tempEntryInfix     = ".new-"
	retiredEntryInfix  = ".old-"
	scratchEntryPrefix = "."
)

// cacheStore is an anchored, containment-safe handle on the registry cache for
// a single registry URL.
//
// Anchoring rule: the cache *parent* (~/.cache) is resolved by path, so a
// symlinked HOME or a symlinked ~/.cache — both legitimate and common user
// setups — keep working. Everything from the cache root downwards
// (~/.cache/agent-marketplace, the per-URL hash directory, entries,
// temporaries) is reached only through *os.Root, which refuses any name whose
// resolution leaves the root. A ref therefore cannot name a location outside
// the cache no matter what it encodes, and a symlink planted inside the cache
// cannot redirect a write out of it.
type cacheStore struct {
	parent *os.Root
	root   *os.Root
	hash   *os.Root

	// hashDir is a descriptor on the per-URL directory. It serves two purposes:
	// it carries the advisory lock (flock is owned by the open file
	// description) and it is the anchor git subprocesses chdir to.
	hashDir *os.File

	rootName string
	hashName string
	rootPath string

	rootCreated bool
	hashCreated bool
	locked      bool
}

// openCacheStore opens — creating if necessary — the cache root and the
// per-URL hash directory beneath it, and returns a handle anchored to both.
func openCacheStore(cacheRoot, hashName string) (*cacheStore, error) {
	cacheParent := filepath.Dir(cacheRoot)
	rootName := filepath.Base(cacheRoot)

	if err := os.MkdirAll(cacheParent, cacheDirPerm); err != nil {
		return nil, fmt.Errorf("create cache parent %q: %w", cacheParent, err)
	}
	// A relative cache root is CacheDir's fallback for when the home directory
	// cannot be determined, and it places the cache under the process's working
	// directory — which may be a repository checkout somebody else wrote. A
	// symlinked parent is only the legitimate case (a relocated home, a cache
	// moved to another volume) when that parent sits inside a home directory, so
	// refuse it here. Everything below the anchored root stays protected by
	// *os.Root either way.
	if !filepath.IsAbs(cacheRoot) {
		if info, statErr := os.Lstat(cacheParent); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing symlinked cache parent %q outside a home directory", cacheParent)
		}
	}
	parent, err := os.OpenRoot(cacheParent)
	if err != nil {
		return nil, fmt.Errorf("open cache parent %q: %w", cacheParent, err)
	}

	store := &cacheStore{
		parent:   parent,
		rootName: rootName,
		hashName: hashName,
		rootPath: filepath.Join(cacheParent, rootName),
	}

	root, rootCreated, err := openOrCreateRoot(parent, rootName)
	if err != nil {
		store.close()
		return nil, fmt.Errorf("open cache root %q: %w", store.rootPath, err)
	}
	store.root, store.rootCreated = root, rootCreated

	hash, hashCreated, err := openOrCreateRoot(root, hashName)
	if err != nil {
		store.close()
		return nil, fmt.Errorf("open cache directory %q: %w", filepath.Join(store.rootPath, hashName), err)
	}
	store.hash, store.hashCreated = hash, hashCreated

	hashDir, err := hash.Open(".")
	if err != nil {
		store.close()
		return nil, fmt.Errorf("open cache directory descriptor %q: %w", filepath.Join(store.rootPath, hashName), err)
	}
	store.hashDir = hashDir

	return store, nil
}

func openOrCreateRoot(parent *os.Root, name string) (*os.Root, bool, error) {
	if root, err := parent.OpenRoot(name); err == nil {
		return root, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	created := true
	if err := parent.Mkdir(name, cacheDirPerm); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, false, err
		}
		created = false
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, false, err
	}
	return root, created, nil
}

// close releases every descriptor, which also releases the advisory lock.
func (s *cacheStore) close() {
	if s == nil {
		return
	}
	if s.hashDir != nil {
		_ = s.hashDir.Close()
		s.hashDir = nil
		s.locked = false
	}
	for _, root := range []**os.Root{&s.hash, &s.root, &s.parent} {
		if *root != nil {
			_ = (*root).Close()
			*root = nil
		}
	}
}

// lock serializes cache mutation for this registry, across processes and across
// goroutines, by taking an exclusive advisory lock on the per-URL directory
// itself.
//
// Locking the directory rather than a lock *file* is deliberate: a lock file is
// state that has to be cleaned up, and cleaning it up is exactly what makes
// lock files racy (unlinking a file another process has open silently splits
// the lock). The directory already exists for as long as the cache entry does.
func (s *cacheStore) lock() error {
	if err := lockDir(s.hashDir); err != nil {
		return err
	}
	s.locked = true
	return nil
}

func (s *cacheStore) unlock() {
	if s == nil || !s.locked || s.hashDir == nil {
		return
	}
	if err := unlockDir(s.hashDir); err != nil {
		slog.Debug("release registry cache lock", "dir", s.hashDir.Name(), "error", err)
	}
	s.locked = false
}

// dir returns the descriptor git subprocesses are anchored to. Names passed to
// git are relative to it, so git never resolves a cache path itself.
func (s *cacheStore) dir() *os.File { return s.hashDir }

// openEntry opens a cache entry by name. A name that resolves outside the cache
// root — the interesting case being a planted symlink — fails here, before any
// mutation.
func (s *cacheStore) openEntry(name string) (*os.File, error) {
	file, err := s.hash.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = file.Close()
		return nil, fmt.Errorf("cache entry %q is not a directory", name)
	}
	return file, nil
}

// makeTemp creates a uniquely named scratch sibling of entryName and returns its
// name.
func (s *cacheStore) makeTemp(entryName, infix string) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("generate temporary cache entry name: %w", err)
		}
		name := scratchEntryPrefix + entryName + infix + hex.EncodeToString(random[:])
		err := s.hash.Mkdir(name, cacheDirPerm)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return name, nil
	}
	return "", errors.New("could not create a unique temporary cache entry")
}

// reserveName returns an unused scratch name without creating it.
//
// A rename target that is a directory is refused with EEXIST on darwin even when
// it is empty, so the target of the retiring rename in swap has to be a name that
// does not exist rather than a directory makeTemp created.
func (s *cacheStore) reserveName(entryName, infix string) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("generate scratch cache entry name: %w", err)
		}
		name := scratchEntryPrefix + entryName + infix + hex.EncodeToString(random[:])
		_, err := s.hash.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not reserve a unique scratch cache entry name")
}

func (s *cacheStore) removeEntry(name string) error {
	return s.hash.RemoveAll(name)
}

// publish moves a completed temporary entry into its published name.
//
// A rename onto a directory another process published first fails with
// ENOTEMPTY. That is convergence, not an error: both processes cloned the same
// ref of the same registry, so the winner's entry is equivalent to ours. This
// is the backstop for filesystems that refuse advisory locks, where two cold
// publishers can genuinely race.
func (s *cacheStore) publish(tempName, entryName string) (bool, error) {
	if err := s.hash.Rename(tempName, entryName); err != nil {
		if _, statErr := s.hash.Stat(entryName); statErr == nil {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// swap replaces a published entry with a completed temporary one.
//
// Two renames, never an in-place mutation: a reader resolving the entry path
// sees either the old tree or the new one, both complete, and a reader that
// already opened the old tree keeps reading it because the retired directory is
// only unlinked after the new one is in place.
func (s *cacheStore) swap(tempName, entryName string) error {
	retired, err := s.reserveName(entryName, retiredEntryInfix)
	if err != nil {
		return err
	}
	if err := s.hash.Rename(entryName, retired); err != nil {
		return fmt.Errorf("retire cache entry %q: %w", entryName, err)
	}
	if err := s.hash.Rename(tempName, entryName); err != nil {
		if restoreErr := s.hash.Rename(retired, entryName); restoreErr != nil {
			return fmt.Errorf("publish cache entry %q: %w (restoring the previous entry also failed: %v)", entryName, err, restoreErr)
		}
		return fmt.Errorf("publish cache entry %q: %w", entryName, err)
	}
	if err := s.removeEntry(retired); err != nil {
		slog.Warn("remove retired registry cache entry", "entry", retired, "error", err)
	}
	return nil
}

// sweepScratch removes scratch directories left behind by a process that died
// mid-publication.
//
// It is only safe while this process holds the exclusive lock: the lock is what
// proves no other writer owns a scratch directory right now.
func (s *cacheStore) sweepScratch() {
	if !s.locked {
		return
	}
	dir, err := s.hash.Open(".")
	if err != nil {
		slog.Debug("list registry cache directory for scratch sweep", "error", err)
		return
	}
	names, err := dir.Readdirnames(-1)
	_ = dir.Close()
	if err != nil {
		slog.Debug("read registry cache directory for scratch sweep", "error", err)
		return
	}
	for _, name := range names {
		if !strings.HasPrefix(name, scratchEntryPrefix) {
			continue
		}
		if !strings.Contains(name, tempEntryInfix) && !strings.Contains(name, retiredEntryInfix) {
			continue
		}
		if err := s.removeEntry(name); err != nil {
			slog.Warn("remove abandoned registry cache scratch entry", "entry", name, "error", err)
			continue
		}
		slog.Debug("removed abandoned registry cache scratch entry", "entry", name)
	}
}

// cleanupNew removes the directories this store created when the operation they
// were created for failed, so a failed run leaves no cache skeleton behind. Only
// empty directories are removed: a non-empty one is in use by someone else.
func (s *cacheStore) cleanupNew() error {
	if s == nil {
		return nil
	}
	var firstErr error
	if s.hashCreated && s.root != nil {
		if err := removeIfEmpty(s.root, s.hashName); err != nil {
			firstErr = err
		}
	}
	if s.rootCreated && s.parent != nil {
		if err := removeIfEmpty(s.parent, s.rootName); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func removeIfEmpty(parent *os.Root, name string) error {
	dir, err := parent.Open(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	names, err := dir.Readdirnames(1)
	_ = dir.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(names) > 0 {
		return nil
	}
	if err := parent.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
