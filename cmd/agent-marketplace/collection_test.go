package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// createV4GitRegistry gives the local bare fixture a hostless file:// origin,
// keeping Git acquisition distinct from a live v4 directory source.
func createV4GitRegistry(t *testing.T, bareName string) (string, string) {
	t.Helper()
	origin, working := createBareRepoWithWorkingClone(t, bareName)
	return "file://" + filepath.ToSlash(origin), working
}

// writeCollection creates a COLLECTION.yaml file at
// <repoDir>/collections/<dirName>/COLLECTION.yaml.
func writeCollection(t *testing.T, repoDir, dirName, content string) {
	t.Helper()
	colDir := filepath.Join(repoDir, "collections", dirName)
	writeFile(t, filepath.Join(colDir, "COLLECTION.yaml"), strings.TrimSpace(content)+"\n")
}

// TestCollectionInstallAndSync verifies the Phase 5 acceptance criterion:
// install a local collection, opt out of one member via exclude, and confirm
// the skills directory reflects the right state.
// spec: resource-installation/singleton-and-no-name-install-behavior-is-retained
func TestCollectionInstallAndSync(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create a bare+working registry with two skills and a collection.
	origin, working := createV4GitRegistry(t, "platform-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", []string{"k8s"})
	writeSkill(t, working, "log-triage", "Log triage", []string{"observability"})
	writeCollection(t, working, "platform-onboarding", `
apiVersion: agent-marketplace/v1
kind: Collection
name: platform-onboarding
version: 1.0.0
description: Day-one platform skills
skills:
  - name: vm-lifecycle
  - name: k8s-basics
  - name: log-triage
`)
	commitAndPushAll(t, working, "Add skills and collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)
	requireExactLocks(t)

	// list --kind collection should find the collection.
	output, err := executeCommand("list", "--kind", "collection")
	if err != nil {
		t.Fatalf("list --kind collection failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "platform-onboarding") {
		t.Fatalf("expected platform-onboarding in collections list, got:\n%s", output)
	}

	// install the collection.
	output, err = executeCommand("install", "--scope", "repo", "platform-onboarding")
	if err != nil {
		t.Fatalf("install platform-onboarding failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "platform-onboarding") {
		t.Fatalf("expected install output to mention platform-onboarding, got:\n%s", output)
	}

	// Verify collection ref was written to repo config.
	configBytes, err := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(configBytes), "platform-onboarding") {
		t.Fatalf("expected collection ref in config, got:\n%s", configBytes)
	}

	// Verify all three skills are symlinked.
	for _, skill := range []string{"vm-lifecycle", "k8s-basics", "log-triage"} {
		assertFactorySkillExists(t, repoRoot, skill)
	}
}

// TestCollectionStatusViaColumn verifies that skills resolved via a collection
// show a non-empty VIA column in status output.
func TestCollectionStatusViaColumn(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "bundle-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", []string{"k8s"})
	writeCollection(t, working, "infra-bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: infra-bundle
version: 2.0.0
skills:
  - name: vm-lifecycle
  - name: k8s-basics
`)
	commitAndPushAll(t, working, "Add bundle collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    include: [collection/infra-bundle]
`)

	withWorkingDir(t, repoRoot)

	// Sync first so status has something to show.
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync failed: %v\n%s", err, output)
	}
	for _, skill := range []string{"vm-lifecycle", "k8s-basics"} {
		assertFactorySkillExists(t, repoRoot, skill)
	}

	output, err := executeCommand("status")
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, output)
	}

	// VIA column header should be present.
	if !strings.Contains(output, "VIA") {
		t.Fatalf("expected VIA column in status output, got:\n%s", output)
	}

	// Skills resolved via the collection should show the collection label.
	if !strings.Contains(output, "collection:infra-bundle@2.0.0") {
		t.Fatalf("expected collection via label in status, got:\n%s", output)
	}
}

