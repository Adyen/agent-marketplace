package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// spec: hook-native-independent-satisfaction/independent-clients-recognize-complete-repository-projection
// spec: hook-native-independent-satisfaction/read-only-reconstruction-leaves-absent-state-absent
// spec: hook-native-independent-satisfaction/reconstruction-grants-no-ownership-or-pruning
// spec: hook-native-location-tolerance/handwritten-native-satisfaction-prevents-legacy-recreation
// spec: hook-native-location-tolerance/handwritten-hook-does-not-become-owned
func TestSyncIndependentNativeHookPreservesCommittedLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	registryOrigin, registryWork := createBareRepoWithWorkingClone(t, "independent-native-hooks.git")
	sourceHook := filepath.Join(registryWork, ".factory", "hooks", "hooks.json")
	writeFile(t, sourceHook, `{"PreToolUse":[{"matcher":"Execute","commandRegex":"^make.*$","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/hooks/scripts/run.sh"}]}]}`)
	sourceSupportBytes := []byte("#!/bin/sh\necho independent witness\n")
	sourceSupport := filepath.Join(registryWork, ".factory", "hooks", "scripts", "run.sh")
	writeFile(t, sourceSupport, string(sourceSupportBytes))
	if err := os.Chmod(sourceSupport, 0o755); err != nil {
		t.Fatal(err)
	}
	commitAndPushAll(t, registryWork, "add independent Factory hook")

	sourceURL := "file://" + filepath.ToSlash(registryOrigin)
	sourceKey := syncIndependentNativeSourceKey(registryOrigin, sourceURL)
	_, repoRoot := createBareRepoWithWorkingClone(t, "independent-native-consumer.git")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	configBody := fmt.Sprintf(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  shared:
    git: %s
    ref: main
    source_harness: factory
resources:
  shared:
    include:
      - hook/PreToolUse-command
`, sourceURL)
	writeConfig(t, configPath, configBody)
	withWorkingDir(t, repoRoot)
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "absent-org.yaml"))

	actorA := newSyncIndependentNativeActor(t)
	actorB := newSyncIndependentNativeActor(t)
	activateSyncIndependentNativeActor(t, actorA)
	configBeforeLock := snapshotSyncIndependentNativeFile(t, configPath)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("lock --scope repo: %v\n%s", err, output)
	}
	assertSyncIndependentNativeFileUnchanged(t, "lock --scope repo", configBeforeLock)
	assertSyncIndependentNativeStateAbsent(t, "lock --scope repo", actorA, actorB)
	lockBeforeActorBLock := snapshotSyncIndependentNativeFile(t, lockPath)
	activateSyncIndependentNativeActor(t, actorB)
	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("actor B lock --scope repo: %v\n%s", err, output)
	}
	assertSyncIndependentNativeFileUnchanged(t, "actor B lock --scope repo", configBeforeLock)
	assertSyncIndependentNativeFileUnchanged(t, "actor B lock --scope repo", lockBeforeActorBLock)
	assertSyncIndependentNativeStateAbsent(t, "actor B lock --scope repo", actorA, actorB)

	lockDoc := readLockDocument(t, lockPath)
	if lockDoc.Scope != "repo" || len(lockDoc.Registries) != 1 {
		t.Fatalf("healthy repository lock = %#v", lockDoc)
	}
	registry := lockDoc.Registries[0]
	if config.NormalizeGitURL(registry.URL) != config.NormalizeGitURL(sourceURL) || registry.Ref != "main" || registry.Commit.String() == "" || len(registry.Roots) != 1 {
		t.Fatalf("repository lock did not pin the expected Git source: %#v", registry)
	}
	root := registry.Roots[0]
	if root.SourceHarness != "factory" || config.NormalizeRegistryPath(root.Path) != "" || len(root.Resources) != 1 {
		t.Fatalf("repository lock root = %#v", root)
	}
	resource := root.Resources[0]
	if resource.Kind != "hook" || resource.Name != "PreToolUse-command" {
		t.Fatalf("catalog selected hook = %s/%s, want hook/PreToolUse-command", resource.Kind, resource.Name)
	}
	commitAndPushAll(t, repoRoot, "commit Factory hook selection and healthy lock")

	nativeRoot := filepath.Join(repoRoot, ".factory")
	nativePath := filepath.Join(nativeRoot, "hooks.json")
	legacyPath := filepath.Join(nativeRoot, "hooks", "hooks.json")
	supportPath := filepath.Join(nativeRoot, "hook-support", sourceKey, "PreToolUse-command", "hooks", "scripts", "run.sh")
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeCommand := `\"$FACTORY_PROJECT_DIR\"/.factory/hook-support/` + sourceKey + `/PreToolUse-command/hooks/scripts/run.sh`
	nativeGroup := `{"hooks":[{"type":"command","command":"` + nativeCommand + `"}],"matcher":"Execute","commandRegex":"^make.*$"}`
	handwrittenGroup := `{"matcher":"Edit","hooks":[{"type":"command","command":"echo handwritten"}]}`
	nativeBytes := []byte(`{"PostToolUse":[` + handwrittenGroup + `],"PreToolUse":[` + nativeGroup + `]}` + "\n")
	expectedNativeEntries := []ownership.EntryValue{
		syncIndependentNativeHookEntryValue(t, "PreToolUse", nativeGroup),
		syncIndependentNativeHookEntryValue(t, "PostToolUse", handwrittenGroup),
	}
	if err := os.WriteFile(nativePath, nativeBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nativePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supportPath, sourceSupportBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(supportPath, 0o755); err != nil {
		t.Fatal(err)
	}

	files := []syncIndependentNativeFileSnapshot{
		snapshotSyncIndependentNativeFile(t, configPath),
		snapshotSyncIndependentNativeFile(t, lockPath),
		snapshotSyncIndependentNativeFile(t, nativePath),
		snapshotSyncIndependentNativeFile(t, supportPath),
	}
	if files[3].info.Mode().Perm() != 0o755 {
		t.Fatalf("portable support mode = %04o, want 0755", files[3].info.Mode().Perm())
	}
	assertSyncIndependentNativeLegacyAbsent(t, "before sync", legacyPath)
	assertSyncIndependentNativeStateAbsent(t, "before sync", actorA, actorB)

	run := func(actor syncIndependentNativeActor, stage string, args ...string) {
		t.Helper()
		activateSyncIndependentNativeActor(t, actor)
		output, err := executeCommand(args...)
		if err != nil {
			t.Fatalf("%s: %v\n%s", stage, err, output)
		}
		for _, file := range files {
			assertSyncIndependentNativeFileUnchanged(t, stage, file)
		}
		assertSyncIndependentNativeLegacyAbsent(t, stage, legacyPath)
	}

	run(actorA, "actor A sync --check before mutation", "sync", "--check")
	assertSyncIndependentNativeStateAbsent(t, "actor A sync --check before mutation", actorA, actorB)
	run(actorB, "actor B sync --check before mutation", "sync", "--check")
	assertSyncIndependentNativeStateAbsent(t, "actor B sync --check before mutation", actorA, actorB)

	run(actorA, "actor A sync", "sync")
	assertSyncIndependentNativeHookProof(t, "actor A sync", actorA, nativeRoot, resource.Name, expectedNativeEntries)
	assertSyncIndependentNativeStateAbsent(t, "actor A sync for actor B", actorB)
	assertSyncIndependentNativeSafetyBackupsAbsent(t, "actor A sync", actorA, actorB)
	run(actorB, "actor B sync --locked", "sync", "--locked")
	assertSyncIndependentNativeHookProof(t, "actor B sync --locked", actorA, nativeRoot, resource.Name, expectedNativeEntries)
	assertSyncIndependentNativeHookProof(t, "actor B sync --locked", actorB, nativeRoot, resource.Name, expectedNativeEntries)
	assertSyncIndependentNativeSafetyBackupsAbsent(t, "actor B sync --locked", actorA, actorB)

	actorAStateBeforeActorBCheck := snapshotSyncIndependentNativeStateTree(t, actorA)
	actorBStateBeforeActorBCheck := snapshotSyncIndependentNativeStateTree(t, actorB)
	run(actorB, "actor B sync --check", "sync", "--check")
	assertSyncIndependentNativeStateTreeUnchanged(t, "actor B sync --check", actorA, actorAStateBeforeActorBCheck)
	assertSyncIndependentNativeStateTreeUnchanged(t, "actor B sync --check", actorB, actorBStateBeforeActorBCheck)
	assertSyncIndependentNativeSafetyBackupsAbsent(t, "actor B sync --check", actorA, actorB)

	run(actorA, "actor A sync --locked", "sync", "--locked")
	assertSyncIndependentNativeHookProof(t, "actor A sync --locked", actorA, nativeRoot, resource.Name, expectedNativeEntries)
	assertSyncIndependentNativeHookProof(t, "actor A sync --locked", actorB, nativeRoot, resource.Name, expectedNativeEntries)
	assertSyncIndependentNativeSafetyBackupsAbsent(t, "actor A sync --locked", actorA, actorB)

	actorAStateBeforeActorACheck := snapshotSyncIndependentNativeStateTree(t, actorA)
	actorBStateBeforeActorACheck := snapshotSyncIndependentNativeStateTree(t, actorB)
	run(actorA, "actor A sync --check", "sync", "--check")
	assertSyncIndependentNativeStateTreeUnchanged(t, "actor A sync --check", actorA, actorAStateBeforeActorACheck)
	assertSyncIndependentNativeStateTreeUnchanged(t, "actor A sync --check", actorB, actorBStateBeforeActorACheck)
	assertSyncIndependentNativeSafetyBackupsAbsent(t, "actor A sync --check", actorA, actorB)
}

type syncIndependentNativeActor struct {
	home      string
	xdgConfig string
	xdgCache  string
	xdgData   string
	xdgState  string
	codexHome string
}

func newSyncIndependentNativeActor(t *testing.T) syncIndependentNativeActor {
	t.Helper()
	home := t.TempDir()
	return syncIndependentNativeActor{
		home:      home,
		xdgConfig: filepath.Join(home, "config"),
		xdgCache:  filepath.Join(home, "cache"),
		xdgData:   filepath.Join(home, "data"),
		xdgState:  filepath.Join(home, "state"),
		codexHome: filepath.Join(home, ".codex"),
	}
}

func activateSyncIndependentNativeActor(t *testing.T, actor syncIndependentNativeActor) {
	t.Helper()
	t.Setenv("HOME", actor.home)
	t.Setenv("XDG_CONFIG_HOME", actor.xdgConfig)
	t.Setenv("XDG_CACHE_HOME", actor.xdgCache)
	t.Setenv("XDG_DATA_HOME", actor.xdgData)
	t.Setenv("XDG_STATE_HOME", actor.xdgState)
	t.Setenv("CODEX_HOME", actor.codexHome)
}

type syncIndependentNativeFileSnapshot struct {
	path string
	data []byte
	info os.FileInfo
}

func snapshotSyncIndependentNativeFile(t *testing.T, path string) syncIndependentNativeFileSnapshot {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file: %v", path, info.Mode())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return syncIndependentNativeFileSnapshot{path: path, data: data, info: info}
}

func assertSyncIndependentNativeFileUnchanged(t *testing.T, stage string, before syncIndependentNativeFileSnapshot) {
	t.Helper()
	afterInfo, err := os.Lstat(before.path)
	if err != nil {
		t.Fatalf("%s removed %s: %v", stage, before.path, err)
	}
	if !afterInfo.Mode().IsRegular() {
		t.Fatalf("%s replaced %s with %v", stage, before.path, afterInfo.Mode())
	}
	afterData, err := os.ReadFile(before.path)
	if err != nil {
		t.Fatalf("%s read %s: %v", stage, before.path, err)
	}
	if !bytes.Equal(afterData, before.data) || !os.SameFile(before.info, afterInfo) || before.info.Mode() != afterInfo.Mode() || !before.info.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatalf("%s changed %s (bytes, mode, mtime, or inode)", stage, before.path)
	}
}

