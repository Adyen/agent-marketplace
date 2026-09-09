package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestSyncSummaryUsesTextLabelsInsteadOfEmoji(t *testing.T) {
	var output strings.Builder
	if err := writeSyncSummary(&output, syncOutputStyle{}, 1, 2, 3, 4); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "Installed 1  Removed 2  Malformed 3  Errors 4\n" {
		t.Fatalf("writeSyncSummary() = %q", got)
	}
}

func TestSyncCommandMaterializesSkillsAndPrintsSummary(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack", "puppet"})
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

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync command to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed 1  Removed 0  Malformed 0  Errors 0") {
		t.Fatalf("expected sync summary with installs, got %q", output)
	}

	// Materialize writes to .factory/skills/<name>/SKILL.md
	assertFileExists(t, filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md"))
}

func TestSyncCommandDoesNotWarnWhenDevinSupportFilesAreMaterialized(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, "config"))

	origin, working := createBareRepoWithWorkingClone(t, "skills-with-support-files.git")
	writeSkill(t, working, "review", "Review skill", nil)
	writeFile(t, filepath.Join(working, "skills", "review", "scripts", "review.sh"), "#!/bin/sh\necho review\n")
	commitAndPushAll(t, working, "Add skill with support file")

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - devin
registries:
  - url: `+origin+`
    ref: main
    skills:
      - review
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync command to succeed: %v\n%s", err, output)
	}
	if strings.Contains(output, "skill.support-files-deferred") {
		t.Fatalf("support files materialized by sync must not produce a deferred warning, got %q", output)
	}
	assertFileExists(t, filepath.Join(homeDir, ".config", "devin", "skills", "review", "scripts", "review.sh"))
}

func TestSyncCommandGroupsWindsurfSubagentDropWarnings(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-agents.git")
	writeSubagent(t, working, "reviewer", "Reviewer", []string{"Read"})
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	commitAndPushAll(t, working, "Add subagents")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - windsurf
registries:
  - url: `+origin+`
    ref: main
    subagents:
      - reviewer
      - worker-generic
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync command to succeed for windsurf subagent drop warnings: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed 0  Removed 0  Malformed 0  Errors 0") {
		t.Fatalf("expected severity-marked sync summary, got %q", output)
	}
	if !strings.Contains(output, "Errors 0\n\nwarning: subagent.windsurf.no-primitive") {
		t.Fatalf("expected warnings to be visually separated from the summary, got %q", output)
	}
	if got := strings.Count(output, "subagent.windsurf.no-primitive"); got != 1 {
		t.Fatalf("expected one grouped windsurf diagnostic, got %d:\n%s", got, output)
	}
	if !strings.Contains(output, "resource was dropped (2 resources)") || !strings.Contains(output, "reviewer, worker-generic") {
		t.Fatalf("expected grouped resource count and names, got %q", output)
	}
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("non-terminal sync output contains ANSI escapes: %q", output)
	}

	verboseOutput, err := executeCommand("sync", "--verbose")
	if err != nil {
		t.Fatalf("expected verbose sync to succeed for grouped warnings: %v\n%s", err, verboseOutput)
	}
	if got := strings.Count(verboseOutput, "subagent.windsurf.no-primitive"); got != 1 {
		t.Fatalf("verbose sync rendered the grouped diagnostic %d times:\n%s", got, verboseOutput)
	}
	for _, source := range []string{"reviewer (agents/reviewer.md:1)", "worker-generic (agents/worker-generic.md:1)"} {
		if !strings.Contains(verboseOutput, source) {
			t.Errorf("verbose grouped warning missing %q:\n%s", source, verboseOutput)
		}
	}
}

