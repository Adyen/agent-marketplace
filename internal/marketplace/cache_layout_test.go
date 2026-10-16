package marketplace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// mainClientReadArtifact performs the read an older client does on the legacy
// artifact path: a verified entry is decoded and validated as that client validates
// it, and anything else is treated as absent so the client refetches.
func mainClientReadArtifact(cache *Cache, digest string) error {
	raw, verified, err := cache.ReadArtifact(digest)
	if err != nil {
		return err
	}
	if raw == nil || !verified {
		return nil
	}
	ca, caRaw, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err := ca.ValidateArchive(nil); err != nil {
		return err
	}
	return ca.VerifyArchiveDigest(caRaw)
}

// spec: marketplace-api/placeholder-form-archives-stay-out-of-the-legacy-artifact-path
func TestPlaceholderFormStaysOutOfLegacyArtifactPath(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, placeholder, manifest := readToolchainFixture(t, goMajorMinor)
			server := serveToolchainFixtureResource(t, raw, placeholder, manifest, true)
			cache := NewCache(t.TempDir())

			resolved, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), toolchainFixtureConfig(server.URL, manifest), []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh)
			if len(errs) != 0 || len(resolved) != 1 {
				t.Fatalf("resolve artifacts=%d errors=%v", len(resolved), errs)
			}
			if got := resolved[0].Path; got != cache.archivePath(placeholderForm, manifest.ArchiveDigest) {
				t.Fatalf("resolved path %q, want the placeholder location", got)
			}
			cached, verified, err := cache.readArchive(placeholderForm, manifest.ArchiveDigest)
			if err != nil || !verified || !bytes.Equal(cached, placeholder) {
				t.Fatalf("placeholder location: verified=%v err=%v, want the placeholder bytes", verified, err)
			}
			if _, err := os.Stat(cache.ArtifactPath(manifest.ArchiveDigest)); !os.IsNotExist(err) {
				t.Fatalf("legacy artifact path written by a placeholder fetch: stat err=%v", err)
			}
			if err := mainClientReadArtifact(cache, manifest.ArchiveDigest); err != nil {
				t.Fatalf("older client read of this cache failed: %v", err)
			}
		})
	}
}

// spec: marketplace-api/placeholder-form-archives-stay-out-of-the-legacy-artifact-path
func TestPlaceholderBytesAtLegacyPathBreakOlderReaders(t *testing.T) {
	raw, placeholder, manifest := readToolchainFixture(t, currentToolchain())
	_ = raw
	cache := NewCache(t.TempDir())
	if _, err := cache.WriteArtifact(manifest.ArchiveDigest, placeholder); err != nil {
		t.Fatal(err)
	}
	if err := cache.MarkArtifactVerified(manifest.ArchiveDigest); err != nil {
		t.Fatal(err)
	}
	err := mainClientReadArtifact(cache, manifest.ArchiveDigest)
	if err == nil || !strings.Contains(err.Error(), "archive digest mismatch") {
		t.Fatalf("older client read of placeholder bytes at the legacy path = %v, want an archive digest mismatch", err)
	}
}

// spec: marketplace-api/cached-placeholder-form-archive-serves-offline-resolution
func TestCachedNoFetchResolvesPlaceholderFormFromCache(t *testing.T) {
	for _, goMajorMinor := range toolchainFixtureVersions {
		t.Run("recorded-by-"+goMajorMinor, func(t *testing.T) {
			raw, placeholder, manifest := readToolchainFixture(t, goMajorMinor)
			server := serveToolchainFixtureResource(t, raw, placeholder, manifest, true)
			cfg := toolchainFixtureConfig(server.URL, manifest)
			cache := NewCache(t.TempDir())
			if _, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeRefresh); len(errs) != 0 {
				t.Fatalf("online resolve: %v", errs)
			}

			server.Close()
			resolved, errs := (&Resolver{Cache: cache}).Resolve(context.Background(), cfg, []hcc.Target{hcc.TargetClaude}, resolver.MarketplaceResolveModeCachedNoFetch)
			if len(errs) != 0 || len(resolved) != 1 {
				t.Fatalf("offline resolve from placeholder location: artifacts=%d errors=%v", len(resolved), errs)
			}
			if resolved[0].MarketplaceArchiveDigest != manifest.ArchiveDigest {
				t.Fatalf("offline archive digest %q, want %q", resolved[0].MarketplaceArchiveDigest, manifest.ArchiveDigest)
			}
		})
	}
}

// gcFixture is a Marketplace cache with one referenced and one orphaned archive,
// each present in both the placeholder and the legacy location.
type gcFixture struct {
	cache      *Cache
	cfg        *config.Config
	referenced string
	orphan     string
}

