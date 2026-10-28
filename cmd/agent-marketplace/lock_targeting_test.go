package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestLockAndUpdateTargetValidation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "named git target requires kind", args: []string{"lock", "--upgrade", "--registry", "https://example.com/registry.git", "review"}, want: "require --kind"},
		{name: "update rejects organization", args: []string{"update", "--scope", "organization"}, want: "does not support organization"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := executeCommand(tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("executeCommand(%q) error = %v, output = %q; want %q", tt.args, err, output, tt.want)
			}
		})
	}
}

func TestParseUpgradeTargetsAllowsWholeRegistryWithoutKind(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	target, err := parseUpgradeTargets(true, nil, "https://example.com/registry.git", "", "", "")
	if err != nil {
		t.Fatalf("whole-registry target: %v", err)
	}
	if target.Registry == "" || target.Kind != "" || len(target.Names) != 0 {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestLockUpgradeTargetsExactRefAndPath(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "qualified-lock.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "seed")
	repo := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nsources:\n  review:\n    git: file://"+origin+"\n    ref: main\n    path: skills\nresources:\n  review: {include: [skill/review]}\n")
	withWorkingDir(t, repo)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("initial lock failed: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	initial, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, working, "review", "Updated review", nil)
	commitAndPushAll(t, working, "update")
	output, err := executeCommand("lock", "--scope", "repo", "--upgrade", "--registry", origin, "--ref", "main", "--path", "skills")
	if err != nil {
		t.Fatalf("qualified lock failed: %v\n%s", err, output)
	}
	locked, err := os.ReadFile(lockPath)
	if err != nil || string(locked) == string(initial) {
		t.Fatalf("qualified lock did not advance its exact source: %q, %v", locked, err)
	}
	if got, err := os.ReadFile(configPath); err != nil || string(got) != string(before) {
		t.Fatalf("lock rewrote authored config: %q, %v", got, err)
	}
	if output, err := executeCommand("lock", "--upgrade", "--registry", origin, "--ref", "missing", "--path", "skills"); err == nil {
		t.Fatalf("unconfigured ref unexpectedly upgraded: %s", output)
	}
	if got, err := os.ReadFile(lockPath); err != nil || string(got) != string(locked) {
		t.Fatalf("failed qualified lock changed lock evidence: %q, %v", got, err)
	}
}

func TestLockUpgradeGitNameExpandsContainingSource(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "targeted.git")
	writeSkill(t, working, "review", "Review v1", nil)
	commitAndPushAll(t, working, "v1")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
sources:
  review:
    git: file://`+origin+`
    ref: main
resources:
  review:
    include: [skill/review]
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("initial lock: %v\n%s", err, output)
	}
	before := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	writeSkill(t, working, "review", "Review v2", nil)
	commitAndPushAll(t, working, "v2")
	output, err := executeCommand("lock", "--upgrade", "--scope", "repo", "--registry", origin, "--kind", "skill", "review")
	if err != nil {
		t.Fatalf("targeted lock: %v\n%s", err, output)
	}
	if !strings.Contains(output, "git target skill/review expands to source") {
		t.Fatalf("target expansion was not reported:\n%s", output)
	}
	after := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if before.Registries[0].Commit == after.Registries[0].Commit {
		t.Fatalf("targeted source occurrence did not advance")
	}
}

func TestLockUpgradeGitTargetCollapsesRootsIntoOneSourceOccurrence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "targeted-roots.git")
	writeSkill(t, filepath.Join(working, "one"), "review", "One v1", nil)
	writeSkill(t, filepath.Join(working, "two"), "review", "Two v1", nil)
	commitAndPushAll(t, working, "v1")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
sources:
  one:
    git: file://`+origin+`
    ref: main
    path: one
  two:
    git: file://`+origin+`
    ref: main
    path: two
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("initial lock: %v\n%s", err, output)
	}
	before := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	writeSkill(t, filepath.Join(working, "one"), "review", "One v2", nil)
	writeSkill(t, filepath.Join(working, "two"), "review", "Two v2", nil)
	commitAndPushAll(t, working, "v2")
	output, err := executeCommand("lock", "--upgrade", "--scope", "repo", "--registry", origin, "--kind", "skill", "review")
	if err != nil {
		t.Fatalf("targeted multi-root lock: %v\n%s", err, output)
	}
	after := readLockDocument(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if len(after.Registries) != 1 || len(after.Registries[0].Roots) != 2 || after.Registries[0].Commit == before.Registries[0].Commit {
		t.Fatalf("source occurrence was not upgraded as a unit: %+v", after.Registries)
	}
}

