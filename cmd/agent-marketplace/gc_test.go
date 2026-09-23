package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestGcCommandUsesVerifiedTransientOrganizationMarketplaceLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, dryRun := range []bool{true, false} {
		name := "gc"
		if dryRun {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			var requests atomic.Int32
			var activeDigest string
			server := newV4MarketplaceLockServerWithRevision(t, func() { requests.Add(1) }, func(revision *store.AssessedResourceRevision) {
				activeDigest = string(revision.ArchiveDigest)
			})
			defer server.Close()
			orgPath := filepath.Join(canonicalTempDir(t), "config.yaml")
			orgConfig := "apiVersion: agent-marketplace/v4\nallowed_marketplaces: ['^" + regexp.QuoteMeta(server.URL) + "$']\nsources:\n  hosted: {marketplace: " + server.URL + "}\nresources:\n  hosted: {include: [tools/skill/review]}\n"
			writeFile(t, orgPath, orgConfig)
			config.SetTestOrganizationConfigPath(t, orgPath)
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			repo := t.TempDir()
			withWorkingDir(t, repo)
			marketplaceCache := marketplace.DefaultCache()
			orphanDigest := "sha256:" + strings.Repeat("f", 64)
			if activeDigest == orphanDigest {
				t.Fatal("orphan digest matches active resource")
			}
			orphanArchive, err := marketplaceCache.WriteArtifact(orphanDigest, []byte("orphan"))
			if err != nil {
				t.Fatal(err)
			}
			orphanRegistry := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
			writeFile(t, orphanRegistry, "orphan\n")
			args := []string{"gc"}
			if dryRun {
				args = append(args, "--dry-run")
			}
			output, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("%v rejected verified organization Marketplace resource: %v\n%s", args, err, output)
			}
			for _, path := range []string{filepath.Dir(orphanArchive), filepath.Dir(filepath.Dir(orphanRegistry))} {
				if !strings.Contains(output, path) {
					t.Fatalf("%v did not report orphan %s:\n%s", args, path, output)
				}
			}
			if strings.Contains(output, filepath.Dir(marketplaceCache.ArtifactPath(activeDigest))) {
				t.Fatalf("gc nominated active Marketplace archive:\n%s", output)
			}
			if requests.Load() != 5 {
				t.Fatalf("gc made %d Marketplace requests, want one five-request acquisition", requests.Load())
			}
			if _, err := os.Stat(marketplaceCache.ArtifactPath(activeDigest)); err != nil {
				t.Fatalf("gc removed active Marketplace archive: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(orgPath), "lock.yaml")); !os.IsNotExist(err) {
				t.Fatalf("gc created organization lock: %v", err)
			}
			if got, err := os.ReadFile(orgPath); err != nil || string(got) != orgConfig {
				t.Fatalf("gc changed organization config: %v\n%s", err, got)
			}
			for _, path := range []string{orphanArchive, orphanRegistry} {
				_, err := os.Stat(path)
				if dryRun && err != nil || !dryRun && !os.IsNotExist(err) {
					t.Fatalf("%v orphan %s survival = %v, want dry-run=%t", args, path, err, dryRun)
				}
			}
		})
	}
}

