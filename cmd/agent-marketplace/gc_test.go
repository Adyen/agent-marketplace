package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestGcCommandDryRunReportsOrphanedCacheDirectoriesWithoutDeleting(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/security.git
    ref: main
`)
	writeFile(t, filepath.Join(homeDir, ".agents", "lock.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: user\n")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/repo-skills.git
    ref: main
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: repo\n")

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
	if isolateCommandTest(t) {
		return
	}
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

func TestGcCommandExactObjectsUseCurrentContext(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	orgPath := filepath.Join(canonicalTempDir(t), "config.yaml")
	writeConfig(t, orgPath, "include: [included.yaml]\nregistries:\n  - url: https://EXAMPLE.com/organization.git\n    ref: main\n")
	writeConfig(t, filepath.Join(filepath.Dir(orgPath), "included.yaml"), "registries:\n  - url: https://example.com/included\n    ref: main\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	repos := []string{filepath.Join(t.TempDir(), "repo-a"), filepath.Join(t.TempDir(), "repo-b")}
	for i, repo := range repos {
		writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), fmt.Sprintf("registries:\n  - url: https://example.com/repo-%d\n    ref: main\n", i))
		writeFile(t, filepath.Join(repo, ".agents", "lock.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: repo\n")
	}
	objectPath := func(source string) string {
		return filepath.Join(cache.CacheDir(), "exact-git", "objects", fmt.Sprintf("%x.git", sha256.Sum256([]byte(source))))
	}
	retained := []string{objectPath("https://example.com/organization"), objectPath("https://example.com/included"), objectPath("https://example.com/repo-0")}
	evicted := objectPath("https://example.com/repo-1")
	for _, path := range append(append([]string{}, retained...), evicted) {
		writeFile(t, filepath.Join(path, "marker"), "cached\n")
	}
	withWorkingDir(t, repos[0])
	for _, args := range [][]string{{"gc", "--dry-run"}, {"gc"}} {
		output, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		if !strings.Contains(output, evicted) || !strings.Contains(output, "1 cache entries") {
			t.Fatalf("other repository store not nominated: %s", output)
		}
		for _, path := range retained {
			if strings.Contains(output, path) {
				t.Fatalf("active store nominated: %s", output)
			}
			raw, err := os.ReadFile(filepath.Join(path, "marker"))
			if err != nil || string(raw) != "cached\n" {
				t.Fatalf("active store changed: %q, %v", raw, err)
			}
		}
		_, err = os.Stat(evicted)
		if len(args) == 2 && err != nil || len(args) == 1 && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%v eviction result: %v", args, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("organization lock created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repos[1], ".agents", "config.yaml")); err != nil {
		t.Fatalf("other repository config changed: %v", err)
	}
}

func TestGcCommandAllowsMissingOrganizationLockWithoutCreatingIt(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, "registries:\n  - url: https://git.example.com/organization.git\n    ref: main\n    required: true\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	lockPath := filepath.Join(authority, "lock.yaml")
	orphan := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
	writeFile(t, orphan, "orphan\n")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("gc rejected missing optional organization lock: %v\n%s", err, output)
	}
	if _, statErr := os.Lstat(lockPath); !os.IsNotExist(statErr) {
		t.Fatalf("gc created or replaced organization lock: %v", statErr)
	}
	if _, statErr := os.Stat(orphan); !os.IsNotExist(statErr) {
		t.Fatalf("gc did not remove orphan cache: %v", statErr)
	}
}

func TestGcCommandRejectsPresentOrganizationLockWhenMalformedUntrustedOrStale(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		name    string
		lock    string
		symlink bool
		want    error
	}{
		{name: "malformed", lock: "apiVersion: agent-marketplace-lock/v1\nscope: organization\nregistries: [\n", want: errors.New("organization.trust.lock.malformed")},
		{name: "stale", lock: "apiVersion: agent-marketplace-lock/v1\nscope: organization\n", want: errors.New("lock.stale")},
		{name: "untrusted symlink", lock: "apiVersion: agent-marketplace-lock/v1\nscope: organization\n", symlink: true, want: errors.New("organization.trust.lock.symlink")},
	} {
		t.Run(test.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

			authority := canonicalTempDir(t)
			orgPath := filepath.Join(authority, "config.yaml")
			writeConfig(t, orgPath, "registries:\n  - url: https://git.example.com/organization.git\n    ref: main\n    required: true\n")
			config.SetTestOrganizationConfigPath(t, orgPath)
			lockPath := filepath.Join(authority, "lock.yaml")
			if test.symlink {
				target := filepath.Join(authority, "lock-target.yaml")
				writeFile(t, target, test.lock)
				if err := os.Symlink(target, lockPath); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, lockPath, test.lock)
			}
			orphan := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
			writeFile(t, orphan, "orphan\n")

			repoRoot := filepath.Join(t.TempDir(), "repo")
			if err := os.MkdirAll(repoRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, repoRoot)

			output, err := executeCommand("gc")
			if err == nil || !strings.Contains(err.Error(), test.want.Error()) {
				t.Fatalf("gc error = %v, want %q\n%s", err, test.want, output)
			}
			if _, statErr := os.Stat(orphan); statErr != nil {
				t.Fatalf("rejected organization lock mutated cache: %v", statErr)
			}
		})
	}
}

func TestGcCommandMissingRequiredLockFailsClosedAndLeavesCacheUntouched(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: https://git.example.com/required.git
    ref: main
`)
	orphan := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
	writeFile(t, orphan, "keep\n")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc")
	if err == nil || !strings.Contains(err.Error(), "required REPO layer lock") {
		t.Fatalf("expected missing required lock error, got %v\n%s", err, output)
	}
	if raw, err := os.ReadFile(orphan); err != nil || string(raw) != "keep\n" {
		t.Fatalf("missing lock mutated cache: raw=%q err=%v", raw, err)
	}
}

