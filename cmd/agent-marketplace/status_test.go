package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

func TestStatusCommandPrintsSyncedSkillsOrphansAndCollisions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: https://example.com/infra.git
    ref: main
    skills:
      - vm-lifecycle
      - shared-skill
  - url: https://example.com/security.git
    ref: main
    skills:
      - shared-skill
`)

	vmLifecycleTarget := writeCachedSkill(t, "https://example.com/infra.git", "main", "vm-lifecycle", []string{"ops"})
	writeCachedSkill(t, "https://example.com/infra.git", "main", "shared-skill", []string{"shared"})
	writeCachedSkill(t, "https://example.com/security.git", "main", "shared-skill", []string{"shared"})
	setCacheModTime(t, cache.RegistryCachePath("https://example.com/infra.git", "main"), time.Now().Add(-2*time.Hour))
	setCacheModTime(t, cache.RegistryCachePath("https://example.com/security.git", "main"), time.Now().Add(-2*time.Hour))

	// Create an orphaned skill as a managed SYMLINK (pointing to the cache so
	// it is recognized as agent-marketplace-managed). User-owned symlinks
	// (pointing outside the cache) are preserved per the coexistence principle.
	cacheDir := cache.CacheDir()
	if err := os.MkdirAll(filepath.Join(cacheDir, "compiled"), 0o755); err != nil {
		t.Fatalf("mkdir cache compiled dir: %v", err)
	}
	orphanedTarget := filepath.Join(cacheDir, "compiled", "orphaned-skill")
	if err := os.WriteFile(orphanedTarget, []byte("# orphaned"), 0o644); err != nil {
		t.Fatalf("write orphaned target: %v", err)
	}
	skillsDir := filepath.Join(repoRoot, ".factory", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.Symlink(orphanedTarget, filepath.Join(skillsDir, "orphaned-skill")); err != nil {
		t.Fatalf("symlink orphaned skill: %v", err)
	}

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	for _, needle := range []string{
		"SYNCED RESOURCES",
		"NAME",
		"HARNESS",
		"factory",
		"TARGET",
		"REGISTRY",
		"REF",
		"CACHE",
		"vm-lifecycle",
		vmLifecycleTarget,
		"https://example.com/infra.git",
		"main",
		"last fetched:",
		"ORPHANED FILES",
		"orphaned-skill",
		filepath.Join(repoRoot, ".factory", "skills", "orphaned-skill"),
		"NAME COLLISIONS",
		"shared-skill",
		"using",
		"https://example.com/infra",
		"https://example.com/security",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}
}

func TestStatusCommandMarksFreshAndStaleCaches(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: https://example.com/fresh.git
    ref: main
    skills:
      - fresh-skill
  - url: https://example.com/stale.git
    ref: main
    skills:
      - stale-skill
`)

	writeCachedSkill(t, "https://example.com/fresh.git", "main", "fresh-skill", []string{"fresh"})
	writeCachedSkill(t, "https://example.com/stale.git", "main", "stale-skill", []string{"stale"})
	setCacheModTime(t, cache.RegistryCachePath("https://example.com/fresh.git", "main"), time.Now().Add(-2*time.Hour))
	setCacheModTime(t, cache.RegistryCachePath("https://example.com/stale.git", "main"), time.Now().Add(-48*time.Hour))

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "fresh-skill") || !strings.Contains(output, "last fetched:") {
		t.Fatalf("expected fresh skill to report last fetched time, got:\n%s", output)
	}

	if !strings.Contains(output, "stale-skill") || !strings.Contains(output, "stale") {
		t.Fatalf("expected stale skill to be marked stale, got:\n%s", output)
	}
}

func TestStatusCommandCacheFreshnessWithPathSpecificEntry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: https://example.com/awesome-ai.git
    ref: main
    path: skills
    skills:
      - vm-lifecycle