func TestGcCommandRejectsUnverifiedOrganizationMarketplaceWithoutDeleting(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scenario := range []string{"unavailable", "malformed lock", "mismatched lock"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			authority := canonicalTempDir(t)
			orgPath := filepath.Join(authority, "config.yaml")
			orgConfig := "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: " + server.URL + "}\nresources:\n  hosted: {include: [tools/skill/review]}\n"
			writeFile(t, orgPath, orgConfig)
			config.SetTestOrganizationConfigPath(t, orgPath)
			writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			lockPath := filepath.Join(authority, "lock.yaml")
			var lock []byte
			switch scenario {
			case "malformed lock":
				lock = []byte("not: [valid\n")
			case "mismatched lock":
				var err error
				lock, err = lockfile.Encode(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeOrganization, Marketplaces: []lockfile.MarketplaceLock{{URL: "https://other.example", InstanceID: "other"}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if lock != nil {
				if err := os.WriteFile(lockPath, lock, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			marketplaceCache := marketplace.DefaultCache()
			orphanArchive, err := marketplaceCache.WriteArtifact("sha256:"+strings.Repeat("f", 64), []byte("keep-artifact"))
			if err != nil {
				t.Fatal(err)
			}
			orphanRegistry := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
			writeFile(t, orphanRegistry, "keep-registry\n")
			withWorkingDir(t, t.TempDir())

			output, err := executeCommand("gc")
			if err == nil {
				t.Fatalf("gc accepted %s organization source:\n%s", scenario, output)
			}
			if scenario == "unavailable" && requests.Load() == 0 || scenario != "unavailable" && requests.Load() != 0 {
				t.Fatalf("%s made %d Marketplace requests", scenario, requests.Load())
			}
			if scenario == "unavailable" && !strings.Contains(err.Error(), "acquire transient organization Marketplace lock") || scenario == "malformed lock" && !strings.Contains(err.Error(), "lock.malformed") || scenario == "mismatched lock" && !strings.Contains(err.Error(), "v4.marketplace-lock-mismatch") {
				t.Fatalf("gc returned wrong error for %s: %v", scenario, err)
			}
			for path, want := range map[string]string{orphanArchive: "keep-artifact", orphanRegistry: "keep-registry\n", orgPath: orgConfig} {
				if after, readErr := os.ReadFile(path); readErr != nil || string(after) != want {
					t.Fatalf("gc changed %s after %s: %v\n%s", path, scenario, readErr, after)
				}
			}
			if lock == nil {
				if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
					t.Fatalf("gc created organization lock: %v", err)
				}
			} else if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lock) {
				t.Fatalf("gc changed organization lock: %v\n%s", err, after)
			}
		})
	}
}

type gcPreflightLockInjector struct {
	lockPath string
	mutated  atomic.Bool
	failed   error
}

func (w *gcPreflightLockInjector) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("marketplace cache garbage collection completed")) && bytes.Contains(p, []byte("dry_run=true")) && w.mutated.CompareAndSwap(false, true) {
		w.failed = os.WriteFile(w.lockPath, []byte("not: [valid\n"), 0o600)
	}
	return len(p), nil
}