func TestGcCommandRejectsOversizedLockAndLeavesCacheUntouched(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: https://git.example.com/required.git
    ref: main
`)
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(lockfile.MaxFileSize + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
	writeFile(t, orphan, "keep\n")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc")
	if !errors.Is(err, lockfile.ErrFileTooLarge) {
		t.Fatalf("gc error = %v\n%s; want oversized lock", err, output)
	}
	if raw, readErr := os.ReadFile(orphan); readErr != nil || string(raw) != "keep\n" {
		t.Fatalf("oversized lock mutated cache: raw=%q err=%v", raw, readErr)
	}
}

func TestGcCommandRemovesInactiveHashDirectoriesAndIsIdempotent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/security.git
    ref: main
`)
	writeFile(t, filepath.Join(homeDir, ".agents", "lock.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: user\n")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: ssh://git@example.com/repo-skills.git
    ref: main
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: repo\n")

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

func TestGcCommandRunsRegistryAndMarketplaceGC(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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

type dialGuardTransport struct {
	dialed *atomic.Int32
}

func (g dialGuardTransport) RoundTrip(*http.Request) (*http.Response, error) {
	g.dialed.Add(1)
	return nil, errors.New("gc made an unexpected network request")
}

func TestGcCommandV4OrganizationMarketplaceWithoutLockCollects(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	var dialed atomic.Int32
	transport := http.DefaultTransport
	http.DefaultTransport = dialGuardTransport{dialed: &dialed}
	t.Cleanup(func() { http.DefaultTransport = transport })

	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: https://marketplace.invalid}
`)
	config.SetTestOrganizationConfigPath(t, orgPath)

	beforeEntries, err := os.ReadDir(authority)
	if err != nil {
		t.Fatal(err)
	}
	var beforeNames []string
	for _, entry := range beforeEntries {
		beforeNames = append(beforeNames, entry.Name())
	}
	beforeInfo, err := os.Stat(authority)
	if err != nil {
		t.Fatal(err)
	}

	contentDigest := "sha256:" + strings.Repeat("1", 64)
	referencedArchive := "sha256:" + strings.Repeat("2", 64)
	orphanArchive := "sha256:" + strings.Repeat("3", 64)
	mpCache := marketplace.DefaultCache()
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(referencedArchive),
	})
	if _, err := mpCache.WriteResourceRevision("https://marketplace.invalid", "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	if _, err := mpCache.WriteArtifact(referencedArchive, []byte("referenced")); err != nil {
		t.Fatalf("WriteArtifact referenced: %v", err)
	}
	if _, err := mpCache.WriteArtifact(orphanArchive, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact orphan: %v", err)
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("expected gc to collect with a lock-free organization Marketplace source: %v\n%s", err, output)
	}
	orphanDir := filepath.Dir(mpCache.ArtifactPath(orphanArchive))
	if !strings.Contains(output, orphanDir) || !strings.Contains(output, "Removed 1 marketplace artifact directories") {
		t.Fatalf("expected marketplace orphan removal in output, got:\n%s", output)
	}
	if _, err := os.Stat(mpCache.ArtifactPath(referencedArchive)); err != nil {
		t.Fatalf("active marketplace artifact was removed: %v", err)
	}
	if _, err := os.Stat(orphanDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected marketplace orphan to be removed, stat err=%v", err)
	}
	if got := dialed.Load(); got != 0 {
		t.Fatalf("gc made %d network requests", got)
	}

	afterEntries, err := os.ReadDir(authority)
	if err != nil {
		t.Fatal(err)
	}
	var afterNames []string
	for _, entry := range afterEntries {
		afterNames = append(afterNames, entry.Name())
	}
	if !slices.Equal(beforeNames, afterNames) {
		t.Fatalf("organization directory listing changed: %v -> %v", beforeNames, afterNames)
	}
	afterInfo, err := os.Stat(authority)
	if err != nil {
		t.Fatal(err)
	}
	if !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatalf("organization directory mtime changed: %v -> %v", beforeInfo.ModTime(), afterInfo.ModTime())
	}
}

func TestGcCommandMarketplacePreflightBlocksLegacyDeletion(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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
	writeFile(t, filepath.Join(repoRoot, ".agents", "lock.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: repo\n")
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

func TestGcCommandV4OrganizationMarketplaceLockMismatchFailsClosed(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	var dialed atomic.Int32
	transport := http.DefaultTransport
	http.DefaultTransport = dialGuardTransport{dialed: &dialed}
	t.Cleanup(func() { http.DefaultTransport = transport })

	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	writeConfig(t, orgPath, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: https://marketplace.invalid}
`)
	writeFile(t, filepath.Join(authority, "lock.yaml"), `apiVersion: agent-marketplace-lock/v2
scope: organization
marketplaces:
  - url: https://other-marketplace.invalid
    instance_id: other
`)
	config.SetTestOrganizationConfigPath(t, orgPath)

	beforeEntries, err := os.ReadDir(authority)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(authority)
	if err != nil {
		t.Fatal(err)
	}

	contentDigest := "sha256:" + strings.Repeat("1", 64)
	referencedArchive := "sha256:" + strings.Repeat("2", 64)
	orphanArchive := "sha256:" + strings.Repeat("3", 64)
	mpCache := marketplace.DefaultCache()
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(referencedArchive),
	})
	if _, err := mpCache.WriteResourceRevision("https://marketplace.invalid", "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	for _, artifact := range []string{referencedArchive, orphanArchive} {
		if _, err := mpCache.WriteArtifact(artifact, []byte("cache")); err != nil {
			t.Fatalf("WriteArtifact: %v", err)
		}
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("gc")
	if err == nil || !strings.Contains(err.Error(), "v4.marketplace-lock-mismatch") {
		t.Fatalf("gc error = %v\n%s; want mismatched organization Marketplace lock rejection", err, output)
	}
	for _, artifact := range []string{referencedArchive, orphanArchive} {
		if _, statErr := os.Stat(mpCache.ArtifactPath(artifact)); statErr != nil {
			t.Fatalf("mismatched lock mutated marketplace cache %q: %v", artifact, statErr)
		}
	}
	if got := dialed.Load(); got != 0 {
		t.Fatalf("gc made %d network requests", got)
	}
	afterEntries, err := os.ReadDir(authority)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeEntries) != len(afterEntries) {
		t.Fatalf("organization directory entries changed: %d -> %d", len(beforeEntries), len(afterEntries))
	}
	afterInfo, err := os.Stat(authority)
	if err != nil {
		t.Fatal(err)
	}
	if !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatalf("organization directory mtime changed: %v -> %v", beforeInfo.ModTime(), afterInfo.ModTime())
	}
}
