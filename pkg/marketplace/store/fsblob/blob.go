package fsblob

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

func (s *Store) path(digest store.ArchiveDigest) string {
	d := string(digest)
	if len(d) < 2 {
		return filepath.Join(s.root, d)
	}
	return filepath.Join(s.root, d[:2], d[2:])
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
	data, err := os.ReadFile(s.path(digest))
	if errors.Is(err, os.ErrNotExist) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", digest, err)
	}
	return data, nil
}

// Put stores data under the given digest using a temporary file and atomic
// rename. Existing content at the same digest is replaced.
func (s *Store) Put(ctx context.Context, digest store.ArchiveDigest, data []byte) error {
	p := s.path(digest)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}

	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	tmp := p + ".tmp." + suffix

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create temp blob: %w", err)
	}

	cleanup := func() { _ = os.Remove(tmp) }

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
	if err := os.Rename(tmp, p); err != nil {
		cleanup()
		return fmt.Errorf("rename temp blob: %w", err)
	}
	return nil
}

// PutImmutable stores data only when the digest address is empty or already
// contains the same bytes. Unlike Put, it never replaces a different first
// writer; this is used for immutable collection manifests.
func (s *Store) PutImmutable(ctx context.Context, digest store.ArchiveDigest, data []byte) error {
	p := s.path(digest)
	if existing, err := os.ReadFile(p); err == nil {
		if string(existing) == string(data) {
			return nil
		}
		return fmt.Errorf("%w: blob %s already contains different data", store.ErrConflict, digest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing blob %s: %w", digest, err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}

	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	tmp := p + ".tmp." + suffix
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create temp blob: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp) }
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
	if err := os.Link(tmp, p); err != nil {
		cleanup()
		if os.IsExist(err) {
			existing, readErr := os.ReadFile(p)
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
	_, err := os.Stat(s.path(digest))
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
	err := os.Remove(s.path(digest))
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