func assertSyncIndependentNativeLegacyAbsent(t *testing.T, stage, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
		t.Fatalf("%s recreated legacy hook output %s: %v", stage, path, err)
	}
}

func assertSyncIndependentNativeStateAbsent(t *testing.T, stage string, actors ...syncIndependentNativeActor) {
	t.Helper()
	for _, actor := range actors {
		for _, area := range []string{"ownership", "safety-backups"} {
			stateRoot := filepath.Join(actor.xdgState, "agent-marketplace", area)
			if _, err := os.Lstat(stateRoot); err == nil || !os.IsNotExist(err) {
				t.Fatalf("%s created %s for %s: %v", stage, stateRoot, actor.home, err)
			}
		}
	}
}

type syncIndependentNativeStateEntrySnapshot struct {
	info       os.FileInfo
	data       []byte
	linkTarget string
}

type syncIndependentNativeStateTreeSnapshot struct {
	exists  bool
	entries map[string]syncIndependentNativeStateEntrySnapshot
}

func snapshotSyncIndependentNativeStateTree(t *testing.T, actor syncIndependentNativeActor) syncIndependentNativeStateTreeSnapshot {
	t.Helper()
	root := filepath.Join(actor.xdgState, "agent-marketplace")
	rootInfo, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return syncIndependentNativeStateTreeSnapshot{}
	}
	if err != nil {
		t.Fatalf("lstat state root %s: %v", root, err)
	}
	if !rootInfo.IsDir() {
		t.Fatalf("state root %s is not a directory: %v", root, rootInfo.Mode())
	}
	snapshot := syncIndependentNativeStateTreeSnapshot{exists: true, entries: make(map[string]syncIndependentNativeStateEntrySnapshot)}
	add := func(path, relative string, info os.FileInfo) error {
		entry := syncIndependentNativeStateEntrySnapshot{info: info}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry.data = data
		} else if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry.linkTarget = linkTarget
		}
		snapshot.entries[relative] = entry
		return nil
	}
	if err := add(root, ".", rootInfo); err != nil {
		t.Fatalf("snapshot state root %s: %v", root, err)
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return add(path, filepath.ToSlash(relative), info)
	}); err != nil {
		t.Fatalf("walk state root %s: %v", root, err)
	}
	return snapshot
}