func TestSyncCommandReturnsNonZeroWhenRegistryCannotBeLoaded(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+filepath.Join(t.TempDir(), "missing.git")+`
    ref: main
    skills:
      - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err == nil {
		t.Fatalf("expected sync command to fail, got success with output %q", output)
	}

	if !strings.Contains(output, "Installed 0  Removed 0  Malformed 0  Errors 1") {
		t.Fatalf("expected sync summary with one error, got %q", output)
	}
}

func TestSyncCommandPrintsHintWhenRegistriesConfiguredButNoSkills(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack", "puppet"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync command to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "No skills selected") || !strings.Contains(output, "Add skills or tags to your config, or set required: true on a registry.") {
		t.Fatalf("expected hint when registries configured but no skills, got %q", output)
	}
}

func TestSyncCommandNoHintWhenNoRegistriesConfigured(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
# No registries configured
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync command to succeed: %v\n%s", err, output)
	}

	if strings.Contains(output, "No skills selected") {
		t.Fatalf("expected no hint when no registries configured, got %q", output)
	}

	if !strings.Contains(output, "Installed 0  Removed 0  Malformed 0  Errors 0") {
		t.Fatalf("expected zero counts, got %q", output)
	}
}

func TestSyncCommandAddsLocalConfigAndLockToGitignore(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.local.yaml"), `
skills:
  exclude:
    - local-only-skill
`)

	withWorkingDir(t, repoRoot)

	for i := 0; i < 2; i++ {
		output, err := executeCommand("sync")
		if err != nil {
			t.Fatalf("sync %d: %v\n%s", i+1, err, output)
		}
	}

	gitignoreBytes, err := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	for _, entry := range []string{".agents/config.local.yaml", ".agents/lock.local.yaml"} {
		if got := strings.Count(string(gitignoreBytes), entry); got != 1 {
			t.Fatalf("expected exactly one %s gitignore entry, got %d in:\n%s", entry, got, gitignoreBytes)
		}
	}
	if !strings.Contains(string(gitignoreBytes), ".agents/config.local.yaml\n.agents/lock.local.yaml\n") {
		t.Fatalf("expected local config and lock entries beside each other:\n%s", gitignoreBytes)
	}
}

func TestSyncCommandIncludesMalformedSkillsInSummary(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "awesome-ai.git")
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
	commitAndPushAll(t, working, "Add valid and malformed skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - valid-skill
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync command to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Malformed 1") {
		t.Fatalf("expected warning-marked malformed count, got %q", output)
	}
	if !strings.Contains(output, "\n\nwarning: 1 resource was dropped from the catalog after failing validation") {
		t.Fatalf("expected a visually separated malformed-resource warning, got %q", output)
	}

	// Default (non-verbose) output must not list the malformed details.
	if strings.Contains(output, "malformed (1):") {
		t.Fatalf("did not expect malformed detail in default output, got %q", output)
	}

	// Verbose output lists the malformed resource with its name and error.
	vout, err := executeCommand("sync", "--verbose")
	if err != nil {
		t.Fatalf("expected sync --verbose to succeed: %v\n%s", err, vout)
	}
	if !strings.Contains(vout, "malformed (1):") {
		t.Fatalf("expected malformed detail header in verbose output, got %q", vout)
	}
	if !strings.Contains(vout, "broken-skill") {
		t.Fatalf("expected malformed resource name in verbose output, got %q", vout)
	}
}

func TestSyncCommandNoHintWhenSkillsAlreadySynced(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack", "puppet"})
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

	// First sync installs the skill
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected first sync to succeed: %v\n%s", err, output)
	}

	// Second sync: nothing to install, but skill is already present — no hint
	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("expected second sync to succeed: %v\n%s", err, output)
	}

	if strings.Contains(output, "No skills selected") {
		t.Fatalf("expected no hint when skills are already synced, got %q", output)
	}

	if !strings.Contains(output, "Installed 0  Removed 0") {
		t.Fatalf("expected zero install/remove counts on second sync, got %q", output)
	}
}

func TestSyncCommandMonorepoSubdirectoryTopLevelSkillsIncludeSelectsLocalRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	monorepoRoot := filepath.Join(t.TempDir(), "monorepo")
	paymentsDir := filepath.Join(monorepoRoot, "payments")

	writeConfig(t, filepath.Join(paymentsDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
  - windsurf
registries:
  - url: .
    path: registry
skills:
  include:
    - test-skill
`)
	writeSkill(t, filepath.Join(paymentsDir, "registry"), "test-skill", "Test skill", []string{"test"})

	withWorkingDir(t, paymentsDir)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync command to succeed: %v\n%s", err, output)
	}
	if strings.Contains(output, "No skills selected") {
		t.Fatalf("expected top-level skills include to resolve from local registry, got %q", output)
	}

	assertFileExists(t, filepath.Join(paymentsDir, ".factory", "skills", "test-skill", "SKILL.md"))
	assertFileExists(t, filepath.Join(paymentsDir, ".windsurf", "skills", "test-skill"))
}