`)

	// Create the cache at the base path (no ":HASH" suffix on disk).
	cachePath := cache.RegistryCachePath("https://example.com/awesome-ai.git", "main")
	writeSkill(t, cachePath, "vm-lifecycle", "vm-lifecycle", []string{"ops"})
	setCacheModTime(t, cachePath, time.Now().Add(-2*time.Hour))

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	if strings.Contains(output, "missing") {
		t.Fatalf("path-specific entry should NOT show 'missing' cache, got:\n%s", output)
	}
	if !strings.Contains(output, "last fetched:") {
		t.Fatalf("expected cache freshness to report 'last fetched:', got:\n%s", output)
	}
}

func TestStatusCommandShowsLocallyExcludedSkills(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: https://example.com/infra.git
    ref: main
    skills:
      - vm-lifecycle
      - log-triage
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), `
skills:
  exclude:
    - log-triage
`)

	writeCachedSkill(t, "https://example.com/infra.git", "main", "vm-lifecycle", []string{"ops"})
	writeCachedSkill(t, "https://example.com/infra.git", "main", "log-triage", []string{"ops"})
	setCacheModTime(t, cache.RegistryCachePath("https://example.com/infra.git", "main"), time.Now().Add(-2*time.Hour))

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	for _, needle := range []string{
		"LOCALLY EXCLUDED",
		"NAME",
		"REGISTRY",
		"REF",
		"log-triage",
		"https://example.com/infra.git",
		"main",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}

	if strings.Contains(output, "vm-lifecycle\t") && strings.Contains(output, "LOCALLY EXCLUDED\nvm-lifecycle") {
		t.Fatalf("expected vm-lifecycle not to be listed as locally excluded, got:\n%s", output)
	}
}

func TestStatusCommandShowsMalformedSkillsInErrors(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: https://example.com/awesome-ai.git
    ref: main
    skills:
      - valid-skill
`)

	cachePath := cache.RegistryCachePath("https://example.com/awesome-ai.git", "main")
	writeSkill(t, cachePath, "valid-skill", "Valid skill", []string{"tested"})
	writeFile(t, filepath.Join(cachePath, "skills", "broken-skill", "SKILL.md"), `---
name: broken-skill
description: [unterminated
metadata:
  tags:
    - broken
---

# Broken
`)
	setCacheModTime(t, cachePath, time.Now().Add(-2*time.Hour))

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	for _, needle := range []string{
		"ERRORS",
		"broken-skill",
		filepath.Join(cachePath, "skills", "broken-skill", "SKILL.md"),
		"did not find expected ',' or ']'",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}
}

func TestStatusDriftSectionInSync(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"ops"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	// Sync first so everything is on disk.
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed: %v\n%s", err, output)
	}

	// Now status should report in sync.
	output, err = executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "DRIFT") {
		t.Fatalf("expected status output to contain DRIFT section, got:\n%s", output)
	}
	if !strings.Contains(output, "In sync") {
		t.Fatalf("expected DRIFT section to show 'In sync', got:\n%s", output)
	}
}

func TestStatusDriftSectionOutOfSync(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: https://example.com/infra.git
    ref: main
    skills:
      - vm-lifecycle
`)

	writeCachedSkill(t, "https://example.com/infra.git", "main", "vm-lifecycle", []string{"ops"})
	setCacheModTime(t, cache.RegistryCachePath("https://example.com/infra.git", "main"), time.Now().Add(-2*time.Hour))

	withWorkingDir(t, repoRoot)

	// No sync yet — skill is configured but not on disk.
	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "DRIFT") {
		t.Fatalf("expected status output to contain DRIFT section, got:\n%s", output)
	}
	if strings.Contains(output, "In sync") {
		t.Fatalf("expected DRIFT section NOT to show 'In sync' when out of sync, got:\n%s", output)
	}
	if !strings.Contains(output, "file(s) to write") {
		t.Fatalf("expected DRIFT section to mention files to write, got:\n%s", output)
	}
}

func TestStatusCommandBlockedRegistryReturnsError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
allowed_registries: []
registries:
  - url: https://example.com/blocked.git
    ref: main
`)

	withWorkingDir(t, repoRoot)

	_, err := executeCommand("status")
	if err == nil {
		t.Fatal("expected status command to fail for blocked registry")
	}
	if !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Fatalf("expected error to contain 'blocked by allowed_registries', got: %v", err)
	}
}

func TestStatusReportsManagedInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission diagnostics are Unix-specific")
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: https://example.com/org.git
    ref: main
    skills:
      include:
        - managed-skill
`)
	if err := os.Chmod(orgPath, 0o664); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	config.SetTestOrganizationConfigPath(t, orgPath)

	writeCachedSkill(t, "https://example.com/org.git", "main", "managed-skill", []string{"ops"})
	setCacheModTime(t, cache.RegistryCachePath("https://example.com/org.git", "main"), time.Now().Add(-2*time.Hour))

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "registries: []\n")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	for _, needle := range []string{
		"MANAGED INSTALLATION",
		"true",
		"validation: valid",
		"managed-skill",
		"is writable by group or other (mode",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}
}

func writeCachedSkill(t *testing.T, url, ref, name string, tags []string) string {
	t.Helper()

	cachePath := cache.RegistryCachePath(url, ref)
	writeSkill(t, cachePath, name, name, tags)
	return filepath.Join(cachePath, "skills", name)
}

func setCacheModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()

	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %q: %v", path, err)
	}
}

func TestBuildHarnessTargetRowsPresentsCodexPartitionsAndLegacyRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	repoRoot := t.TempDir()
	canonicalRepo, _ := ownership.CanonicalRoot(repoRoot)
	canonicalHome, _ := ownership.CanonicalRoot(home)
	canonicalCodexHome, _ := ownership.CanonicalRoot(codexHome)

	rows, err := buildHarnessTargetRows(repoRoot, []string{"factory", "devin", "codex", "devin", " ", "not-a-harness"})
	if err != nil {
		t.Fatal(err)
	}

	want := []harnessTargetRow{
		{Target: "Codex", Capabilities: "compile,ingest,convert,tui", RepoRoot: "repo-agents=" + filepath.Join(canonicalRepo, ".agents") + ", repo-codex=" + filepath.Join(canonicalRepo, ".codex"), MachineRoot: "machine-agents=" + filepath.Join(canonicalHome, ".agents") + ", machine-codex=" + canonicalCodexHome},
		{Target: "devin", Capabilities: "compile,ingest,convert,tui", RepoRoot: filepath.Join(repoRoot, ".devin"), MachineRoot: filepath.Join(home, ".config", "devin")},
		{Target: "factory", Capabilities: "compile,ingest,convert,tui", RepoRoot: filepath.Join(repoRoot, ".factory"), MachineRoot: filepath.Join(home, ".factory")},
		{Target: "not-a-harness", Capabilities: "unknown", RepoRoot: "-", MachineRoot: "-"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("harness target rows =\n%#v\nwant\n%#v", rows, want)
	}

	var out bytes.Buffer
	if err := writeStatusTable(&out, "HARNESS TARGETS", []string{"TARGET", "CAPABILITIES", "REPO ROOT", "MACHINE ROOT"}, harnessTargetRowsToCells(rows)); err != nil {
		t.Fatalf("write table: %v", err)
	}
	for _, needle := range []string{"HARNESS TARGETS", "devin", "Codex", "compile,ingest,convert,tui", filepath.Join(repoRoot, ".devin"), filepath.Join(home, ".config", "devin"), "repo-agents=", "repo-codex=", "machine-agents=", "machine-codex="} {
		if !strings.Contains(out.String(), needle) {
			t.Fatalf("status HARNESS TARGETS section missing %q:\n%s", needle, out.String())
		}
	}
}

func TestBuildHarnessTargetRowsRejectsRelativeCodexHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "relative/codex")
	if _, err := buildHarnessTargetRows(t.TempDir(), []string{"codex"}); err == nil || !strings.Contains(err.Error(), "CODEX_HOME must be absolute") {
		t.Fatalf("buildHarnessTargetRows error = %v", err)
	}
}

func TestTargetDisplayNameLabelsOnlyCodex(t *testing.T) {
	for input, want := range map[string]string{"codex": "Codex", "factory": "factory", "unknown": "unknown"} {
		if got := targetDisplayName(input); got != want {
			t.Errorf("targetDisplayName(%q) = %q, want %q", input, got, want)
		}
	}
}
