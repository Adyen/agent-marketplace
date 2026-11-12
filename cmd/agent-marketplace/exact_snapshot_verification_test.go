package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func exactSnapshotDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(cache.CacheDir(), "exact-git", "snapshots", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("expected exact snapshots in the cache: %v, %v", dirs, err)
	}
	return dirs
}

func tamperExactSnapshot(t *testing.T) string {
	t.Helper()
	var tampered string
	for _, dir := range exactSnapshotDirs(t) {
		matches, err := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md"))
		if err != nil || len(matches) == 0 {
			t.Fatalf("snapshot %s has no tracked skill files: %v, %v", dir, matches, err)
		}
		for _, path := range matches {
			if err := os.WriteFile(path, []byte("tampered between commands\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		tampered = dir
	}
	return tampered
}

func setupPathScopedExactRegistry(t *testing.T) (repo, origin, working, lockPath string) {
	t.Helper()
	repo, origin, working, lockPath = setupReadOnlyExactRegistry(t)
	writeCommand(t, working, "outside", "Outside the configured root", "true")
	writeFile(t, filepath.Join(working, "outside", "README.md"), "not consumed\n")
	commitAndPushAll(t, working, "Add files outside the configured root")

	configPath := filepath.Join(repo, ".agents", "config.yaml")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	const ref = "    ref: main\n"
	if !strings.Contains(string(raw), ref) {
		t.Fatalf("registry config has no expected ref field:\n%s", raw)
	}
	writeConfig(t, configPath, strings.Replace(string(raw), ref, ref+"    path: skills\n", 1))
	if output, err := executeCommand("lock", "--scope", "repo", "--upgrade"); err != nil {
		t.Fatalf("create path-scoped exact lock: %v\n%s", err, output)
	}
	clearExactSnapshots(t)
	return repo, origin, working, lockPath
}

func clearExactSnapshots(t *testing.T) {
	t.Helper()
	snapshotsDir := filepath.Join(cache.CacheDir(), "exact-git", "snapshots")
	entries, err := os.ReadDir(snapshotsDir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(snapshotsDir, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

func exactRootSnapshot(t *testing.T, included, excluded []string) string {
	t.Helper()
	var found []string
	for _, dir := range exactSnapshotDirs(t) {
		matches := true
		for _, rel := range included {
			if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		for _, rel := range excluded {
			if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
				matches = false
				break
			}
		}
		if matches {
			found = append(found, dir)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d snapshots containing %v and excluding %v, want exactly one", len(found), included, excluded)
	}
	return found[0]
}

func exactSnapshotFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(candidate string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, candidate)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

// spec: exact-snapshot-verification/snapshot-for-a-configured-path-contains-only-that-path
func TestSyncLockedUsesOnlyConfiguredPathSnapshot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupPathScopedExactRegistry(t)
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	snapshot := exactRootSnapshot(t, []string{"skills/locked-skill/SKILL.md"}, []string{"commands/outside.md", "outside/README.md"})
	if got, want := exactSnapshotFiles(t, snapshot), []string{"skills/locked-skill/SKILL.md"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("path-scoped snapshot files = %v, want %v", got, want)
	}
	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("sync --check from path-scoped snapshot: %v\n%s", err, output)
	}
}

// spec: exact-snapshot-verification/tampering-inside-the-configured-root-is-refused
func TestSyncCheckRejectsTamperingInsideConfiguredRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, _, _, lockPath := setupPathScopedExactRegistry(t)
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := stateTreeSnapshot(t, repo)
	snapshot := exactRootSnapshot(t, []string{"skills/locked-skill/SKILL.md"}, []string{"commands/outside.md", "outside/README.md"})
	if err := os.WriteFile(filepath.Join(snapshot, "skills", "locked-skill", "SKILL.md"), []byte("tampered inside configured root\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("sync", "--check")
	if err == nil || !strings.Contains(output+err.Error(), "lock.snapshot-unavailable") {
		t.Fatalf("sync --check did not refuse the tampered configured root: %v\n%s", err, output)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockBytes) {
		t.Fatalf("sync --check changed the acquisition lock: %v", err)
	}
	if after := stateTreeSnapshot(t, repo); after != stateBefore {
		t.Fatalf("sync --check changed configuration, ownership, or output:\nbefore:\n%s\nafter:\n%s", stateBefore, after)
	}
}

// spec: exact-snapshot-verification/content-outside-the-configured-root-is-not-consumed
func TestSyncCheckIgnoresContentOutsideConfiguredRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, _, _, lockPath := setupPathScopedExactRegistry(t)
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := stateTreeSnapshot(t, repo)
	snapshot := exactRootSnapshot(t, []string{"skills/locked-skill/SKILL.md"}, []string{"commands/outside.md", "outside/README.md"})
	if err := os.MkdirAll(filepath.Join(snapshot, "outside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "outside", "injected.txt"), []byte("not consumed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("sync --check consumed content outside the configured root: %v\n%s", err, output)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockBytes) {
		t.Fatalf("sync --check changed the acquisition lock: %v", err)
	}
	if after := stateTreeSnapshot(t, repo); after != stateBefore {
		t.Fatalf("sync --check changed configuration, ownership, or output:\nbefore:\n%s\nafter:\n%s", stateBefore, after)
	}
}

// spec: exact-snapshot-verification/layers-with-different-paths-for-one-source-resolve-independently
func TestSyncResolvesDifferentPathsForOneSourceAcrossLayers(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "two-roots.git")
	writeSkill(t, working, "user-skill", "User layer skill", nil)
	writeSkill(t, filepath.Join(working, "nested"), "repo-skill", "Repository layer skill", nil)
	writeFile(t, filepath.Join(working, "outside", "README.md"), "not consumed\n")
	commitAndPushAll(t, working, "Add both roots")

	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  user-registry:
    git: file://`+origin+`
    ref: main
    path: skills
resources:
  user-registry:
    include: [skill/user-skill]
`)
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  repo-registry:
    git: file://`+origin+`
    ref: main
    path: nested
resources:
  repo-registry:
    include: [skill/repo-skill]
`)
	withWorkingDir(t, repo)
	for _, scope := range []string{"user", "repo"} {
		if output, err := executeCommand("lock", "--scope", scope); err != nil {
			t.Fatalf("lock --scope %s: %v\n%s", scope, err, output)
		}
	}
	for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".factory", "skills", "user-skill", "SKILL.md")); err != nil {
		t.Fatalf("user root resource was not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "repo-skill", "SKILL.md")); err != nil {
		t.Fatalf("repository root resource was not materialized: %v", err)
	}
	exactRootSnapshot(t, []string{"skills/user-skill/SKILL.md"}, []string{"nested/skills/repo-skill/SKILL.md", "outside/README.md"})
	exactRootSnapshot(t, []string{"nested/skills/repo-skill/SKILL.md"}, []string{"skills/user-skill/SKILL.md", "outside/README.md"})
}

// spec: exact-snapshot-verification/existing-whole-repository-snapshot-remains-usable
func TestSyncCheckFallsBackToExistingWholeRepositorySnapshot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, origin, working, lockPath := setupPathScopedExactRegistry(t)
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	clearExactSnapshots(t)
	commit, err := exec.Command("git", "-C", working, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("resolve locked commit: %v", err)
	}
	oid, err := cache.ParseGitOID("sha1:" + strings.TrimSpace(string(commit)))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := lockfile.CanonicalGitURL("file://" + origin)
	if err != nil {
		t.Fatalf("canonicalize source URL: %v", err)
	}
	if _, err := cache.AcquireExactGit(canonical, origin, oid); err != nil {
		t.Fatalf("seed whole-repository snapshot: %v", err)
	}
	exactRootSnapshot(t, []string{"skills/locked-skill/SKILL.md", "commands/outside.md", "outside/README.md"}, nil)

	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("sync --check from whole-repository fallback: %v\n%s", err, output)
	}
	if got := len(exactSnapshotDirs(t)); got != 1 {
		t.Fatalf("read-only fallback published a root snapshot; found %d snapshots, want 1", got)
	}
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked after whole-repository fallback: %v\n%s", err, output)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockBytes) {
		t.Fatalf("sync changed the acquisition lock: %v", err)
	}
	exactRootSnapshot(t, []string{"skills/locked-skill/SKILL.md"}, []string{"commands/outside.md", "outside/README.md"})
}

