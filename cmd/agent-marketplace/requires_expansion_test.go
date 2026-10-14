package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func writeRequiringSkill(t *testing.T, repoDir, name string, requires ...string) {
	t.Helper()
	skillDir := filepath.Join(repoDir, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", skillDir, err)
	}
	content := strings.Builder{}
	content.WriteString("---\nname: " + name + "\ndescription: Requiring skill\nmetadata:\n  requires:\n")
	for _, req := range requires {
		content.WriteString("    - " + req + "\n")
	}
	content.WriteString("---\n\n# " + name + "\n")
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), content.String())
}

func requiresRegistry(t *testing.T) string {
	t.Helper()
	origin, working := createBareRepoWithWorkingClone(t, "requires-registry.git")
	writeRequiringSkill(t, working, "parent", "skill:child")
	writeSkill(t, working, "child", "Child skill", nil)
	commitAndPushAll(t, working, "setup")
	return origin
}

func requiresV4RepoConfig(t *testing.T, repoRoot, origin string, expand *bool, include string) string {
	t.Helper()
	expandLine := ""
	if expand != nil {
		v := "false"
		if *expand {
			v = "true"
		}
		expandLine = "requires: {expand: " + v + "}\n"
	}
	path := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, path, "apiVersion: agent-marketplace/v4\n"+expandLine+`
sources:
  reg: {git: file://`+origin+`, ref: main}
resources:
  reg: `+include+`
`)
	return path
}

func repoEntrySelector(t *testing.T) (include, exclude []string) {
	t.Helper()
	cfg, err := config.LoadRepoConfig()
	if err != nil || cfg == nil {
		t.Fatalf("load repo config: cfg=%v err=%v", cfg, err)
		return
	}
	for _, entry := range cfg.Registries {
		return entry.Skills.Include, entry.Skills.Exclude
	}
	t.Fatalf("no registry entry in repo config")
	return nil, nil
}

// spec: resource-removal/enabled-removal-excludes-an-optional-prerequisite
func TestEnabledRemoveExcludesStillRequiredOptionalPrerequisite(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	origin := requiresRegistry(t)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	expand := true
	configPath := requiresV4RepoConfig(t, repoRoot, origin, &expand, "{include: [skill/parent, skill/child]}")
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync failed: %v\n%s", err, out)
	}

	out, err := executeCommand("remove", "child")
	if err != nil {
		t.Fatalf("remove failed: %v\n%s", err, out)
	}

	if !strings.Contains(out, "requires") || !strings.Contains(out, "child") {
		t.Fatalf("expected unmet prerequisite warning for child, got:\n%s", out)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "skill/parent") {
		t.Fatalf("requiring skill lost its selection:\n%s", text)
	}
	include, exclude := repoEntrySelector(t)
	for _, n := range include {
		if n == "child" {
			t.Fatalf("child still included after removal: include=%v", include)
		}
	}
	found := false
	for _, n := range exclude {
		if n == "child" {
			found = true
		}
	}
	if !found {
		t.Fatalf("enabled removal must exclude the still-required optional resource: exclude=%v\nconfig:\n%s", exclude, text)
	}
}

// spec: resource-removal/advisory-removal-does-not-add-a-requirement-exclusion
func TestAdvisoryRemoveDoesNotAddRequirementExclusion(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	origin := requiresRegistry(t)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := requiresV4RepoConfig(t, repoRoot, origin, nil, "{include: [skill/parent, skill/child]}")
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync failed: %v\n%s", err, out)
	}
	if out, err := executeCommand("remove", "child"); err != nil {
		t.Fatalf("remove failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "skill/parent") {
		t.Fatalf("requiring skill lost its selection:\n%s", text)
	}
	_, exclude := repoEntrySelector(t)
	for _, n := range exclude {
		if n == "child" {
			t.Fatalf("advisory removal wrote a requirement-specific exclusion for child:\n%s", text)
		}
	}
}

