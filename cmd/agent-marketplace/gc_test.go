package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
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
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	registryURL := "https://registry.example/unused.git"
	registryEntry := filepath.Join(cache.CacheDir(), cache.HashURL(registryURL), "main", ".gitkeep")
	writeFile(t, registryEntry, "registry cache\n")

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
}

func TestGcCommandAllowsMissingOrganizationLockWithoutCreatingIt(t *testing.T) {
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