// spec: exact-snapshot-verification/snapshot-tampered-between-commands-is-rejected-by-the-next-read-only-command
func TestSyncCheckRejectsSnapshotTamperedBetweenCommands(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, _, _, lockPath := setupReadOnlyExactRegistry(t)
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := stateTreeSnapshot(t, repo)
	tamperExactSnapshot(t)

	output, err := executeCommand("sync", "--check")
	if err == nil || !strings.Contains(output+err.Error(), "lock.snapshot-unavailable") {
		t.Fatalf("sync --check did not reject the tampered snapshot: %v\n%s", err, output)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockBytes) {
		t.Fatalf("sync --check changed the acquisition lock: %v", err)
	}
	if after := stateTreeSnapshot(t, repo); after != stateBefore {
		t.Fatalf("sync --check changed configuration, ownership, or output:\nbefore:\n%s\nafter:\n%s", stateBefore, after)
	}
}

// spec: exact-snapshot-verification/snapshot-tampered-between-commands-is-repaired-by-the-next-sync
func TestSyncLockedRepairsSnapshotTamperedBetweenCommands(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, _, lockPath := setupReadOnlyExactRegistry(t)
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	tamperExactSnapshot(t)

	output, err := executeCommand("sync", "--locked")
	if err != nil {
		t.Fatalf("second sync --locked did not repair the tampered snapshot: %v\n%s", err, output)
	}
	for _, dir := range exactSnapshotDirs(t) {
		matches, globErr := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md"))
		if globErr != nil || len(matches) == 0 {
			t.Fatalf("snapshot %s has no tracked skill files: %v", dir, globErr)
		}
		for _, path := range matches {
			restored, readErr := os.ReadFile(path)
			if readErr != nil || !strings.Contains(string(restored), "Locked") || strings.Contains(string(restored), "tampered") {
				t.Fatalf("repaired snapshot %s does not match the locked commit: %v\n%s", path, readErr, restored)
			}
		}
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockBytes) {
		t.Fatalf("repair changed the acquisition lock: %v", err)
	}
}