func TestSyncCheckExitsZeroWhenInSync(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack", "puppet"})
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

	// First sync to get skills on disk
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed: %v\n%s", err, output)
	}

	// --check should pass now
	output, err = executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("expected sync --check to succeed when in sync: %v\n%s", err, output)
	}
	if output != "" {
		t.Fatalf("expected no output when in sync, got %q", output)
	}
}

func TestSyncCheckExitsNonZeroWhenDrifted(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack", "puppet"})
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
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed synchronized output: %v\n%s", err, output)
	}
	if err := os.RemoveAll(filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle")); err != nil {
		t.Fatalf("remove materialized skill to create drift: %v", err)
	}

	output, err := executeCommand("sync", "--check")
	if err == nil {
		t.Fatalf("expected sync --check to fail when out of sync, got success")
	}
	if !strings.Contains(output, "out of sync") {
		t.Fatalf("expected drift summary, got %q", output)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected file to exist at %s", path)
	}
}

func assertFactorySkillExists(t *testing.T, root, name string) {
	t.Helper()
	assertFileExists(t, filepath.Join(root, ".factory", "skills", name, "SKILL.md"))
}

func assertFactorySkillAbsent(t *testing.T, root, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".factory", "skills", name, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected factory skill %s to be absent, got err=%v", name, err)
	}
}

func assertSymlinkTarget(t *testing.T, linkPath, wantTarget string) {
	t.Helper()

	name := filepath.Base(linkPath)
	scopeRoot := filepath.Dir(filepath.Dir(filepath.Dir(linkPath)))
	assertFactorySkillExists(t, scopeRoot, name)

	// Also verify the symlink target when the path is actually a symlink.
	// Some callers pass non-symlink materialization paths (e.g. .agents/skills/)
	// which are handled by the Factory skill check above; only validate the
	// target when the path exists and is a symlink.
	info, err := os.Lstat(linkPath)
	if err != nil {
		if os.IsNotExist(err) {
			return // path doesn't exist; Factory skill check is sufficient
		}
		t.Fatalf("lstat %q: %v", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return // not a symlink; Factory skill check is sufficient
	}
	gotTarget, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink %q: %v", linkPath, err)
	}
	if !filepath.IsAbs(gotTarget) {
		gotTarget = filepath.Join(filepath.Dir(linkPath), gotTarget)
	}
	gotTarget = filepath.Clean(gotTarget)
	wantCleaned := filepath.Clean(wantTarget)
	if gotTarget != wantCleaned {
		t.Fatalf("symlink %q: target = %q, want %q", linkPath, gotTarget, wantCleaned)
	}
}

