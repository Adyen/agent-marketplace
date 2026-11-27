package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"gopkg.in/yaml.v3"
)

type lifecycleLock struct {
	APIVersion string `yaml:"apiVersion"`
	Scope      string `yaml:"scope"`
	Registries []struct {
		URL    string `yaml:"url"`
		Ref    string `yaml:"ref"`
		Commit string `yaml:"commit"`
	} `yaml:"registries"`
}

func TestSyncLockRetainsCommitUntilUpdate(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "locked-sync.git")
	writeSkill(t, working, "review", "Review v1", nil)
	commitAndPushAll(t, working, "review v1")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  origin: {git: file://`+origin+`, ref: main}
resources:
  origin: [skill/review]
`)
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	firstBytes, first := readLifecycleLock(t, lockPath)
	if first.APIVersion != "agent-marketplace-lock/v2" || first.Scope != "repo" {
		t.Fatalf("unexpected lock header: %+v\n%s", first, firstBytes)
	}
	if len(first.Registries) != 1 || !strings.HasPrefix(first.Registries[0].Commit, "sha1:") {
		t.Fatalf("expected one typed Git resolution, got %+v", first.Registries)
	}
	assertMaterializedSkillContains(t, repoRoot, "review", "Review v1")

	writeSkill(t, working, "review", "Review v2", nil)
	commitAndPushAll(t, working, "review v2")

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("retaining sync: %v\n%s", err, output)
	}
	secondBytes, second := readLifecycleLock(t, lockPath)
	if string(secondBytes) != string(firstBytes) {
		t.Fatalf("ordinary sync rewrote a compatible lock\nbefore:\n%s\nafter:\n%s", firstBytes, secondBytes)
	}
	if second.Registries[0].Commit != first.Registries[0].Commit {
		t.Fatalf("ordinary sync advanced %q to %q", first.Registries[0].Commit, second.Registries[0].Commit)
	}
	assertMaterializedSkillContains(t, repoRoot, "review", "Review v1")

	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("update: %v\n%s", err, output)
	}
	_, updated := readLifecycleLock(t, lockPath)
	if updated.Registries[0].Commit == first.Registries[0].Commit {
		t.Fatalf("update retained old commit %q", first.Registries[0].Commit)
	}
	assertMaterializedSkillContains(t, repoRoot, "review", "Review v2")
}

// spec: acquisition-lock-codec/targeted-upgrade-retains-repeated-and-ordinary-sources
func TestSyncRepeatedGitSuffixLockIdentitySurvivesUpgrade(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	repeatedOrigin, repeatedWorking := createBareRepoWithWorkingClone(t, "tools.git.git")
	shortened := strings.TrimSuffix(repeatedOrigin, ".git")
	if _, err := os.Stat(shortened); err == nil {
		t.Fatalf("shortened repository path unexpectedly exists beside %q", repeatedOrigin)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat shortened repository path %q: %v", shortened, err)
	}
	ordinaryOrigin, ordinaryWorking := createBareRepoWithWorkingClone(t, "ordinary.git")
	writeSkill(t, repeatedWorking, "review", "Repeated v1", nil)
	writeSkill(t, ordinaryWorking, "ordinary", "Ordinary v1", nil)
	commitAndPushAll(t, repeatedWorking, "repeated v1")
	commitAndPushAll(t, ordinaryWorking, "ordinary v1")

	repeatedURL := "file://" + filepath.ToSlash(repeatedOrigin)
	ordinaryURL := "file://" + filepath.ToSlash(ordinaryOrigin)
	repeatedIdentity, err := lockfile.CanonicalGitConfigURL(repeatedURL)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryIdentity, err := lockfile.CanonicalGitConfigURL(ordinaryURL)
	if err != nil {
		t.Fatal(err)
	}
	if repeatedIdentity == ordinaryIdentity {
		t.Fatalf("repeated and ordinary source identities collapsed to %q", repeatedIdentity)
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  repeated: {git: `+repeatedURL+`, ref: main}
  ordinary: {git: `+ordinaryURL+`, ref: main}
resources:
  repeated: [skill/review]
  ordinary: [skill/ordinary]
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}

	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	initialBytes, initialWire := readLifecycleLock(t, lockPath)
	if initialWire.APIVersion != "agent-marketplace-lock/v2" || len(initialWire.Registries) != 2 {
		t.Fatalf("unexpected initial lock: %+v\n%s", initialWire, initialBytes)
	}
	if !strings.Contains(string(initialBytes), repeatedURL) {
		t.Fatalf("initial lock lost repeated-suffix wire URL %q:\n%s", repeatedURL, initialBytes)
	}
	initial := readLockDocument(t, lockPath)
	findRegistry := func(doc lockfile.Document, url string) (lockfile.GitRegistry, bool) {
		t.Helper()
		for _, registry := range doc.Registries {
			if registry.URL == url {
				return registry, true
			}
		}
		return lockfile.GitRegistry{}, false
	}
	repeated, ok := findRegistry(initial, repeatedIdentity)
	if !ok || len(repeated.Roots) == 0 {
		t.Fatalf("initial lock lacks repeated-suffix identity and manifest %q: %+v", repeatedIdentity, initial.Registries)
	}
	ordinary, ok := findRegistry(initial, ordinaryIdentity)
	if !ok || len(ordinary.Roots) == 0 {
		t.Fatalf("initial lock lacks ordinary identity and manifest %q: %+v", ordinaryIdentity, initial.Registries)
	}
	initialRepeatedCommit := repeated.Commit
	initialOrdinaryCommit := ordinary.Commit

	assertLockReadsStable := func(stage string, before []byte) {
		t.Helper()
		for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
			if output, err := executeCommand(args...); err != nil {
				t.Fatalf("%s %v: %v\n%s", stage, args, err, output)
			}
			after, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("%s %v rewrote the lock:\nbefore:\n%s\nafter:\n%s", stage, args, before, after)
			}
		}
	}
	assertLockReadsStable("initial", initialBytes)
	assertMaterializedSkillContains(t, repoRoot, "review", "Repeated v1")
	assertMaterializedSkillContains(t, repoRoot, "ordinary", "Ordinary v1")

	writeSkill(t, repeatedWorking, "review", "Repeated v2", nil)
	commitAndPushAll(t, repeatedWorking, "repeated v2")
	if output, err := executeCommand("lock", "--upgrade", "--scope", "repo", "--registry", repeatedURL); err != nil {
		t.Fatalf("targeted repeated-suffix lock upgrade: %v\n%s", err, output)
	}

	upgradedBytes, upgradedWire := readLifecycleLock(t, lockPath)
	if upgradedWire.APIVersion != "agent-marketplace-lock/v2" || len(upgradedWire.Registries) != 2 {
		t.Fatalf("unexpected upgraded lock: %+v\n%s", upgradedWire, upgradedBytes)
	}
	if !strings.Contains(string(upgradedBytes), repeatedURL) {
		t.Fatalf("upgraded lock lost repeated-suffix wire URL %q:\n%s", repeatedURL, upgradedBytes)
	}
	upgraded := readLockDocument(t, lockPath)
	repeated, ok = findRegistry(upgraded, repeatedIdentity)
	if !ok || repeated.Commit == initialRepeatedCommit || len(repeated.Roots) == 0 {
		t.Fatalf("targeted upgrade did not advance repeated source %q: %+v", repeatedIdentity, upgraded.Registries)
	}
	ordinary, ok = findRegistry(upgraded, ordinaryIdentity)
	if !ok || ordinary.Commit != initialOrdinaryCommit {
		t.Fatalf("targeted upgrade changed ordinary source %q: %+v", ordinaryIdentity, upgraded.Registries)
	}

	assertLockReadsStable("upgraded", upgradedBytes)
	assertMaterializedSkillContains(t, repoRoot, "review", "Repeated v2")
	assertMaterializedSkillContains(t, repoRoot, "ordinary", "Ordinary v1")
}

// spec: acquisition-lock-codec/missing-configured-fetch-mapping-retains-the-repeated-suffix-origin
func TestNewFetchURLResolverPreservesRepeatedSuffixFallback(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const raw = "https://git.example/team/tools.git.git"
	doc, err := lockfile.Decode([]byte("apiVersion: " + lockfile.APIVersionV2 + "\nscope: repo\nregistries:\n  - url: " + raw + "\n    ref: main\n    commit: sha1:0123456789abcdef0123456789abcdef01234567\n"))
	if err != nil {
		t.Fatal(err)
	}
	resolveFetchURL, err := newFetchURLResolver(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveFetchURL(doc.Registries[0]); got != raw {
		t.Fatalf("missing-mapping fetch URL = %q, want preserved origin %q", got, raw)
	}
}

func TestSyncRootRemovalProjectsOldCommit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "root-removal.git")
	writeSkill(t, filepath.Join(working, "kept"), "keep", "Keep v1", nil)
	writeSkill(t, filepath.Join(working, "removed"), "drop", "Drop v1", nil)
	commitAndPushAll(t, working, "v1")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  kept: {git: file://`+origin+`, ref: main, path: kept}
  removed: {git: file://`+origin+`, ref: main, path: removed}
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	before := readLockDocument(t, lockPath)
	writeSkill(t, filepath.Join(working, "kept"), "keep", "Keep v2", nil)
	commitAndPushAll(t, working, "move branch")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  kept: {git: file://`+origin+`, ref: main, path: kept}
`)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("projecting sync: %v\n%s", err, output)
	}
	after := readLockDocument(t, lockPath)
	if len(after.Registries) != 1 || after.Registries[0].Commit != before.Registries[0].Commit || len(after.Registries[0].Roots) != 1 || after.Registries[0].Roots[0].Path != "kept" {
		t.Fatalf("root removal did not project the old commit: %+v", after.Registries)
	}
	if after.Registries[0].Roots[0].RevisionDigest != before.Registries[0].Roots[0].RevisionDigest {
		t.Fatal("remaining root was rebuilt instead of projected")
	}
}

