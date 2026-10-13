package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
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
	writeFile(t, sourceHook, `{"PreToolUse":[{"matcher":"Execute","commandRegex":"^make.*$","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/scripts/run.sh"}]}]}`)
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

	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
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
	supportPath := filepath.Join(nativeRoot, "hook-support", sourceKey, "PreToolUse-command", "scripts", "run.sh")
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeCommand := `\"$FACTORY_PROJECT_DIR\"/.factory/hook-support/` + sourceKey + `/PreToolUse-command/scripts/run.sh`
	nativeGroup := `{"hooks":[{"type":"command","command":"` + nativeCommand + `"}],"matcher":"Execute","commandRegex":"^make.*$"}`
	nativeBytes := []byte(`{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"echo handwritten"}]}],"PreToolUse":[` + nativeGroup + `]}` + "\n")
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
		assertSyncIndependentNativeStateAbsent(t, stage, actorA, actorB)
	}
	run(actorA, "actor A sync", "sync")
	run(actorB, "actor B sync --locked", "sync", "--locked")
	run(actorB, "actor B sync --check", "sync", "--check")
	run(actorA, "actor A sync --locked", "sync", "--locked")
	run(actorA, "actor A sync --check", "sync", "--check")
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

func syncIndependentNativeSourceKey(origin, sourceURL string) string {
	slug := strings.TrimSuffix(filepath.Base(origin), ".git")
	identity := "git:" + config.NormalizeGitURL(sourceURL) + "@main:" + config.NormalizeRegistryPath("")
	digest := sha256.Sum256([]byte(identity))
	return slug + "-" + hex.EncodeToString(digest[:])
}