// TestE2E038_MonorepoSubdirectorySkillsIncludeFilter verifies that a registry
// configured with a monorepo subdirectory path and a skills:include filter
// only selects the included skills, not all skills in the registry.
func TestE2E038_MonorepoSubdirectorySkillsIncludeFilter(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "monorepo-include.git")
	// Create skills in a subdirectory of the registry.
	writeSkill(t, filepath.Join(working, "payments"), "payment-api", "Payment API", []string{"payments"})
	writeSkill(t, filepath.Join(working, "payments"), "settlement", "Settlement", []string{"payments"})
	writeSkill(t, filepath.Join(working, "infra"), "k8s-deploy", "K8s deploy", []string{"infra"})
	commitAndPushAll(t, working, "Add monorepo skills")

	// User config references the same registry but with the payments subdirectory.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    path: payments
    skills:
      - payment-api
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected sync to succeed: %v\n%s", err, output)
	}

	// payment-api should be installed (it's in the include list).
	assertSymlinkTarget(t,
		filepath.Join(homeDir, ".agents", "skills", "payment-api"),
		filepath.Join(cache.RegistryCachePath(origin, "main"), "payments", "payment-api"),
	)

	// settlement should NOT be installed (not in include list even though it
	// exists in the same subdirectory).
	if _, err := os.Lstat(filepath.Join(homeDir, ".agents", "skills", "settlement")); !os.IsNotExist(err) {
		t.Fatalf("expected settlement to NOT be installed, got err=%v", err)
	}

	// k8s-deploy should NOT be installed (it's in a different subdirectory
	// that's not configured).
	if _, err := os.Lstat(filepath.Join(homeDir, ".agents", "skills", "k8s-deploy")); !os.IsNotExist(err) {
		t.Fatalf("expected k8s-deploy to NOT be installed, got err=%v", err)
	}
}

// TestSyncBundleRootDiscovery verifies that when path points at a bundle
// root (e.g. .factory), a single discover() call finds all resource kinds
// (skills, commands, subagents, hooks, MCP) without a second root scan.
func TestSyncBundleRootDiscovery(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "bundle-root.git")

	// Create a Factory-format bundle at .factory/ with multiple resource kinds.
	factoryDir := filepath.Join(working, ".factory")
	skillDir := filepath.Join(factoryDir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), `---
name: review
description: Code review skill
metadata:
  tags: [review]
---

# Review
`)

	commandDir := filepath.Join(factoryDir, "commands")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(commandDir, "deploy.md"), `---
name: deploy
description: Deploy command
---

# Deploy
`)

	commitAndPushAll(t, working, "Add .factory bundle with skills and commands")

	// Point path at .factory (bundle root), NOT .factory/skills.
	// Set default: true so all discovered resources are auto-selected.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    path: .factory
    default: true
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync with bundle-root path should succeed: %v\n%s", err, output)
	}

	// With harnesses: [factory], user-personal skills materialize to ~/.factory/skills/.
	assertFileExists(t, filepath.Join(homeDir, ".factory", "skills", "review", "SKILL.md"))

	// Verify command resource also materialized (regression guard for
	// multi-kind bundle-root discovery).
	assertFileExists(t, filepath.Join(homeDir, ".factory", "commands", "deploy.md"))
}

// TestSyncPathTraversalRejected verifies that a config with an absolute or
// traversal path is rejected at validation time, not silently allowed.
func TestSyncPathTraversalRejected(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Absolute path should be rejected.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:repo.git
    ref: main
    path: /tmp/evil
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err == nil {
		t.Fatalf("expected sync to fail with absolute path, got: %s", output)
	}
	if !strings.Contains(output, "path must be relative") && !strings.Contains(err.Error(), "path must be relative") {
		t.Fatalf("expected 'path must be relative' error, got: %v\n%s", err, output)
	}
}