// spec: exact-snapshot-verification/equivalent-non-canonical-repository-metadata-is-accepted
func TestSyncCheckAcceptsNonCanonicalSnapshotConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, _, _, _ = setupReadOnlyExactRegistry(t)
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	variants := []string{
		"; leading comment\n[Core]\n   RepositoryFormatVersion = 0\n\tFileMode = true\n\tBare = false\n\tLogAllRefUpdates = true\n# trailing comment\n",
		"[core]\n\trepositoryformatversion=0\n\tfilemode =   true\n\tbare = false\n\tlogallrefupdates = true\n\n; spaced and commented\n",
		"[CORE]\n\tREPOSITORYFORMATVERSION = \"0\"\n\tFILEMODE = \"true\"\n\tBARE = \"false\"\n\tLOGALLREFUPDATES = \"true\"\n",
	}
	for i, variant := range variants {
		for _, dir := range exactSnapshotDirs(t) {
			configPath := filepath.Join(dir, ".git", "config")
			if err := os.WriteFile(configPath, []byte(variant), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		output, err := executeCommand("sync", "--check")
		if err != nil {
			t.Fatalf("sync --check rejected equivalent non-canonical config variant %d: %v\n%s", i, err, output)
		}
	}
}

// spec: exact-snapshot-verification/disallowed-repository-configuration-is-rejected-however-it-is-written
func TestSyncCheckRejectsDisallowedSnapshotConfigSpellings(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, _, _, lockPath := setupReadOnlyExactRegistry(t)
	if output, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("sync --locked: %v\n%s", err, output)
	}
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore := stateTreeSnapshot(t, repo)
	base := "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n\tlogallrefupdates = true\n"
	for _, dir := range exactSnapshotDirs(t) {
		configPath := filepath.Join(dir, ".git", "config")
		if err := os.WriteFile(configPath, []byte(base), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("sync --check rejected the canonical baseline config: %v\n%s", err, output)
	}
	variants := []string{
		base + "\tWORKTREE = \"/elsewhere\" ; trailing comment\n",
		"[Core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n\tlogallrefupdates = true\n\tWorkTree = \"/elsewhere\"\n",
		base + "[include]\n\tpath = \"/dev/null\"\n",
		base + "\tworktree = \"/else\\\n\twhere\"\n",
	}
	for i, variant := range variants {
		for _, dir := range exactSnapshotDirs(t) {
			configPath := filepath.Join(dir, ".git", "config")
			if err := os.WriteFile(configPath, []byte(variant), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		output, err := executeCommand("sync", "--check")
		if err == nil || !strings.Contains(output+err.Error(), "lock.snapshot-unavailable") {
			t.Fatalf("sync --check accepted disallowed config variant %d: %v\n%s", i, err, output)
		}
		if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockBytes) {
			t.Fatalf("sync --check changed the acquisition lock for variant %d: %v", i, err)
		}
	}
	if after := stateTreeSnapshot(t, repo); after != stateBefore {
		t.Fatalf("rejections changed configuration, ownership, or output:\nbefore:\n%s\nafter:\n%s", stateBefore, after)
	}
}

func TestLongRunningCommandsKeepNoCommandScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	setupTelemetryHome(t)
	t.Setenv("AGENT_MARKETPLACE_NO_UPDATE_CHECK", "1")
	root := newRootCmd()
	commands := map[string]*cobra.Command{}
	for _, cmd := range root.Commands() {
		commands[cmd.Name()] = cmd
	}
	for _, name := range []string{"serve", "tui", "sync"} {
		if commands[name] == nil {
			t.Fatalf("root command is missing %s", name)
		}
	}
	for _, name := range []string{"serve", "tui"} {
		cmd := commands[name]
		cmd.SetContext(context.Background())
		err := root.PersistentPreRunE(cmd, nil)
		finishRootLogging(err)
		if err != nil {
			t.Fatalf("%s pre-run: %v", name, err)
		}
		if cache.ExactGitVerificationScopePresent(cmd.Context()) {
			t.Fatalf("%s context carries an exact-git verification scope; long-running sessions must verify each action in a fresh scope", name)
		}
	}
	sync := commands["sync"]
	sync.SetContext(context.Background())
	err := root.PersistentPreRunE(sync, nil)
	finishRootLogging(err)
	if err != nil {
		t.Fatalf("sync pre-run: %v", err)
	}
	if !cache.ExactGitVerificationScopePresent(sync.Context()) {
		t.Fatal("sync context is missing its per-command exact-git verification scope")
	}
}