func assertSyncIndependentNativeStateTreeUnchanged(t *testing.T, stage string, actor syncIndependentNativeActor, before syncIndependentNativeStateTreeSnapshot) {
	t.Helper()
	after := snapshotSyncIndependentNativeStateTree(t, actor)
	if before.exists != after.exists {
		t.Fatalf("%s changed state-root existence for %s", stage, actor.home)
	}
	if len(before.entries) != len(after.entries) {
		t.Fatalf("%s changed state paths for %s: before=%v after=%v", stage, actor.home, sortedSyncIndependentNativeStatePaths(before.entries), sortedSyncIndependentNativeStatePaths(after.entries))
	}
	for _, relative := range sortedSyncIndependentNativeStatePaths(before.entries) {
		prior := before.entries[relative]
		current, ok := after.entries[relative]
		if !ok || !os.SameFile(prior.info, current.info) || prior.info.Mode() != current.info.Mode() ||
			!prior.info.ModTime().Equal(current.info.ModTime()) || !bytes.Equal(prior.data, current.data) || prior.linkTarget != current.linkTarget {
			t.Fatalf("%s changed state entry %s for %s", stage, relative, actor.home)
		}
	}
}

func sortedSyncIndependentNativeStatePaths(entries map[string]syncIndependentNativeStateEntrySnapshot) []string {
	paths := make([]string, 0, len(entries))
	for path := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func assertSyncIndependentNativeSafetyBackupsAbsent(t *testing.T, stage string, actors ...syncIndependentNativeActor) {
	t.Helper()
	for _, actor := range actors {
		path := filepath.Join(actor.xdgState, "agent-marketplace", "safety-backups")
		if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s created safety backups for %s: %v", stage, actor.home, err)
		}
	}
}