// TestCollectionRemove verifies that remove <collection> removes the
// collection ref from config and de-syncs the member skills.
func TestCollectionRemove(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "remove-test-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
	writeCollection(t, working, "small-bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: small-bundle
version: 1.0.0
skills:
  - name: vm-lifecycle
`)
	commitAndPushAll(t, working, "Add bundle")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    include: [collection/small-bundle]
`)

	withWorkingDir(t, repoRoot)

	// Sync to install and require the real materialization path to succeed.
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed: %v\n%s", err, output)
	}
	skillPath := filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("sync did not materialize skill (may be expected in CI without git): %v", err)
	}

	// Remove the collection.
	output, err := executeCommand("remove", "small-bundle")
	if err != nil {
		t.Fatalf("remove small-bundle failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "small-bundle") {
		t.Fatalf("expected remove output to mention small-bundle, got:\n%s", output)
	}

	// The collection include is replaced by a source-scoped exclusion.
	cfg, err := config.LoadV4ConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if err != nil {
		t.Fatalf("load v4 config: %v", err)
	}
	group := cfg.Groups["registry"]
	if len(group.Include) != 0 || !slices.Equal(group.Exclude, []string{"collection/small-bundle"}) {
		t.Fatalf("registry group = include %#v exclude %#v, want exclude [collection/small-bundle] only", group.Include, group.Exclude)
	}

	// Symlink should be removed after sync.
	assertFactorySkillAbsent(t, repoRoot, "vm-lifecycle")
}

// TestCollectionMemberExclude verifies that removing a single skill that is
// provided by a collection writes an exclude rather than removing the
// collection reference, and that the other member skills remain synced.
func TestCollectionMemberExclude(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "member-exclude-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
	writeSkill(t, working, "k8s-basics", "Kubernetes basics", []string{"k8s"})
	writeCollection(t, working, "platform-bundle", `
name: platform-bundle
skills:
  - name: vm-lifecycle
  - name: k8s-basics
`)
	commitAndPushAll(t, working, "Add skills and collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    include: [collection/platform-bundle]
`)

	withWorkingDir(t, repoRoot)

	// Install + sync.
	if _, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync failed (may lack git in CI): %v", err)
	}
	for _, s := range []string{"vm-lifecycle", "k8s-basics"} {
		if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", s, "SKILL.md")); err != nil {
			t.Fatalf("factory skill for %q missing after sync (CI limitation): %v", s, err)
		}
	}

	// Remove just one member skill — should NOT remove the collection.
	output, err := executeCommand("remove", "k8s-basics")
	if err != nil {
		t.Fatalf("remove k8s-basics failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Excluded") {
		t.Fatalf("expected 'Excluded' in output (not a plain remove), got:\n%s", output)
	}
	if !strings.Contains(output, "platform-bundle") {
		t.Fatalf("expected output to name the source collection, got:\n%s", output)
	}

	// Collection ref must still be in the repo config.
	configBytes, err := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(configBytes), "platform-bundle") {
		t.Fatalf("collection ref should survive member exclude, config:\n%s", configBytes)
	}

	// An exclude entry must have been written somewhere for k8s-basics.
	localCfgPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	repoCfgPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	localBytes, _ := os.ReadFile(localCfgPath)
	repoBytes, _ := os.ReadFile(repoCfgPath)
	combined := string(localBytes) + string(repoBytes)
	if !strings.Contains(combined, "k8s-basics") {
		t.Fatalf("expected k8s-basics exclude in local or repo config, got:\nlocal: %s\nrepo: %s", localBytes, repoBytes)
	}

	// k8s-basics symlink gone; vm-lifecycle still present.
	assertFactorySkillAbsent(t, repoRoot, "k8s-basics")
	assertFactorySkillExists(t, repoRoot, "vm-lifecycle")
}

// TestCollectionIncludesExpansion verifies that a collection that includes
// another collection transitively installs all member skills.
func TestCollectionIncludesExpansion(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createV4GitRegistry(t, "includes-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"infra"})
	writeSkill(t, working, "security-baseline", "Security baseline", []string{"security"})
	writeCollection(t, working, "security", `
apiVersion: agent-marketplace/v1
kind: Collection
name: security
version: 1.0.0
skills:
  - name: security-baseline
`)
	writeCollection(t, working, "full-onboarding", `
apiVersion: agent-marketplace/v1
kind: Collection
name: full-onboarding
version: 1.0.0
skills:
  - name: vm-lifecycle
includes:
  - name: security
    version: 1.0.0
`)
	commitAndPushAll(t, working, "Add includes collections")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  registry:
    git: `+origin+`
    ref: main
resources:
  registry:
    include: [collection/full-onboarding]
`)

	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync included collections: %v\n%s", err, output)
	}

	for _, skill := range []string{"vm-lifecycle", "security-baseline"} {
		assertFactorySkillExists(t, repoRoot, skill)
	}
}