func TestGcCommandRefusesOrganizationLockAppearingAfterPreflight(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var activeDigest string
	server := newV4MarketplaceLockServerWithRevision(t, nil, func(revision *store.AssessedResourceRevision) {
		activeDigest = string(revision.ArchiveDigest)
	})
	defer server.Close()
	authority := canonicalTempDir(t)
	orgPath := filepath.Join(authority, "config.yaml")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+server.URL+"}\nresources:\n  hosted: {include: [tools/skill/review]}\n")
	config.SetTestOrganizationConfigPath(t, orgPath)
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	orphanArchive, err := marketplace.DefaultCache().WriteArtifact("sha256:"+strings.Repeat("f", 64), []byte("preserve-artifact"))
	if err != nil {
		t.Fatal(err)
	}
	orphanRegistry := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
	writeFile(t, orphanRegistry, "preserve-registry\n")
	withWorkingDir(t, t.TempDir())

	injector := &gcPreflightLockInjector{lockPath: filepath.Join(authority, "lock.yaml")}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(injector, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	var out strings.Builder
	err = runGC(context.Background(), &out, false)
	if !injector.mutated.Load() || injector.failed != nil {
		t.Fatalf("test did not deploy lock after preflight: %v", injector.failed)
	}
	if err == nil || !strings.Contains(err.Error(), "lock.malformed") {
		t.Fatalf("gc accepted lock deployed after preflight: %v\n%s", err, out.String())
	}
	for path, want := range map[string]string{orphanArchive: "preserve-artifact", orphanRegistry: "preserve-registry\n"} {
		if got, readErr := os.ReadFile(path); readErr != nil || string(got) != want {
			t.Fatalf("gc deleted %s after organization lock appeared: %v\n%s", path, readErr, got)
		}
	}
	if _, err := os.Stat(marketplace.DefaultCache().ArtifactPath(activeDigest)); err != nil {
		t.Fatalf("gc lost the active Marketplace archive: %v", err)
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

// spec: cache-garbage-collection/configuration-or-lock-changes-while-waiting-for-lease
func TestGcCommandReadsConfigChangedWhileWaitingForLease(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	userConfig := filepath.Join(homeDir, ".agents", "config.yaml")
	writeConfig(t, userConfig, `registries:
  - url: ssh://git@example.com/kept.git
    ref: main
`)
	writeFile(t, filepath.Join(homeDir, ".agents", "lock.yaml"), "apiVersion: agent-marketplace-lock/v1\nscope: user\n")

	kept := filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.com/kept.git"), "main", ".gitkeep")
	added := filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.com/added-while-waiting.git"), "main", ".gitkeep")
	orphan := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
	for _, path := range []string{kept, added, orphan} {
		writeFile(t, path, "cache\n")
	}

	withWorkingDir(t, repoRoot)

	release, err := cache.AcquireExactGitNamespaceLeaseWithContext(context.Background())
	if err != nil {
		t.Fatalf("acquire shared exact lease: %v", err)
	}
	gcDone := make(chan struct{})
	var output string
	var gcErr error
	go func() {
		defer close(gcDone)
		output, gcErr = executeCommand("gc")
	}()
	time.Sleep(200 * time.Millisecond)
	writeConfig(t, userConfig, `registries:
  - url: ssh://git@example.com/kept.git
    ref: main
  - url: ssh://git@example.com/added-while-waiting.git
    ref: main
`)
	release()
	<-gcDone

	if gcErr != nil {
		t.Fatalf("gc failed: %v\n%s", gcErr, output)
	}
	if _, err := os.Stat(added); err != nil {
		t.Fatalf("cache made reachable while GC waited for its lease was removed: %v", err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced cache was not collected: %v", err)
	}
}

// spec: cache-garbage-collection/configuration-or-lock-changes-while-waiting-for-lease
func TestGcCommandReadsDeployedLockChangedWhileWaitingForLease(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `registries:
  - url: ssh://git@example.com/kept.git
    ref: main
`)
	lockPath := filepath.Join(homeDir, ".agents", "lock.yaml")
	writeFile(t, lockPath, "apiVersion: agent-marketplace-lock/v1\nscope: user\n")

	kept := filepath.Join(cache.CacheDir(), cache.HashURL("ssh://git@example.com/kept.git"), "main", ".gitkeep")
	orphan := filepath.Join(cache.CacheDir(), cache.HashURL("https://git.example.com/orphan.git"), "main", ".gitkeep")
	for _, path := range []string{kept, orphan} {
		writeFile(t, path, "cache\n")
	}

	withWorkingDir(t, repoRoot)

	release, err := cache.AcquireExactGitNamespaceLeaseWithContext(context.Background())
	if err != nil {
		t.Fatalf("acquire shared exact lease: %v", err)
	}
	gcDone := make(chan struct{})
	var output string
	var gcErr error
	go func() {
		defer close(gcDone)
		output, gcErr = executeCommand("gc")
	}()
	time.Sleep(200 * time.Millisecond)
	writeFile(t, lockPath, "apiVersion: agent-marketplace-lock/v1\nscope: user\nregistries: [\n")
	release()
	<-gcDone

	if gcErr == nil {
		t.Fatalf("gc succeeded after deployed lock turned malformed while waiting for its lease:\n%s", output)
	}
	for _, path := range []string{kept, orphan} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("fail-closed gc deleted %q: %v", path, err)
		}
	}
}

type dialGuardTransport struct {
	dialed *atomic.Int32
}

func (g dialGuardTransport) RoundTrip(*http.Request) (*http.Response, error) {
	g.dialed.Add(1)
	return nil, errors.New("gc made an unexpected network request")
}

type organizationMarketplaceGCFixture struct {
	authority   string
	lockPath    string
	cache       *marketplace.Cache
	dialed      *atomic.Int32
	beforeNames []string
	beforeMod   time.Time
}