func syncIndependentNativeHookEntryValue(t *testing.T, event, raw string) ownership.EntryValue {
	t.Helper()
	canonical, err := jsonc.Canonicalize("hooks.json", []byte(raw))
	if err != nil {
		t.Fatalf("canonicalize expected %s hook: %v", event, err)
	}
	valueDigest := ownership.Digest(canonical)
	return ownership.EntryValue{
		ID:          ownership.EntryID{Kind: ownership.KindHook, Container: event, IdentityDigest: ownership.HookIdentityDigest(event, valueDigest)},
		ValueDigest: valueDigest,
	}
}

func assertSyncIndependentNativeHookProof(t *testing.T, stage string, actor syncIndependentNativeActor, nativeRoot, selectedResource string, expected []ownership.EntryValue) {
	t.Helper()
	activateSyncIndependentNativeActor(t, actor)
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatalf("%s open ownership proof for %s: %v", stage, actor.home, err)
	}
	proof, err := store.Proof("hooks.json")
	if err != nil && !errors.Is(err, ownership.ErrStateMissing) {
		t.Fatalf("%s read ownership proof for %s: %v", stage, actor.home, err)
	}
	if err != nil {
		proof = ownership.Proof{}
	}
	if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 {
		t.Fatalf("%s adopted native hooks for %s: owned=%v", stage, actor.home, proof.OwnedIDs())
	}
	for _, entry := range expected {
		if status := proof.Classify(entry.ID, entry.ValueDigest); status != ownership.StatusUnmanaged {
			t.Fatalf("%s classified native hook %s/%s as %v for %s", stage, entry.ID.Container, entry.ID.IdentityDigest, status, actor.home)
		}
	}
	for _, satisfaction := range proof.HookSatisfactions() {
		if satisfaction.Container != "PreToolUse" || len(satisfaction.Sources) == 0 {
			t.Fatalf("%s recorded unexpected satisfaction for %s: %+v", stage, actor.home, satisfaction)
		}
		for _, source := range satisfaction.Sources {
			if source.Resource != selectedResource {
				t.Fatalf("%s recorded satisfaction for unrelated resource %q for %s", stage, source.Resource, actor.home)
			}
		}
	}
}

func syncIndependentNativeSourceKey(origin, sourceURL string) string {
	slug := strings.TrimSuffix(filepath.Base(origin), ".git")
	identity := "git:" + config.NormalizeGitURL(sourceURL) + "@main:" + config.NormalizeRegistryPath("")
	digest := sha256.Sum256([]byte(identity))
	return slug + "-" + hex.EncodeToString(digest[:])
}