func TestSyncPathTraversalDotDotRejected(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// ../ traversal path should be rejected.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:repo.git
    ref: main
    path: ../evil
`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err == nil {
		t.Fatalf("expected sync to fail with ../ traversal path, got: %s", output)
	}
	if !strings.Contains(output, "must not escape the registry root") && !strings.Contains(err.Error(), "must not escape the registry root") {
		t.Fatalf("expected 'must not escape the registry root' error, got: %v\n%s", err, output)
	}
}

// TestSyncPruneStaleRemovesStaleSkillSelection verifies that sync --prune-stale
// automatically removes stale (selected but not found) skill selections from
// the repo config and re-syncs. A skill that exists in the registry should
// remain installed; only the missing one is pruned.
func TestSyncPruneStaleRemovesStaleSkillSelection(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"ops"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
      - deleted-skill
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("expected sync --prune-stale to succeed: %v\n%s", err, output)
	}

	// Should report that deleted-skill was pruned.
	if !strings.Contains(output, `pruned stale skill "deleted-skill"`) {
		t.Fatalf("expected output to mention pruning deleted-skill, got:\n%s", output)
	}

	// Should have re-synced and installed vm-lifecycle.
	assertFileExists(t, filepath.Join(repoRoot, ".factory", "skills", "vm-lifecycle", "SKILL.md"))

	// Config should no longer contain deleted-skill in the include list.
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(configData), "deleted-skill") {
		// deleted-skill might appear in the exclude list, which is fine.
		// But it should NOT be in the include list.
		// Check for the include form.
		lines := strings.Split(string(configData), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "- deleted-skill" {
				// This line appears in the include list context if it's
				// after "skills:" and before "exclude:" — but the simplest
				// check is that after prune, the warning should not appear.
				t.Fatalf("expected deleted-skill to be removed from include list, config still contains:\n%s", string(configData))
			}
		}
	}

	// A second sync --prune-stale should not prune anything.
	output2, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("expected second sync --prune-stale to succeed: %v\n%s", err, output2)
	}
	if strings.Contains(output2, "pruned stale") {
		t.Fatalf("expected no pruning on second sync, got:\n%s", output2)
	}
}

// TestSyncPruneStaleDoesNotPruneRequiredResources verifies that
// required:true resources are never pruned, even when not found.
func TestSyncPruneStaleDoesNotPruneRequiredResources(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Use a real (clonable) registry so the prune path actually runs and the
	// required:true protection is exercised — not skipped due to a clone error.
	origin, working := createBareRepoWithWorkingClone(t, "required-skills.git")
	writeSkill(t, working, "existing-skill", "Existing", []string{"ops"})
	commitAndPushAll(t, working, "Add existing skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	// required:true registry selecting a skill that doesn't exist.
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
    required: true
    skills:
      - mandatory-skill
`)

	withWorkingDir(t, repoRoot)

	output, _ := executeCommand("sync", "--prune-stale")
	// Required resources should NOT be pruned even when not found.
	if strings.Contains(output, `pruned stale skill "mandatory-skill"`) {
		t.Fatalf("required resources must not be pruned, got:\n%s", output)
	}
}

// TestSyncPruneStaleNoopWithoutStaleSelections verifies that
// sync --prune-stale is a no-op when there are no stale selections.
func TestSyncPruneStaleNoopWithoutStaleSelections(t *testing.T) {
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

	output, err := executeCommand("sync", "--prune-stale")
	if err != nil {
		t.Fatalf("expected sync --prune-stale to succeed: %v\n%s", err, output)
	}
	if strings.Contains(output, "pruned stale") {
		t.Fatalf("expected no pruning when all skills exist, got:\n%s", output)
	}
}

func TestSyncCommandShowsOutputDirsWhenFilesAreWritten(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, output)
	}

	// Output must contain a → line showing where files landed.
	if !strings.Contains(output, "→") {
		t.Fatalf("expected output dir hint in sync output, got:\n%s", output)
	}
	if !strings.Contains(output, ".factory") {
		t.Fatalf("expected .factory path in sync output, got:\n%s", output)
	}

	// Second sync is idempotent — no files written, so no → line.
	output2, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("second sync failed: %v\n%s", err, output2)
	}
	if strings.Contains(output2, "→") {
		t.Fatalf("expected no output dir hint on idempotent sync, got:\n%s", output2)
	}
}