func TestSyncEquivalentAutoAndExplicitRootsStayFresh(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "equivalent-roots.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")
	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
apiVersion: agent-marketplace/v4
sources:
  origin: {git: file://`+origin+`, ref: main}
`)
	withWorkingDir(t, repoRoot)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("initial lock: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	initial := readLockDocument(t, lockPath)
	if len(initial.Registries) != 1 || len(initial.Registries[0].Roots) != 1 {
		t.Fatalf("unexpected initial roots: %+v", initial.Registries)
	}
	auto := "    git: file://" + origin + "\n    ref: main\n"
	explicit := auto + "    source_harness: " + initial.Registries[0].Roots[0].SourceHarness + "\n"
	for _, tc := range []struct{ name, entries string }{
		{"auto-first", "  first:\n" + auto + "  second:\n" + explicit},
		{"explicit-first", "  first:\n" + explicit + "  second:\n" + auto},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nsources:\n"+tc.entries)
			configBefore, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"lock", "--check", "--scope", "repo"}, {"list"}, {"status"}} {
				t.Run(args[0], func(t *testing.T) {
					output, err := executeCommand(args...)
					if err != nil {
						t.Errorf("equivalent roots remained stale: %v\n%s", err, output)
					}
					if args[0] == "list" && !strings.Contains(output, "review") {
						t.Errorf("exact inventory missing review: %s", output)
					}
					if args[0] == "status" && (!strings.Contains(output, initial.Registries[0].Commit.String()) || !strings.Contains(output, "available")) {
						t.Errorf("exact status missing available locked commit: %s", output)
					}
					for path, want := range map[string][]byte{configPath: configBefore, lockPath: before} {
						after, err := os.ReadFile(path)
						if err != nil || string(after) != string(want) {
							t.Errorf("inspection changed %s: %v", path, err)
						}
					}
				})
			}
		})
	}
}

func TestSyncLockedRefusesMissingLockWithoutMaterializing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "missing-lock.git")
	writeSkill(t, working, "review", "Review", nil)
	commitAndPushAll(t, working, "review")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v4
sources:
  origin: {git: file://`+origin+`, ref: main}
resources:
  origin: [skill/review]
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync", "--locked")
	if err == nil {
		t.Fatalf("sync --locked unexpectedly succeeded:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("sync --locked created a lock: %v", statErr)
	}
	if _, statErr := os.Lstat(filepath.Join(repoRoot, ".agents", "skills", "review")); !os.IsNotExist(statErr) {
		t.Fatalf("sync --locked materialized output: %v", statErr)
	}
}

func readLifecycleLock(t *testing.T, path string) ([]byte, lifecycleLock) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock %q: %v", path, err)
	}
	var lock lifecycleLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		t.Fatalf("parse lock %q: %v\n%s", path, err, data)
	}
	return data, lock
}

func assertMaterializedSkillContains(t *testing.T, repoRoot, name, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "skills", name, "SKILL.md"))
	if err != nil {
		t.Fatalf("read materialized skill %q: %v", name, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("materialized skill %q does not contain %q:\n%s", name, want, data)
	}
}
