package fsblob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Store is a filesystem-backed content-addressed [store.BlobStore].
// Blobs are laid out as <root>/<first-2-digest-chars>/<remaining-digest-chars>.
type Store struct {
	root string
}

// New returns a filesystem blob store rooted at the given path.
func New(root string) *Store {
	return &Store{root: root}
}

func (s *Store) open(digest store.ArchiveDigest, create bool) (*os.Root, string, error) {
	d := string(digest)
	if !filepath.IsLocal(d) || d == "." || strings.HasPrefix(d, "..") || strings.ContainsAny(d, "/\\\x00") {
		return nil, "", fmt.Errorf("%w: invalid blob address", store.ErrTopology)
	}
	p := d
	if len(d) > 2 {
		p = filepath.Join(d[:2], d[2:])
	}
	if create {
		if err := os.MkdirAll(s.root, 0755); err != nil {
			return nil, "", fmt.Errorf("create blob root: %w", err)
		}
	}
	root, err := os.OpenRoot(s.root)
	return root, p, err
}

// Probe verifies that the blob root is usable. A missing root is valid for a
// new empty store; it is created on the first write.
func (s *Store) Probe(ctx context.Context) error {
	info, err := os.Stat(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat blob root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: blob root %q is not a directory", store.ErrTopology, s.root)
	}
	return nil
}

// Get returns the blob bytes for digest. It returns [store.ErrNotFound] when
// the blob does not exist.
func (s *Store) Get(ctx context.Context, digest store.ArchiveDigest) ([]byte, error) {
	root, p, err := s.open(digest, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", digest, err)
	}
	return data, nil
}

func (s *Store) GetBounded(ctx context.Context, digest store.ArchiveDigest, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, p, err := s.open(digest, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open bounded blob: %w", err)
	}
	defer func() { _ = file.Close() }()
	return store.ReadBlobBounded(file, limit)
}

// Put stores data under the given digest using a temporary file and atomic
// rename. Existing content at the same digest is replaced.
func (s *Store) Put(ctx context.Context, digest store.ArchiveDigest, data []byte) error {
	root, p, err := s.open(digest, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}

	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	tmp := p + ".tmp." + suffix

	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create temp blob: %w", err)
	}

	cleanup := func() { _ = root.Remove(tmp) }

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("write temp blob: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("sync temp blob: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp blob: %w", err)
	}
	if err := root.Rename(tmp, p); err != nil {
		cleanup()
		return fmt.Errorf("rename temp blob: %w", err)
	}
	return nil
}

// PutImmutable stores data only when the digest address is empty or already
// contains the same bytes. Unlike Put, it never replaces a different first
// writer; this is used for immutable collection manifests.
func (s *Store) PutImmutable(ctx context.Context, digest store.ArchiveDigest, data []byte) error {
	root, p, err := s.open(digest, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if existing, err := root.ReadFile(p); err == nil {
		if string(existing) == string(data) {
			return nil
		}
		return fmt.Errorf("%w: blob %s already contains different data", store.ErrConflict, digest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing blob %s: %w", digest, err)
	}
	if err := root.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}

	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	tmp := p + ".tmp." + suffix
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create temp blob: %w", err)
	}
	cleanup := func() { _ = root.Remove(tmp) }
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("write temp blob: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("sync temp blob: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp blob: %w", err)
	}
	// Linking, rather than renaming, makes the destination creation
	// non-overwriting even when another publisher wins the race.
	if err := root.Link(tmp, p); err != nil {
		cleanup()
		if os.IsExist(err) {
			existing, readErr := root.ReadFile(p)
			if readErr == nil && string(existing) == string(data) {
				return nil
			}
			if readErr != nil {
				return fmt.Errorf("read concurrently published blob %s: %w", digest, readErr)
			}
			return fmt.Errorf("%w: blob %s already contains different data", store.ErrConflict, digest)
		}
		return fmt.Errorf("create immutable blob %s: %w", digest, err)
	}
	cleanup()
	return nil
}

// Exists reports whether a blob exists for digest.
func (s *Store) Exists(ctx context.Context, digest store.ArchiveDigest) (bool, error) {
	root, p, err := s.open(digest, false)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	_, err = root.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat blob %s: %w", digest, err)
	}
	return true, nil
}

// Delete removes the blob for digest.
func (s *Store) Delete(ctx context.Context, digest store.ArchiveDigest) error {
	root, p, err := s.open(digest, false)
	if errors.Is(err, os.ErrNotExist) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	err = root.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return store.ErrNotFound
	}
	return err
}

func randomSuffix() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate random suffix: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