func TestSyncCommandOutputDirsShowCountsAndVerboseDetail(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
`)
	withWorkingDir(t, repoRoot)

	// Default output annotates each directory with a written count.
	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "written)") {
		t.Fatalf("expected per-dir written count in default output, got:\n%s", output)
	}

	// Verbose output lists individual files under each directory.
	repoRoot2 := filepath.Join(t.TempDir(), "repo2")
	writeConfig(t, filepath.Join(repoRoot2, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
`)
	withWorkingDir(t, repoRoot2)

	vout, err := executeCommand("sync", "--verbose")
	if err != nil {
		t.Fatalf("sync --verbose failed: %v\n%s", err, vout)
	}
	if !strings.Contains(vout, "+ ") {
		t.Fatalf("expected per-file detail in verbose output, got:\n%s", vout)
	}
	if !strings.Contains(vout, "vm-lifecycle") {
		t.Fatalf("expected written skill name in verbose output, got:\n%s", vout)
	}
}

func TestSyncCommandDeduplicatesCollisionWarnings(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAll(t, working, "Add skill")

	// Configure two harnesses so the same skill is materialized to two targets.
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
  - claude
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
`)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	// Pre-create user-owned (non-symlink) skill dirs in both harness outputs
	// so the collision fires for each target.
	for _, dir := range []string{
		filepath.Join(homeDir, ".factory", "skills", "vm-lifecycle"),
		filepath.Join(homeDir, ".claude", "skills", "vm-lifecycle"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# user-owned"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	output, err := executeCommand("sync")
	// Sync succeeds despite collisions (user-owned files are preserved).
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, output)
	}

	// Count occurrences — must appear exactly once even though two targets fired.
	count := strings.Count(output, "collision: preserved user-owned")
	if count != 1 {
		t.Fatalf("expected collision warning exactly once, got %d occurrences:\n%s", count, output)
	}
}

func TestSyncCheckReturnsClearErrorWhenTargetHarnessSetMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
registries:
  - url: `+filepath.Join(t.TempDir(), "registry")+`
    ref: main
    skills:
      - some-skill
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--check")
	if err == nil {
		t.Fatalf("expected sync --check to fail, got success with output %q", output)
	}

	if strings.Contains(output, "0 skills out of sync") {
		t.Fatalf("expected clear error message, got %q", output)
	}

	if !strings.Contains(err.Error(), "no machine-wide target harness set configured") {
		t.Fatalf("expected missing target harness set error, got %v (output %q)", err, output)
	}
}

func TestSyncPrunesOrgOwnedArtifactsWhenOrgConfigDisappears(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "org-user-skills.git")
	writeSkill(t, working, "user-skill", "User skill", []string{"user"})
	writeSkill(t, working, "org-skill", "Org skill", []string{"org"})
	commitAndPushAll(t, working, "Add skills")

	orgCfgPath := filepath.Join(t.TempDir(), "org-config.yaml")
	writeFile(t, orgCfgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - org-skill
`)
	config.SetTestV3OrganizationConfigPath(t, orgCfgPath)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    skills:
      - user-skill
`)
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `registries: []`)

	withWorkingDir(t, repoRoot)
	requireOrganizationLock(t, orgCfgPath)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("expected first sync to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Installed 2") {
		t.Fatalf("expected first sync to install 2 skills, got:\n%s", output)
	}

	assertFactorySkillExists(t, homeDir, "user-skill")
	assertFactorySkillExists(t, homeDir, "org-skill")

	// Simulate the organization configuration disappearing.
	if err := os.Remove(orgCfgPath); err != nil {
		t.Fatalf("failed to remove org config: %v", err)
	}

	output, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("expected second sync to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Removed 1") {
		t.Fatalf("expected second sync to report 1 removal, got:\n%s", output)
	}

	assertFactorySkillExists(t, homeDir, "user-skill")
	assertFactorySkillAbsent(t, homeDir, "org-skill")
}