func newOrganizationMarketplaceGCFixture(t *testing.T, orgConfig, orgLock string) *organizationMarketplaceGCFixture {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	dialed := &atomic.Int32{}
	transport := http.DefaultTransport
	http.DefaultTransport = dialGuardTransport{dialed: dialed}
	t.Cleanup(func() { http.DefaultTransport = transport })

	authority := canonicalTempDir(t)
	writeConfig(t, filepath.Join(authority, "config.yaml"), orgConfig)
	fixture := &organizationMarketplaceGCFixture{
		authority: authority,
		lockPath:  filepath.Join(authority, "lock.yaml"),
		cache:     marketplace.DefaultCache(),
		dialed:    dialed,
	}
	if orgLock != "" {
		writeFile(t, fixture.lockPath, orgLock)
	}
	config.SetTestOrganizationConfigPath(t, filepath.Join(authority, "config.yaml"))

	entries, err := os.ReadDir(authority)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		fixture.beforeNames = append(fixture.beforeNames, entry.Name())
	}
	info, err := os.Stat(authority)
	if err != nil {
		t.Fatal(err)
	}
	fixture.beforeMod = info.ModTime()

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)
	return fixture
}

func (f *organizationMarketplaceGCFixture) assertOrganizationDirUnchanged(t *testing.T) {
	t.Helper()
	if got := f.dialed.Load(); got != 0 {
		t.Fatalf("gc made %d network requests", got)
	}
	entries, err := os.ReadDir(f.authority)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Equal(f.beforeNames, names) {
		t.Fatalf("organization directory listing changed: %v -> %v", f.beforeNames, names)
	}
	info, err := os.Stat(f.authority)
	if err != nil {
		t.Fatal(err)
	}
	if !f.beforeMod.Equal(info.ModTime()) {
		t.Fatalf("organization directory mtime changed: %v -> %v", f.beforeMod, info.ModTime())
	}
}

// spec: cache-garbage-collection/organization-marketplace-without-a-lock
func TestGcCommandV4OrganizationMarketplaceWithoutLockCollects(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newOrganizationMarketplaceGCFixture(t, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: https://marketplace.invalid}
`, "")

	contentDigest := "sha256:" + strings.Repeat("1", 64)
	referencedArchive := "sha256:" + strings.Repeat("2", 64)
	orphanArchive := "sha256:" + strings.Repeat("3", 64)
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(referencedArchive),
	})
	if _, err := fixture.cache.WriteResourceRevision("https://marketplace.invalid", "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	if _, err := fixture.cache.WriteArtifact(referencedArchive, []byte("referenced")); err != nil {
		t.Fatalf("WriteArtifact referenced: %v", err)
	}
	if _, err := fixture.cache.WriteArtifact(orphanArchive, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact orphan: %v", err)
	}

	output, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("expected gc to collect with a lock-free organization Marketplace source: %v\n%s", err, output)
	}
	orphanDir := filepath.Dir(fixture.cache.ArtifactPath(orphanArchive))
	if !strings.Contains(output, orphanDir) || !strings.Contains(output, "Removed 1 marketplace artifact directories") {
		t.Fatalf("expected marketplace orphan removal in output, got:\n%s", output)
	}
	if _, err := os.Stat(fixture.cache.ArtifactPath(referencedArchive)); err != nil {
		t.Fatalf("active marketplace artifact was removed: %v", err)
	}
	if _, err := os.Stat(orphanDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected marketplace orphan to be removed, stat err=%v", err)
	}
	fixture.assertOrganizationDirUnchanged(t)
}

// spec: cache-garbage-collection/organization-marketplace-with-empty-cache
func TestGcCommandV4OrganizationMarketplaceEmptyCacheIsNoop(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newOrganizationMarketplaceGCFixture(t, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: https://marketplace.invalid}
`, "")

	output, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("expected gc to collect an empty organization Marketplace cache: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Removed 0 cache entries") || !strings.Contains(output, "Removed 0 marketplace artifact directories") {
		t.Fatalf("expected zero removals in output, got:\n%s", output)
	}
	if _, err := os.Lstat(fixture.lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("organization acquisition lock created: %v", err)
	}
	fixture.assertOrganizationDirUnchanged(t)
}