func newGCFixture(t *testing.T) gcFixture {
	t.Helper()
	c := NewCache(t.TempDir())
	contentDigest := "sha256:" + strings.Repeat("1", 64)
	referenced := "sha256:" + strings.Repeat("2", 64)
	orphan := "sha256:" + strings.Repeat("3", 64)
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: "v1", Digest: contentDigest}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://marketplace.example", InstanceID: "mp", Resources: []config.MarketplaceResourcePin{pin}}}}
	rev := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(referenced),
	})
	if _, err := c.WriteResourceRevision(cfg.Marketplaces[0].URL, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, rev); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{referenced, orphan} {
		if _, err := c.writeArchive(placeholderForm, digest, []byte("placeholder "+digest)); err != nil {
			t.Fatal(err)
		}
		if err := c.markArchiveVerified(placeholderForm, digest); err != nil {
			t.Fatal(err)
		}
		if _, err := c.WriteArtifact(digest, []byte("final "+digest)); err != nil {
			t.Fatal(err)
		}
		if err := c.MarkArtifactVerified(digest); err != nil {
			t.Fatal(err)
		}
	}
	return gcFixture{cache: c, cfg: cfg, referenced: referenced, orphan: orphan}
}

func (f gcFixture) orphanDirs() []string {
	dirs := []string{
		filepath.Dir(f.cache.archivePath(finalForm, f.orphan)),
		filepath.Dir(f.cache.archivePath(placeholderForm, f.orphan)),
	}
	sort.Strings(dirs)
	return dirs
}

// spec: marketplace-api/placeholder-form-archive-is-collected-with-its-archive
func TestCacheGarbageCollectCollectsPlaceholderFormWithItsArchive(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run("dry_run_"+map[bool]string{true: "true", false: "false"}[dryRun], func(t *testing.T) {
			f := newGCFixture(t)
			result, err := f.cache.garbageCollect(dryRun, GCRoots{ExactPins: f.cfg.Marketplaces})
			if err != nil {
				t.Fatalf("GarbageCollect: %v", err)
			}
			want := f.orphanDirs()
			sort.Strings(result.Removed)
			if strings.Join(result.Removed, "\n") != strings.Join(want, "\n") {
				t.Fatalf("removed = %v, want orphan placeholder and legacy directories %v", result.Removed, want)
			}
			for _, dir := range want {
				_, err := os.Stat(dir)
				if dryRun && err != nil {
					t.Fatalf("dry run removed %q: %v", dir, err)
				}
				if !dryRun && !os.IsNotExist(err) {
					t.Fatalf("orphan %q survived, stat err=%v", dir, err)
				}
			}
			for _, digest := range []string{f.referenced} {
				for _, path := range []string{f.cache.archivePath(finalForm, digest), f.cache.archivePath(placeholderForm, digest)} {
					if _, err := os.Stat(path); err != nil {
						t.Fatalf("referenced archive %q removed: %v", path, err)
					}
				}
			}
		})
	}
}

// spec: marketplace-api/placeholder-form-archive-is-collected-with-its-archive
func TestCacheGarbageCollectPreservesMalformedPlaceholderEntries(t *testing.T) {
	c := NewCache(t.TempDir())
	unknown := filepath.Join(c.root, placeholderArchivesDirName, "not-a-digest")
	malformed := filepath.Join(c.root, placeholderArchivesDirName, "sha256-"+strings.Repeat("d", 64))
	if err := os.MkdirAll(unknown, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(malformed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformed, "unexpected"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GarbageCollect(false, GCRoots{}); err != nil {
		t.Fatalf("GarbageCollect: %v", err)
	}
	for _, path := range []string{unknown, malformed} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("GC removed preserved placeholder entry %q: %v", path, err)
		}
	}
}

// spec: marketplace-api/placeholder-form-archive-is-collected-with-its-archive
func TestCacheGarbageCollectRejectsSymlinkedPlaceholderEntry(t *testing.T) {
	c := NewCache(t.TempDir())
	external := t.TempDir()
	digest := "sha256:" + strings.Repeat("4", 64)
	if err := os.MkdirAll(filepath.Join(c.root, placeholderArchivesDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "artifact.tar.gz"), []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(c.root, placeholderArchivesDirName, safeDigest(digest))); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GarbageCollect(false, GCRoots{}); err == nil {
		t.Fatal("symlinked placeholder entry did not fail closed")
	}
	if _, err := os.Stat(filepath.Join(external, "artifact.tar.gz")); err != nil {
		t.Fatalf("symlink target was removed: %v", err)
	}
}
