package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestGcCommandDryRunReportsOrphanedCacheDirectoriesWithoutDeleting(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/security.git
    ref: main
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/repo-skills.git
    ref: main
`)

	activeRepoCache := filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.com/repo-skills.git"))
	activeGlobalCache := filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.com/security.git"))
	orphanCache := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/legacy-skills.git"))

	writeFile(t, filepath.Join(activeRepoCache, "main", ".gitkeep"), "active\n")
	writeFile(t, filepath.Join(activeRepoCache, "v1.0.0", ".gitkeep"), "keep stale ref for active registry\n")
	writeFile(t, filepath.Join(activeGlobalCache, "main", ".gitkeep"), "global\n")
	writeFile(t, filepath.Join(orphanCache, "main", ".gitkeep"), "orphan\n")

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc", "--dry-run")
	if err != nil {
		t.Fatalf("expected gc dry-run to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, orphanCache) {
		t.Fatalf("expected dry-run output to mention orphan cache %q, got:\n%s", orphanCache, output)
	}

	if strings.Contains(output, activeRepoCache) || strings.Contains(output, activeGlobalCache) {
		t.Fatalf("expected dry-run output to skip active cache directories, got:\n%s", output)
	}

	if _, err := os.Stat(orphanCache); err != nil {
		t.Fatalf("expected dry-run to leave orphan cache on disk: %v", err)
	}

	if _, err := os.Stat(filepath.Join(activeRepoCache, "v1.0.0")); err != nil {
		t.Fatalf("expected active registry cache refs to stay on disk: %v", err)
	}

	if !strings.Contains(output, "Would remove 1 cache entries") {
		t.Fatalf("expected dry-run summary in output, got:\n%s", output)
	}
}

func TestGcCommandWithoutConfigFailsClosedAndLeavesCachesUntouched(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	registryURL := "https://registry.example/unused.git"
	registryEntry := filepath.Join(cache.CacheDir(), cache.HashURL(registryURL), "main", ".gitkeep")
	writeFile(t, registryEntry, "registry cache\n")

	marketplaceCache := marketplace.DefaultCache()
	marketplaceDigest := "sha256:" + strings.Repeat("1", 64)
	marketplaceEntry, err := marketplaceCache.WriteArtifact(marketplaceDigest, []byte("marketplace cache\n"))
	if err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}

	withWorkingDir(t, repoRoot)
	output, err := executeCommand("gc")
	if err == nil {
		t.Fatalf("expected gc without configuration to fail, output:\n%s", output)
	}
	if !strings.Contains(err.Error(), "at least one effective configuration layer") {
		t.Fatalf("unexpected gc error: %v", err)
	}

	if got, readErr := os.ReadFile(registryEntry); readErr != nil {
		t.Fatalf("registry cache was removed: %v", readErr)
	} else if string(got) != "registry cache\n" {
		t.Fatalf("registry cache changed: %q", got)
	}
	if got, readErr := os.ReadFile(marketplaceEntry); readErr != nil {
		t.Fatalf("Marketplace cache was removed: %v", readErr)
	} else if string(got) != "marketplace cache\n" {
		t.Fatalf("Marketplace cache changed: %q", got)
	}
}

func TestGcCommandRunsRegistryAndMarketplaceGC(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	contentDigest := "sha256:" + strings.Repeat("1", 64)
	referencedArchive := "sha256:" + strings.Repeat("2", 64)
	orphanArchive := "sha256:" + strings.Repeat("3", 64)
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
marketplaces:
  - url: https://marketplace.example
    instance_id: mp
    resources:
      - registry_id: src
        kind: skill
        name: review
        revision: v1
        digest: `+contentDigest+`
`)
	mpCache := marketplace.DefaultCache()
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(referencedArchive),
	})
	if _, err := mpCache.WriteResourceRevision("https://marketplace.example", "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	if _, err := mpCache.WriteArtifact(referencedArchive, []byte("referenced")); err != nil {
		t.Fatalf("WriteArtifact referenced: %v", err)
	}
	if _, err := mpCache.WriteArtifact(orphanArchive, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact orphan: %v", err)
	}
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("expected combined gc to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Removed 0 cache entries") || !strings.Contains(output, "Removed 1 marketplace artifact directories") {
		t.Fatalf("expected both GC result summaries, got:\n%s", output)
	}
	if !strings.Contains(output, filepath.Dir(mpCache.ArtifactPath(orphanArchive))) {
		t.Fatalf("expected marketplace orphan in output, got:\n%s", output)
	}
	if _, err := os.Stat(mpCache.ArtifactPath(referencedArchive)); err != nil {
		t.Fatalf("referenced marketplace archive was removed: %v", err)
	}
	if _, err := os.Stat(mpCache.ArtifactPath(orphanArchive)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected marketplace orphan to be removed, stat err=%v", err)
	}
}

func TestGcCommandRemovesInactiveHashDirectoriesAndIsIdempotent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/security.git
    ref: main
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/repo-skills.git
    ref: main
`)

	activeCache := filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.com/repo-skills.git"))
	orphanCache := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/legacy-skills.git"))

	writeFile(t, filepath.Join(activeCache, "main", ".gitkeep"), "active\n")
	writeFile(t, filepath.Join(activeCache, "old-tag", ".gitkeep"), "still active because url hash is configured\n")
	writeFile(t, filepath.Join(orphanCache, "main", ".gitkeep"), "orphan\n")

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("expected gc to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, orphanCache) {
		t.Fatalf("expected gc output to mention removed orphan cache %q, got:\n%s", orphanCache, output)
	}

	if !strings.Contains(output, "Removed 1 cache entries") {
		t.Fatalf("expected removal summary in output, got:\n%s", output)
	}

	if _, err := os.Stat(orphanCache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected orphan cache to be removed, stat err=%v", err)
	}

	if _, err := os.Stat(filepath.Join(activeCache, "old-tag")); err != nil {
		t.Fatalf("expected active registry cache refs to remain: %v", err)
	}

	output, err = executeCommand("gc")
	if err != nil {
		t.Fatalf("expected second gc run to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Removed 0 cache entries") {
		t.Fatalf("expected idempotent second run summary, got:\n%s", output)
	}
}

func TestGcCommandMarketplacePreflightBlocksLegacyDeletion(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
marketplaces:
  - url: https://marketplace.example
    instance_id: mp
    resources:
      - registry_id: src
        kind: skill
        name: review
        revision: v1
        digest: sha256:`+strings.Repeat("1", 64)+`
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/active.git
    ref: main
`)
	orphanCache := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/stale.git"))
	writeFile(t, filepath.Join(orphanCache, "main", ".gitkeep"), "orphan\n")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc")
	if err == nil {
		t.Fatal("expected Marketplace preflight failure")
	}
	if _, statErr := os.Stat(orphanCache); statErr != nil {
		t.Fatalf("Marketplace preflight failure deleted legacy registry cache: %v\n%s", statErr, output)
	}
}