// spec: resource-removal/requirement-removal-preserves-required-policy-and-scope-authorization
func TestRequirementRemovalPreservesRequiredPolicy(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	origin := requiresRegistry(t)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	expand := true
	configPath := requiresV4RepoConfig(t, repoRoot, origin, &expand, "{include: [skill/parent, skill/child], required: true}")
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync failed: %v\n%s", err, out)
	}

	out, err := executeCommand("remove", "child")
	if err == nil {
		t.Fatalf("required resource removal must be refused\n%s", out)
	}
	if !strings.Contains(err.Error()+out, "required") {
		t.Fatalf("expected required-policy refusal, got: %v\n%s", err, out)
	}

	data, rerr := os.ReadFile(configPath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "skill/child") {
		t.Fatalf("required resource selection was mutated by the refused removal:\n%s", data)
	}
}

func requiresPreviewFixture(t *testing.T) (repoRoot, lockPath, childOutput string, lockBytes []byte) {
	t.Helper()
	origin := requiresRegistry(t)
	repoRoot = filepath.Join(t.TempDir(), "repo")
	expand := true
	requiresV4RepoConfig(t, repoRoot, origin, &expand, "{include: [skill/parent]}")
	withWorkingDir(t, repoRoot)

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("enabled seed sync failed: %v\n%s", err, out)
	}
	childOutput = filepath.Join(repoRoot, ".factory", "skills", "child", "SKILL.md")
	if _, err := os.Stat(childOutput); err != nil {
		t.Fatalf("implied child output missing after enabled sync: %v", err)
	}

	expand = false
	requiresV4RepoConfig(t, repoRoot, origin, &expand, "{include: [skill/parent]}")
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("advisory seed sync failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(childOutput); err != nil {
		t.Fatalf("advisory sync pruned the implied-preview output: %v", err)
	}

	lockPath = filepath.Join(repoRoot, ".agents", "lock.yaml")
	var err error
	lockBytes, err = os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read acquisition lock: %v", err)
	}
	return repoRoot, lockPath, childOutput, lockBytes
}

func requireIdenticalBytes(t *testing.T, path string, want []byte, what string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", what, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s changed under preview tolerance", what)
	}
}

// spec: materialization/plain-sync-preserves-healthy-acquisition-lock-bytes
func TestPlainSyncPreservesLockBytesWithPreviewOutputs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	_, lockPath, childOutput, lockBytes := requiresPreviewFixture(t)
	childBytes, err := os.ReadFile(childOutput)
	if err != nil {
		t.Fatal(err)
	}

	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("plain sync failed: %v\n%s", err, out)
	}
	requireIdenticalBytes(t, lockPath, lockBytes, "acquisition lock")
	requireIdenticalBytes(t, childOutput, childBytes, "preview output")
}

// spec: materialization/locked-sync-tolerates-existing-preview-outputs
func TestLockedSyncToleratesExistingPreviewOutputs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	_, lockPath, childOutput, lockBytes := requiresPreviewFixture(t)
	childBytes, err := os.ReadFile(childOutput)
	if err != nil {
		t.Fatal(err)
	}

	if out, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked failed with existing preview outputs: %v\n%s", err, out)
	}
	requireIdenticalBytes(t, lockPath, lockBytes, "acquisition lock")
	requireIdenticalBytes(t, childOutput, childBytes, "preview output")
}

// spec: materialization/check-only-sync-tolerates-existing-preview-outputs
func TestCheckSyncToleratesExistingPreviewOutputs(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	t.Setenv("HOME", t.TempDir())

	_, lockPath, childOutput, lockBytes := requiresPreviewFixture(t)
	childBytes, err := os.ReadFile(childOutput)
	if err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("sync --check reported drift solely from preview outputs: %v\n%s", err, out)
	}
	requireIdenticalBytes(t, lockPath, lockBytes, "acquisition lock")
	requireIdenticalBytes(t, childOutput, childBytes, "preview output")
}