func TestUpdateAcceptsDirectGitTargetAndReportsExpansion(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "update-targeted.git")
	writeSkill(t, working, "review", "Review v1", nil)
	commitAndPushAll(t, working, "v1")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `
sources:
  review:
    git: file://`+origin+`
    ref: main
resources:
  review:
    include: [skill/review]
`)
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	before := readLockDocument(t, lockPath).Registries[0].Commit
	writeSkill(t, working, "review", "Review v2", nil)
	commitAndPushAll(t, working, "v2")
	output, err := executeCommand("update", "--scope", "repo", "--registry", origin, "--kind", "skill", "review")
	if err != nil {
		t.Fatalf("targeted update: %v\n%s", err, output)
	}
	if !strings.Contains(output, "git target skill/review expands to source occurrence") {
		t.Fatalf("update did not report Git expansion:\n%s", output)
	}
	if got := readLockDocument(t, lockPath).Registries[0].Commit; got == before {
		t.Fatal("targeted update did not advance the lock")
	}
}

func TestLockUpgradeScopeLeavesOtherLayerAtExactCommit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	userOrigin, userWorking := createBareRepoWithWorkingClone(t, "user-target.git")
	repoOrigin, repoWorking := createBareRepoWithWorkingClone(t, "repo-target.git")
	writeSkill(t, userWorking, "user-skill", "User v1", nil)
	writeSkill(t, repoWorking, "repo-skill", "Repo v1", nil)
	commitAndPushAll(t, userWorking, "user v1")
	commitAndPushAll(t, repoWorking, "repo v1")
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  user:\n    git: file://"+userOrigin+"\n    ref: main\n")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  repo:\n    git: file://"+repoOrigin+"\n    ref: main\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("initial locks: %v\n%s", err, output)
	}
	userLockPath := filepath.Join(home, ".agents", "lock.yaml")
	repoLockPath := filepath.Join(repo, ".agents", "lock.yaml")
	userBefore := readLockDocument(t, userLockPath).Registries[0].Commit
	repoBefore := readLockDocument(t, repoLockPath).Registries[0].Commit
	writeSkill(t, userWorking, "user-skill", "User v2", nil)
	writeSkill(t, repoWorking, "repo-skill", "Repo v2", nil)
	commitAndPushAll(t, userWorking, "user v2")
	commitAndPushAll(t, repoWorking, "repo v2")
	if output, err := executeCommand("lock", "--upgrade", "--scope", "repo"); err != nil {
		t.Fatalf("repo upgrade: %v\n%s", err, output)
	}
	if got := readLockDocument(t, userLockPath).Registries[0].Commit; got != userBefore {
		t.Fatalf("user lock advanced during repo-scoped upgrade: %s -> %s", userBefore, got)
	}
	if got := readLockDocument(t, repoLockPath).Registries[0].Commit; got == repoBefore {
		t.Fatalf("repo lock did not advance")
	}
	if output, err := executeCommand("lock", "--upgrade"); err != nil {
		t.Fatalf("no-target all upgrade: %v\n%s", err, output)
	}
	if got := readLockDocument(t, userLockPath).Registries[0].Commit; got == userBefore {
		t.Fatalf("no-target upgrade did not advance the user layer")
	}
}

func TestLockUpgradeScopeLeavesStaleUnselectedLayerUntouched(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	userOrigin, userWorking := createBareRepoWithWorkingClone(t, "user-stale.git")
	repoOrigin, repoWorking := createBareRepoWithWorkingClone(t, "repo-selected.git")
	writeSkill(t, userWorking, "user-skill", "User", nil)
	writeSkill(t, repoWorking, "repo-skill", "Repo v1", nil)
	commitAndPushAll(t, userWorking, "user")
	commitAndPushAll(t, repoWorking, "repo v1")
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeConfig(t, userConfig, "apiVersion: agent-marketplace/v4\nsources:\n  user:\n    git: file://"+userOrigin+"\n    ref: main\n")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nsources:\n  repo:\n    git: file://"+repoOrigin+"\n    ref: main\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("initial lock: %v\n%s", err, output)
	}
	userLockPath := filepath.Join(home, ".agents", "lock.yaml")
	before, err := os.ReadFile(userLockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, userConfig, "apiVersion: agent-marketplace/v4\nsources:\n  user:\n    git: file://"+userOrigin+"\n    ref: main\n    path: missing-root\n")
	writeSkill(t, repoWorking, "repo-skill", "Repo v2", nil)
	commitAndPushAll(t, repoWorking, "repo v2")
	if output, err := executeCommand("lock", "--upgrade", "--scope", "repo"); err != nil {
		t.Fatalf("scoped upgrade reconciled stale user layer: %v\n%s", err, output)
	}
	after, err := os.ReadFile(userLockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("unselected stale lock was mutated\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func readLockDocument(t *testing.T, path string) lockfile.Document {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := lockfile.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