// spec: cache-garbage-collection/trusted-organization-lock-retains-exact-archive
func TestGcCommandV4OrganizationMarketplaceTrustedLockRetainsExactReferences(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	referencedArchive := "sha256:" + strings.Repeat("2", 64)
	orphanArchive := "sha256:" + strings.Repeat("3", 64)
	fixture := newOrganizationMarketplaceGCFixture(t, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: https://marketplace.invalid}
resources:
  organization:
    include: [src/skill/review]
`, `apiVersion: agent-marketplace-lock/v2
scope: organization
marketplaces:
  - url: https://marketplace.invalid
    instance_id: mp
    registry_bindings:
      - name: src
        registry_id: src-id
    resources:
      - registry_name: src
        registry_id: src-id
        kind: skill
        name: review
        revision: rev-a
        content_digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
        archive_digest: `+referencedArchive+`
        acquisition: mirror
`)
	lockBytes, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.cache.WriteArtifact(referencedArchive, []byte("referenced")); err != nil {
		t.Fatalf("WriteArtifact referenced: %v", err)
	}
	if _, err := fixture.cache.WriteArtifact(orphanArchive, []byte("orphan")); err != nil {
		t.Fatalf("WriteArtifact orphan: %v", err)
	}

	output, err := executeCommand("gc")
	if err != nil {
		t.Fatalf("expected gc to collect under the trusted organization lock: %v\n%s", err, output)
	}
	if _, err := os.Stat(fixture.cache.ArtifactPath(referencedArchive)); err != nil {
		t.Fatalf("locked marketplace archive was removed: %v", err)
	}
	if _, err := os.Stat(fixture.cache.ArtifactPath(orphanArchive)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unreferenced marketplace archive to be removed, stat err=%v", err)
	}
	got, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(lockBytes) || fmt.Sprintf("%x", sha256.Sum256(got)) != fmt.Sprintf("%x", sha256.Sum256(lockBytes)) {
		t.Fatal("organization acquisition lock changed")
	}
	fixture.assertOrganizationDirUnchanged(t)
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

// spec: cache-garbage-collection/mismatched-organization-lock
func TestGcCommandV4OrganizationMarketplaceLockMismatchFailsClosed(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newOrganizationMarketplaceGCFixture(t, `apiVersion: agent-marketplace/v4
sources:
  organization: {marketplace: https://marketplace.invalid}
`, `apiVersion: agent-marketplace-lock/v2
scope: organization
marketplaces:
  - url: https://other-marketplace.invalid
    instance_id: other
`)

	contentDigest := "sha256:" + strings.Repeat("1", 64)
	referencedArchive := "sha256:" + strings.Repeat("2", 64)
	orphanArchive := "sha256:" + strings.Repeat("3", 64)
	revision := testAssessedMarketplaceRevision(t, store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(contentDigest),
		ArchiveDigest: store.ArchiveDigest(referencedArchive),
	})
	if _, err := fixture.cache.WriteResourceRevision("https://marketplace.invalid", "src", "skill", "review", contentDigest, revision); err != nil {
		t.Fatalf("WriteResourceRevision: %v", err)
	}
	for _, artifact := range []string{referencedArchive, orphanArchive} {
		if _, err := fixture.cache.WriteArtifact(artifact, []byte("cache")); err != nil {
			t.Fatalf("WriteArtifact: %v", err)
		}
	}

	output, err := executeCommand("gc")
	if err == nil || !strings.Contains(err.Error(), "v4.marketplace-lock-mismatch") {
		t.Fatalf("gc error = %v\n%s; want mismatched organization Marketplace lock rejection", err, output)
	}
	for _, artifact := range []string{referencedArchive, orphanArchive} {
		if _, statErr := os.Stat(fixture.cache.ArtifactPath(artifact)); statErr != nil {
			t.Fatalf("mismatched lock mutated marketplace cache %q: %v", artifact, statErr)
		}
	}
	fixture.assertOrganizationDirUnchanged(t)
}
