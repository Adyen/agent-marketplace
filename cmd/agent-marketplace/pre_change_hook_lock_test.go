package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: hook-resources/pre-change-hook-lock-verifies-unchanged

// A repository whose acquisition lock was written before the native-hook
// change (v4 schema, Factory source layout, literal pre-change digests over a
// hooks.json+support+droid inventory) must keep verifying: sync --locked,
// plain sync, and sync --check succeed without touching the lock bytes, an
// upstream move changes nothing until an explicit update, and update
// refreshes the revision through the existing v4 generator path.
func TestPreChangeHookLockVerifiesUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "pre-change-hooks.git")
	hooksPath := filepath.Join(working, ".factory", "hooks", "hooks.json")
	writeFile(t, hooksPath, `{"PreToolUse":[{"matcher":"Execute","commandRegex":"^git ","hooks":[{"type":"command","command":"echo legacy-factory"}]}]}`)
	writeFile(t, filepath.Join(working, ".factory", "hooks", "check.sh"), "#!/bin/sh\necho legacy-support\n")
	writeFile(t, filepath.Join(working, ".factory", "droids", "review.md"), "---\nname: review\ndescription: Legacy review\ntools: [Read, Execute]\n---\nReview the change.\n")
	commitAndPushAll(t, working, "legacy factory hook inventory")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
harnesses: [factory]
registries:
  - url: `+origin+`
    ref: main
    source_harness: factory
    default: false
    hooks:
      - PreToolUse-command
`)
	withWorkingDir(t, repoRoot)

	// lock resolves the dynamic git URL/ref/commit/root-path identities; the
	// pre-change digest and resource data below are the frozen historical
	// bytes, not derived from this binary's builder.
	if output, err := executeCommand("lock"); err != nil {
		t.Fatalf("seed lock: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	doc := readExactManifestLock(t, lockPath)
	if len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 {
		t.Fatalf("fixture lock shape = %+v, want one registry with one root", doc.Registries)
	}
	root := &doc.Registries[0].Roots[0]
	oldCommit := doc.Registries[0].Commit
	root.RevisionSchema = "v4"
	root.SourceHarness = "factory"
	root.RevisionDigest = lockfile.Digest("sha256:f51f5d2c733ed5684a7833f9c41f51475deac38e26049a790aefb48fad4c8343")
	root.Resources = []lockfile.GitResource{
		{
			Kind:          "hook",
			Name:          "PreToolUse-command",
			ContentDigest: lockfile.Digest("sha256:6aff9aec1c5e6b3d5979edfb8dfb4a67be94c16f8f0e1fa4f3fdcbd17d7c8d6c"),
			ArchiveDigest: lockfile.Digest("sha256:f3764fdc7c705bfd9fcfcf73e1105a0f4044ae07ffcffd70396eee7e2276dd05"),
			SupportFiles: map[string]lockfile.Digest{
				"check.sh": lockfile.Digest("sha256:f837d00a2bac29fc731ebaf85ef8c8db0b5cc14d301040b2574fc23c47c73862"),
			},
		},
		{
			Kind:          "subagent",
			Name:          "review",
			ContentDigest: lockfile.Digest("sha256:2b8c4e574b21b3b1a4251c8cf9f87face1568297b73d55a19956694791dad77e"),
			ArchiveDigest: lockfile.Digest("sha256:2b8c4e574b21b3b1a4251c8cf9f87face1568297b73d55a19956694791dad77e"),
		},
	}
	writeExactManifestLock(t, lockPath, doc)
	oldLockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	oldConfigBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	assertLockUnchanged := func(step string, wantLockBytes []byte) {
		t.Helper()
		after, readErr := os.ReadFile(lockPath)
		if readErr != nil {
			t.Fatalf("%s: read lock: %v", step, readErr)
		}
		if !bytes.Equal(after, wantLockBytes) {
			t.Fatalf("%s rewrote the lock\nbefore:\n%s\nafter:\n%s", step, wantLockBytes, after)
		}
		afterConfig, readErr := os.ReadFile(configPath)
		if readErr != nil {
			t.Fatalf("%s: read config: %v", step, readErr)
		}
		if !bytes.Equal(afterConfig, oldConfigBytes) {
			t.Fatalf("%s changed config bytes\nbefore:\n%s\nafter:\n%s", step, oldConfigBytes, afterConfig)
		}
	}
	assertMaterializedHook := func(step, wantCommand string) {
		t.Helper()
		data, readErr := os.ReadFile(filepath.Join(repoRoot, ".factory", "hooks", "hooks.json"))
		if readErr != nil {
			t.Fatalf("%s: read materialized hooks: %v", step, readErr)
		}
		var document map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: decode materialized hooks: %v\n%s", step, err, data)
		}
		groups := document["PreToolUse"]
		if len(document) != 1 || len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s: materialized hooks = %#v, want exactly one PreToolUse group with one handler", step, document)
		}
		group := groups[0]
		handler := group.Hooks[0]
		if group.Matcher != "Execute" || handler.Type != "command" || handler.Command != wantCommand {
			t.Fatalf("%s: materialized hook = matcher %q type %q command %q, want matcher Execute, sole handler type command, command %q", step, group.Matcher, handler.Type, handler.Command, wantCommand)
		}
	}

	for _, args := range [][]string{{"sync", "--locked"}, {"sync"}, {"sync", "--check"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v on the pre-change lock: %v\n%s", args, err, output)
		}
		assertLockUnchanged(strings.Join(args, " "), oldLockBytes)
	}
	assertMaterializedHook("initial sync", "echo legacy-factory")

	// Advance upstream: only the hook's command changes. Locked, plain, and
	// check runs still pin the old commit and keep the old command.
	writeFile(t, hooksPath, `{"PreToolUse":[{"matcher":"Execute","commandRegex":"^git ","hooks":[{"type":"command","command":"echo next-factory"}]}]}`)
	commitAndPushAll(t, working, "advance hook command")
	for _, args := range [][]string{{"sync", "--locked"}, {"sync"}, {"sync", "--check"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v after upstream advance: %v\n%s", args, err, output)
		}
		assertLockUnchanged(strings.Join(args, " ")+" after upstream advance", oldLockBytes)
	}
	assertMaterializedHook("post-advance sync", "echo legacy-factory")

	// An explicit update refreshes the selected revision through the existing
	// v4 path, materializes the new command, and leaves config bytes stable.
	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("update: %v\n%s", err, output)
	}
	updated := readExactManifestLock(t, lockPath)
	updatedRoot := updated.Registries[0].Roots[0]
	if updated.Registries[0].Commit == oldCommit {
		t.Fatalf("update kept the old commit %s", updated.Registries[0].Commit)
	}
	if updatedRoot.RevisionSchema != "v4" {
		t.Fatalf("updated revision schema = %q, want v4", updatedRoot.RevisionSchema)
	}
	if updatedRoot.RevisionDigest == root.RevisionDigest {
		t.Fatalf("update kept the pre-change revision digest")
	}
	assertMaterializedHook("post-update sync", "echo next-factory")
	newLockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"sync", "--locked"}, {"sync", "--check"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v on the updated lock: %v\n%s", args, err, output)
		}
		assertLockUnchanged(strings.Join(args, " ")+" on the updated lock", newLockBytes)
	}
}
