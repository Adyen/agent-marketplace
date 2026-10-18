package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/cache"
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
