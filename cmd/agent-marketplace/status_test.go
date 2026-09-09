package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

func scenarioStatusCommandPrintsSyncedSkillsOrphansAndCollisions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	infraOrigin, infraWorking := createBareRepoWithWorkingClone(t, "status-infra.git")
	writeSkill(t, infraWorking, "vm-lifecycle", "VM lifecycle", []string{"ops"})
	writeSkill(t, infraWorking, "shared-skill", "Shared", []string{"shared"})
	commitAndPushAll(t, infraWorking, "infra resources")
	securityOrigin, securityWorking := createBareRepoWithWorkingClone(t, "status-security.git")
	writeSkill(t, securityWorking, "shared-skill", "Shared", []string{"shared"})
	commitAndPushAll(t, securityWorking, "security resources")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+infraOrigin+`
    ref: main
    skills:
      - vm-lifecycle
      - shared-skill
  - url: `+securityOrigin+`
    ref: main
    skills:
      - shared-skill
`)

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
	requireExactLocks(t)

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
		infraOrigin,
		"main",
		"locked: sha1:",
		"ORPHANED FILES",
		"orphaned-skill",
		filepath.Join(repoRoot, ".factory", "skills", "orphaned-skill"),
		"NAME COLLISIONS",
		"shared-skill",
		"using",
		strings.TrimSuffix(infraOrigin, ".git"),
		strings.TrimSuffix(securityOrigin, ".git"),
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}
}

func scenarioStatusKeepsExactLeaseThroughDryRunConsumption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	origin, working := createBareRepoWithWorkingClone(t, "status-gc.git")
	writeSkill(t, working, "keeper", "Keeper", nil)
	commitAndPushAll(t, working, "keeper")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills: [keeper]
`)
	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	entered := make(chan struct{})
	resume := make(chan struct{})
	previous := runStatusMaterialize
	runStatusMaterialize = func(ctx context.Context, root string, artifacts []resolver.ResolvedArtifact, opts ...materialize.RunOption) (*materialize.Report, error) {
		close(entered)
		<-resume
		return previous(ctx, root, artifacts, opts...)
	}
	t.Cleanup(func() { runStatusMaterialize = previous })

	statusDone := make(chan error, 1)
	go func() { statusDone <- runStatus(context.Background(), &bytes.Buffer{}, time.Now()) }()
	<-entered
	gcDone := make(chan error, 1)
	go func() {
		_, err := cache.GarbageCollectWithLocks(false, nil, &config.Config{})
		gcDone <- err
	}()
	select {
	case err := <-gcDone:
		t.Fatalf("GC completed while status consumed exact snapshot: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(resume)
	if err := <-statusDone; err != nil {
		t.Fatal(err)
	}
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
}

func scenarioStatusCommandMarksLockedCaches(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	freshOrigin, freshWorking := createBareRepoWithWorkingClone(t, "status-fresh.git")
	writeSkill(t, freshWorking, "fresh-skill", "Fresh", []string{"fresh"})
	commitAndPushAll(t, freshWorking, "fresh resource")
	staleOrigin, staleWorking := createBareRepoWithWorkingClone(t, "status-stale.git")
	writeSkill(t, staleWorking, "stale-skill", "Stale", []string{"stale"})
	commitAndPushAll(t, staleWorking, "stale resource")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+freshOrigin+`
    ref: main
    skills:
      - fresh-skill
  - url: `+staleOrigin+`
    ref: main
    skills:
      - stale-skill
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	for _, skill := range []string{"fresh-skill", "stale-skill"} {
		if !strings.Contains(output, skill) || !strings.Contains(output, "locked: sha1:") {
			t.Fatalf("expected %s to report exact lock freshness, got:\n%s", skill, output)
		}
	}
}

func scenarioStatusCommandCacheFreshnessWithPathSpecificEntry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "status-path-specific.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"ops"})
	commitAndPushAll(t, working, "path-specific resource")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    path: skills
    skills:
      - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	if strings.Contains(output, "missing") {
		t.Fatalf("path-specific entry should NOT show 'missing' cache, got:\n%s", output)
	}
	if !strings.Contains(output, "locked: sha1:") {
		t.Fatalf("expected path-specific cache to report its exact lock, got:\n%s", output)
	}
}

func scenarioStatusCommandShowsLocallyExcludedSkills(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "status-excluded.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"ops"})
	writeSkill(t, working, "log-triage", "Log triage", []string{"ops"})
	commitAndPushAll(t, working, "excluded resources")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
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

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

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
		origin,
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

func scenarioStatusCommandShowsMalformedSkillsInErrors(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "status-malformed.git")
	writeSkill(t, working, "valid-skill", "Valid skill", []string{"tested"})
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), `---
name: broken-skill
description: [unterminated
metadata:
  tags:
    - broken
---

# Broken
`)
	commitAndPushAll(t, working, "malformed resource")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("expected status command to succeed: %v\n%s", err, output)
	}

	for _, needle := range []string{
		"ERRORS",
		"broken-skill",
		filepath.Join("skills", "broken-skill", "SKILL.md"),
		"did not find expected ',' or ']'",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}
}

func scenarioStatusDriftSectionInSync(t *testing.T) {
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

func scenarioStatusDriftSectionOutOfSync(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "status-drift.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"ops"})
	commitAndPushAll(t, working, "drift resource")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed synchronized output: %v\n%s", err, output)
	}
	if err := os.RemoveAll(filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle")); err != nil {
		t.Fatalf("remove materialized skill to create drift: %v", err)
	}

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

func scenarioStatusCommandBlockedRegistryReturnsError(t *testing.T) {
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

func scenarioStatusReportsManagedInstallation(t *testing.T) {
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
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "registries: []\n")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("status")
	if err == nil {
		t.Fatalf("expected status to reject an untrusted organization authority:\n%s", output)
	}

	for _, needle := range []string{
		"organization.trust.config.writable",
		"is writable by group or other (mode",
		"wrong owner",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected status output to contain %q, got:\n%s", needle, output)
		}
	}
}

func scenarioBuildHarnessTargetRowsPresentsCodexPartitionsAndLegacyRoots(t *testing.T) {
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

func scenarioBuildHarnessTargetRowsRejectsRelativeCodexHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "relative/codex")
	if _, err := buildHarnessTargetRows(t.TempDir(), []string{"codex"}); err == nil || !strings.Contains(err.Error(), "CODEX_HOME must be absolute") {
		t.Fatalf("buildHarnessTargetRows error = %v", err)
	}
}

func scenarioTargetDisplayNameLabelsOnlyCodex(t *testing.T) {
	for input, want := range map[string]string{"codex": "Codex", "factory": "factory", "unknown": "unknown"} {
		if got := targetDisplayName(input); got != want {
			t.Errorf("targetDisplayName(%q) = %q, want %q", input, got, want)
		}
	}
}
