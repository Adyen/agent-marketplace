package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

const cliSatisfactionSentinel = "rm -rf /sentinel-payload"

type cliSatisfactionCase int

const (
	cliSatisfactionComplete cliSatisfactionCase = iota
	cliSatisfactionIncomplete
	cliSatisfactionCorrupt
)

func cliSeedHookSatisfaction(t *testing.T, nativeRoot string, d cliNativeDoc, doc string, fps map[string]string, evidenceCase cliSatisfactionCase) (statePath string) {
	t.Helper()
	abs := filepath.Join(nativeRoot, filepath.FromSlash(d.rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	entry := nativeHookProofEntry(t, d.event, d.rule, cliHookNames[0], fps[cliHookNames[0]])
	pairs := []any{}
	for _, name := range cliHookNames {
		pairs = append(pairs, map[string]any{"resource": name, "source_digest": fps[name]})
	}
	switch evidenceCase {
	case cliSatisfactionIncomplete:
		pairs = pairs[:1]
	case cliSatisfactionCorrupt:
		pairs = append(pairs, map[string]any{"resource": cliSatisfactionSentinel, "source_digest": "sha256:zz"})
	}
	record := map[string]any{
		"version":        1,
		"canonical_root": store.CanonicalRoot(),
		"root_key":       store.RootKey(),
		"generation":     1,
		"updated_at":     "2026-10-04T00:00:00Z",
		"files": map[string]any{d.rel: map[string]any{
			"content_digest": ownership.Digest([]byte(doc)),
			"entries":        []any{},
			"hook_satisfactions": []any{map[string]any{
				"container":       d.event,
				"identity_digest": entry.IdentityDigest,
				"value_digest":    entry.ValueDigest,
				"sources":         pairs,
			}},
		}},
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(store.StateFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.StateFile(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return store.StateFile()
}

func cliSatisfactionFixture(t *testing.T, d cliNativeDoc) (homeDir, repoRoot, nativeRoot, nativePath, legacyPath, lockPath, configPath string, fps map[string]string) {
	t.Helper()
	homeDir = t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	repoRoot = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	fps = cliSharedHookFingerprints(t, repoRoot, homeDir, d)
	base := repoRoot
	if d.machineScope {
		base = homeDir
	}
	nativeRoot = filepath.Join(base, d.rootDir)
	nativePath = filepath.Join(nativeRoot, filepath.FromSlash(d.rel))
	legacyPath = cliLegacyHookPath(d, repoRoot, homeDir)
	lockPath = filepath.Join(repoRoot, ".agents", "lock.yaml")
	configPath = filepath.Join(repoRoot, ".agents", "config.yaml")
	if d.machineScope {
		lockPath = filepath.Join(homeDir, ".agents", "lock.yaml")
		configPath = filepath.Join(homeDir, ".agents", "config.yaml")
	}
	return
}

func seedCliOwnershipLock(t *testing.T, root string) string {
	t.Helper()
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithWriteSessionContext(context.Background(), "seed-cli-ownership-lock", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return store.LockPath()
}
func assertCliSatisfactionUnmanaged(t *testing.T, phase, nativeRoot string, d cliNativeDoc, fps map[string]string) {
	t.Helper()
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(d.rel)
	if err != nil {
		t.Fatalf("%s: proof: %v", phase, err)
	}
	if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 {
		t.Fatalf("%s: satisfaction evidence became owned: %d/%v", phase, proof.OwnedCount(), proof.OwnedIDs())
	}
	entry := nativeHookProofEntry(t, d.event, d.rule, cliHookNames[0], fps[cliHookNames[0]])
	if status := proof.Classify(entry.ID(), entry.ValueDigest); status != ownership.StatusUnmanaged {
		t.Fatalf("%s: satisfied native entry classified %v, want unmanaged", phase, status)
	}
	entries, err := materialize.InspectNativeHooks(store, nativeRoot, hcc.Target(d.target), d.machineScope)
	if err != nil {
		t.Fatalf("%s: InspectNativeHooks: %v", phase, err)
	}
	for _, item := range entries {
		if item.ID == entry.ID() {
			if item.Status != ownership.StatusUnmanaged || item.SourceDigest != "" {
				t.Fatalf("%s: satisfied native entry = %+v, want unmanaged without a source digest", phase, item)
			}
			return
		}
	}
	t.Fatalf("%s: native inspector did not return %v", phase, entry.ID())
}

func assertCliCompleteHookSatisfaction(t *testing.T, nativeRoot string, d cliNativeDoc, fps map[string]string) {
	t.Helper()
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(d.rel)
	if err != nil {
		t.Fatal(err)
	}
	records := proof.HookSatisfactions()
	if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 || len(records) != 1 || records[0].Container != d.event || records[0].IdentityDigest == "" || records[0].ValueDigest == "" {
		t.Fatalf("satisfaction proof = owned %v / records %+v", proof.OwnedIDs(), records)
	}
	want := make(map[ownership.HookSourceEvidence]bool, len(cliHookNames))
	for _, name := range cliHookNames {
		want[ownership.HookSourceEvidence{Resource: name, SourceDigest: fps[name]}] = true
	}
	got := make(map[ownership.HookSourceEvidence]bool, len(records[0].Sources))
	for _, source := range records[0].Sources {
		got[source] = true
	}
	if len(records[0].Sources) != len(want) || len(got) != len(want) {
		t.Fatalf("satisfaction sources = %+v, want exactly both contributors", records[0].Sources)
	}
	for source := range want {
		if !got[source] {
			t.Fatalf("satisfaction sources = %+v, missing %+v", records[0].Sources, source)
		}
	}
}
func cliTwinAndSiblingDoc(d cliNativeDoc) string {
	sibling := `{"command":"./handwritten.sh","matcher":"Bash","type":"command"}`
	twin := `{"_managed_by":"agent-marketplace","command":"` + cliSharedHookCommand + `","matcher":"Bash","type":"command"}`
	switch d.target {
	case "claude":
		return `{"hooks":{"PreToolUse":[` + sibling + `,` + twin + `]}}` + "\n"
	case "cursor":
		sibling = `{"command":"./handwritten.sh","event":"PreToolUse","matcher":"Bash","type":"command"}`
		twin = `{"_managed_by":"agent-marketplace","command":"` + cliSharedHookCommand + `","event":"PreToolUse","matcher":"Bash","type":"command"}`
		return `{"hooks":{"PreToolUse":[` + sibling + `,` + twin + `]}}` + "\n"
	case "factory":
		sibling = `{"hooks":[{"command":"./handwritten.sh","type":"command"}],"matcher":"Bash"}`
		twin = `{"_managed_by":"agent-marketplace","hooks":[{"command":"` + cliSharedHookCommand + `","type":"command"}],"matcher":"Bash"}`
		return `{"PreToolUse":[` + sibling + `,` + twin + `]}` + "\n"
	default:
		return `{"hooks":{"pre_run_command":[{"command":"./handwritten.sh"},{"_managed_by":"agent-marketplace","command":"` + cliSharedHookCommand + `"}]}}` + "\n"
	}
}

func cliLegacyHookRules(t *testing.T, d cliNativeDoc, data []byte) []json.RawMessage {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("legacy document: %v", err)
	}
	container := root
	if d.target != "factory" {
		if err := json.Unmarshal(root["hooks"], &container); err != nil {
			t.Fatalf("legacy hooks member: %v", err)
		}
	}
	event := "PreToolUse"
	if d.target == "windsurf" {
		event = "pre_run_command"
	}
	var rules []json.RawMessage
	if err := json.Unmarshal(container[event], &rules); err != nil {
		t.Fatalf("legacy event %s: %v", event, err)
	}
	return rules
}
func cliTree(t *testing.T, root string) []string {
	t.Helper()
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		t.Fatal(err)
	}
	var paths []string
	if err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func assertCliTreeUnchanged(t *testing.T, phase, root string, before []string) {
	t.Helper()
	after := cliTree(t, root)
	if len(after) != len(before) {
		t.Fatalf("%s: tree under %s changed: %v -> %v", phase, root, before, after)
	}
	for i := range after {
		if after[i] != before[i] {
			t.Fatalf("%s: tree under %s changed: %v -> %v", phase, root, before, after)
		}
	}
}

type cliFilesystemEntry struct {
	mode       os.FileMode
	size       int64
	modified   int64
	data       []byte
	linkTarget string
}

func cliFilesystemSnapshot(t *testing.T, root string) map[string]cliFilesystemEntry {
	t.Helper()
	paths := cliTree(t, root)
	if len(paths) == 0 {
		return nil
	}
	entries := make(map[string]cliFilesystemEntry, len(paths))
	for _, rel := range paths {
		path := root
		if rel != "." {
			path = filepath.Join(root, rel)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("lstat %s: %v", path, err)
		}
		entry := cliFilesystemEntry{mode: info.Mode(), size: info.Size(), modified: info.ModTime().UnixNano()}
		if info.Mode().IsRegular() {
			entry.data, err = os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
		} else if info.Mode()&os.ModeSymlink != 0 {
			entry.linkTarget, err = os.Readlink(path)
			if err != nil {
				t.Fatalf("read link %s: %v", path, err)
			}
		}
		entries[rel] = entry
	}
	return entries
}

func assertCliFilesystemSnapshot(t *testing.T, phase, root string, before map[string]cliFilesystemEntry) {
	t.Helper()
	if !reflect.DeepEqual(before, cliFilesystemSnapshot(t, root)) {
		t.Fatalf("%s: filesystem snapshot under %s changed", phase, root)
	}
}

func cliRelativeStatePath(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("state path %s is outside root %s", path, root)
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

func assertCliFilesystemSnapshotExceptLock(t *testing.T, phase, root, lockPath string, before map[string]cliFilesystemEntry) map[string]cliFilesystemEntry {
	t.Helper()
	lockRel := cliRelativeStatePath(t, root, lockPath)
	after := cliFilesystemSnapshot(t, root)
	beforeLock, beforeOK := before[lockRel]
	afterLock, afterOK := after[lockRel]
	if !beforeOK || !afterOK || beforeLock.mode != afterLock.mode {
		t.Fatalf("ownership lock entry changed unexpectedly during %s", phase)
	}
	normalize := func(entries map[string]cliFilesystemEntry) map[string]cliFilesystemEntry {
		copy := make(map[string]cliFilesystemEntry, len(entries))
		for rel, entry := range entries {
			if rel == lockRel {
				entry.size = 0
				entry.modified = 0
				entry.data = nil
				entry.linkTarget = ""
			}
			copy[rel] = entry
		}
		return copy
	}
	if !reflect.DeepEqual(normalize(before), normalize(after)) {
		t.Fatalf("%s: state filesystem under %s changed outside its ownership lock metadata", phase, root)
	}
	return after
}

func cliPathIsAncestorOf(parent, child string) bool {
	if parent == "." {
		return true
	}
	return strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
}

func assertCliStateOnlyRecordChanged(t *testing.T, phase, root, lockPath, statePath string, before, after map[string]cliFilesystemEntry) {
	t.Helper()
	lockRel := cliRelativeStatePath(t, root, lockPath)
	stateRel := cliRelativeStatePath(t, root, statePath)
	if lockRel == stateRel {
		t.Fatal("ownership lock and state record paths unexpectedly match")
	}
	beforeLock, beforeLockOK := before[lockRel]
	afterLock, afterLockOK := after[lockRel]
	beforeState, beforeStateOK := before[stateRel]
	afterState, afterStateOK := after[stateRel]
	if !beforeLockOK || !afterLockOK || beforeLock.mode != afterLock.mode || !beforeStateOK || !afterStateOK || beforeState.mode != afterState.mode {
		t.Fatalf("%s changed the ownership lock or state record mode", phase)
	}
	if reflect.DeepEqual(beforeState.data, afterState.data) {
		t.Fatalf("%s did not change state record bytes", phase)
	}
	for rel, oldEntry := range before {
		newEntry, ok := after[rel]
		if !ok {
			t.Fatalf("%s removed prior state entry %s", phase, rel)
		}
		if rel == lockRel || rel == stateRel {
			continue
		}
		if oldEntry.mode.IsDir() && (cliPathIsAncestorOf(rel, stateRel) || rel == filepath.ToSlash(filepath.Join("agent-marketplace", "dependencies"))) {
			if newEntry.mode != oldEntry.mode {
				t.Fatalf("%s changed state directory mode at %s", phase, rel)
			}
			continue
		}
		if !reflect.DeepEqual(oldEntry, newEntry) {
			t.Fatalf("%s changed unrelated state entry %s", phase, rel)
		}
	}
	for rel := range after {
		if _, existed := before[rel]; !existed {
			t.Fatalf("%s added unexpected state entry %s", phase, rel)
		}
	}
}

func assertCliStateSnapshotAfterLegacyBackup(t *testing.T, phase, root, lockPath string, before map[string]cliFilesystemEntry, backup ownership.RetainedBackup) map[string]cliFilesystemEntry {
	t.Helper()
	lockRel := cliRelativeStatePath(t, root, lockPath)
	expected := cliSafetyBackupTreeEntries(t, root, backup)
	after := cliFilesystemSnapshot(t, root)
	beforeLock, beforeOK := before[lockRel]
	afterLock, afterOK := after[lockRel]
	if !beforeOK || !afterOK || beforeLock.mode != afterLock.mode {
		t.Fatalf("ownership lock entry changed unexpectedly during %s", phase)
	}
	for rel, oldEntry := range before {
		newEntry, ok := after[rel]
		if !ok {
			t.Fatalf("%s removed prior state entry %s", phase, rel)
		}
		if rel == lockRel {
			continue
		}
		if oldEntry.mode.IsDir() && cliStateDirectoryMetadataMayChange(rel, expected) {
			if newEntry.mode != oldEntry.mode {
				t.Fatalf("%s changed state directory mode at %s", phase, rel)
			}
			continue
		}
		if !reflect.DeepEqual(oldEntry, newEntry) {
			t.Fatalf("%s changed prior state entry %s", phase, rel)
		}
	}
	for rel := range after {
		if _, existed := before[rel]; !existed && !expected[rel] {
			t.Fatalf("%s added unexpected state entry %s", phase, rel)
		}
	}
	for rel := range expected {
		if _, exists := after[rel]; !exists {
			t.Fatalf("%s did not retain expected safety-backup path %s", phase, rel)
		}
	}
	return after
}

func cliSafetyBackupTreeEntries(t *testing.T, stateRoot string, backup ownership.RetainedBackup) map[string]bool {
	t.Helper()
	if !strings.HasSuffix(backup.Path, ".bak") {
		t.Fatalf("safety backup path %q has no .bak suffix", backup.Path)
	}
	metaPath := strings.TrimSuffix(backup.Path, ".bak") + ".meta.json"
	expected := make(map[string]bool)
	for _, file := range []string{backup.Path, metaPath} {
		for current := cliRelativeStatePath(t, stateRoot, file); current != "."; {
			expected[current] = true
			parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(current)))
			if parent == current {
				break
			}
			current = parent
		}
	}
	return expected
}

func cliStateDirectoryMetadataMayChange(rel string, backupPaths map[string]bool) bool {
	if rel == "." || rel == filepath.ToSlash(filepath.Join("agent-marketplace", "dependencies")) {
		return true
	}
	prefix := strings.TrimSuffix(rel, "/") + "/"
	for candidate := range backupPaths {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

func assertCliLegacySafetyBackup(t *testing.T, store *ownership.Store, rel string, original []byte) ownership.RetainedBackup {
	t.Helper()
	backups, err := store.SafetyBackups(rel)
	if err != nil || len(backups) != 1 {
		t.Fatalf("SafetyBackups(%q) = %d / %v, want the one migration backup", rel, len(backups), err)
	}
	backup := backups[0]
	data, err := backup.RawBytes()
	if err != nil || string(data) != string(original) {
		t.Fatalf("safety backup for %s does not contain the exact original Legacy bytes: %v", rel, err)
	}
	info, err := os.Stat(backup.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("safety backup for %s mode = %v / %v, want 0600", rel, info, err)
	}
	if backup.Manifest.File != rel || backup.Manifest.PriorAbsent || backup.Manifest.PriorDigest != ownership.Digest(original) || backup.Manifest.PriorSize != int64(len(original)) {
		t.Fatalf("safety backup manifest for %s = %+v", rel, backup.Manifest)
	}
	return backup

}

// spec: hook-native-location-tolerance/handwritten-native-satisfaction-prevents-legacy-recreation
// spec: hook-native-location-tolerance/satisfaction-checks-leave-all-evidence-unchanged
func TestSyncRecognizesUnownedNativeSatisfaction(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, d := range cliNativeDocs() {
		sharedFile := d.target == "windsurf" && d.machineScope
		twins := []bool{false, true}
		if sharedFile {
			twins = []bool{false}
		}
		for _, twin := range twins {
			t.Run(fmt.Sprintf("%s_machine=%v_twin=%v", d.target, d.machineScope, twin), func(t *testing.T) {
				home, _, nativeRoot, nativePath, legacyPath, lockPath, configPath, fps := cliSatisfactionFixture(t, d)
				var legacySeed []byte
				var siblingRaw string
				var legacyRel string
				if twin {
					legacySeed = []byte(cliTwinAndSiblingDoc(d))
					legacyRules := cliLegacyHookRules(t, d, legacySeed)
					if len(legacyRules) != 2 || strings.Contains(string(legacyRules[0]), "_managed_by") || !strings.Contains(string(legacyRules[1]), "_managed_by") {
						t.Fatalf("legacy twin fixture does not contain handwritten sibling then marked twin: %q", legacySeed)
					}
					siblingRaw = string(legacyRules[0])
					rel, err := filepath.Rel(nativeRoot, legacyPath)
					if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
						t.Fatalf("legacy path %s is outside Native root %s", legacyPath, nativeRoot)
					}
					legacyRel = filepath.ToSlash(rel)
					writeFile(t, legacyPath, string(legacySeed))
				}
				statePath := cliSeedHookSatisfaction(t, nativeRoot, d, d.doc, fps, cliSatisfactionComplete)
				ownerLockPath := seedCliOwnershipLock(t, nativeRoot)

				nativeBefore, nativeInfo := statSnapshot(t, nativePath)
				stateBefore, stateInfo := statSnapshot(t, statePath)
				lockBefore, lockInfo := statSnapshot(t, lockPath)
				configBefore, configInfo := statSnapshot(t, configPath)
				stateRoot := os.Getenv("XDG_STATE_HOME")
				stateSnapshotBefore := cliFilesystemSnapshot(t, stateRoot)
				nativeTreeBefore := cliTree(t, nativeRoot)
				nativeSnapshotBefore := cliFilesystemSnapshot(t, nativeRoot)
				cacheTree := cliFilesystemSnapshot(t, filepath.Join(home, "cache"))
				var stateSnapshotAfterFirst map[string]cliFilesystemEntry
				var nativeSnapshotAfterFirst map[string]cliFilesystemEntry
				firstSyncCompleted := false
				var legacyAfter []byte
				var legacyAfterInfo os.FileInfo
				legacyAfterFirstSync := false

				checkAll := func(phase string, commandIndex int) {

					t.Helper()
					assertStatUnchanged(t, phase+" native", nativePath, nativeBefore, nativeInfo)
					assertStatUnchanged(t, phase+" state", statePath, stateBefore, stateInfo)
					assertStatUnchanged(t, phase+" lock", lockPath, lockBefore, lockInfo)
					assertStatUnchanged(t, phase+" config", configPath, configBefore, configInfo)
					assertCliFilesystemSnapshot(t, phase+" cache", filepath.Join(home, "cache"), cacheTree)
					if twin && !sharedFile {
						data, readErr := os.ReadFile(legacyPath)
						if readErr != nil {
							t.Fatalf("%s removed the legacy file containing the handwritten sibling: %v", phase, readErr)
						}
						rules := cliLegacyHookRules(t, d, data)
						if len(rules) != 1 || string(rules[0]) != siblingRaw || strings.Contains(string(rules[0]), "_managed_by") {
							t.Fatalf("%s legacy file did not keep only the exact handwritten sibling: %s", phase, data)
						}
						if commandIndex == 0 {
							legacyAfter, legacyAfterInfo = statSnapshot(t, legacyPath)
							legacyAfterFirstSync = true
							store, err := ownership.Open(nativeRoot)
							if err != nil {
								t.Fatal(err)
							}
							backup := assertCliLegacySafetyBackup(t, store, legacyRel, legacySeed)
							stateSnapshotAfterFirst = assertCliStateSnapshotAfterLegacyBackup(t, phase, stateRoot, ownerLockPath, stateSnapshotBefore, backup)
							assertCliTreeUnchanged(t, phase+" Native paths", nativeRoot, nativeTreeBefore)
							nativeSnapshotAfterFirst = cliFilesystemSnapshot(t, nativeRoot)
							firstSyncCompleted = true
						} else {
							if !legacyAfterFirstSync || !firstSyncCompleted {
								t.Fatal("Legacy sibling and state trees were not snapshotted after the first sync")
							}
							assertStatUnchanged(t, phase+" legacy sibling", legacyPath, legacyAfter, legacyAfterInfo)
							assertCliFilesystemSnapshotExceptLock(t, phase+" state root", stateRoot, ownerLockPath, stateSnapshotAfterFirst)
							assertCliFilesystemSnapshot(t, phase+" Native root", nativeRoot, nativeSnapshotAfterFirst)
						}

					} else if !sharedFile {
						if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
							t.Fatalf("%s created legacy hook output at %s (err=%v)", phase, legacyPath, statErr)
						}
						if commandIndex == 0 {
							stateSnapshotAfterFirst = assertCliFilesystemSnapshotExceptLock(t, phase+" state root", stateRoot, ownerLockPath, stateSnapshotBefore)
							assertCliFilesystemSnapshot(t, phase+" Native root", nativeRoot, nativeSnapshotBefore)
							nativeSnapshotAfterFirst = cliFilesystemSnapshot(t, nativeRoot)
							firstSyncCompleted = true
						} else {
							if !firstSyncCompleted {
								t.Fatal("state trees were not snapshotted after the first sync")
							}
							assertCliFilesystemSnapshotExceptLock(t, phase+" state root", stateRoot, ownerLockPath, stateSnapshotAfterFirst)
							assertCliFilesystemSnapshot(t, phase+" Native root", nativeRoot, nativeSnapshotAfterFirst)
						}
					} else {
						if commandIndex == 0 {
							stateSnapshotAfterFirst = assertCliFilesystemSnapshotExceptLock(t, phase+" state root", stateRoot, ownerLockPath, stateSnapshotBefore)
							assertCliFilesystemSnapshot(t, phase+" Native root", nativeRoot, nativeSnapshotBefore)
							nativeSnapshotAfterFirst = cliFilesystemSnapshot(t, nativeRoot)
							firstSyncCompleted = true
						} else {
							if !firstSyncCompleted {
								t.Fatal("state trees were not snapshotted after the first sync")
							}
							assertCliFilesystemSnapshotExceptLock(t, phase+" state root", stateRoot, ownerLockPath, stateSnapshotAfterFirst)
							assertCliFilesystemSnapshot(t, phase+" Native root", nativeRoot, nativeSnapshotAfterFirst)
						}
					}
					assertCliSatisfactionUnmanaged(t, phase, nativeRoot, d, fps)
				}

				for i, args := range [][]string{{"sync"}, {"sync", "--locked"}, {"sync", "--check"}} {
					label := strings.Join(args, " ")
					out, err := executeCommand(args...)
					t.Logf("%s output:\n%s", label, out)
					if err != nil {
						t.Fatalf("%s: %v\n%s", label, err, out)
					}
					if strings.Contains(out, "out of sync") || strings.Contains(out, "error") {
						t.Fatalf("%s reports pending or error work under complete satisfaction:\n%s", label, out)
					}
					checkAll(label, i)
				}
			})
		}
	}
}

// spec: hook-native-location-tolerance/satisfaction-checks-leave-all-evidence-unchanged
func TestSyncIncompleteSatisfactionKeepsLegacyBehavior(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, d := range cliNativeDocs() {
		for _, mutating := range [][]string{{"sync"}, {"sync", "--locked"}} {
			label := strings.Join(mutating, " ")
			t.Run(fmt.Sprintf("%s_machine=%v/%s", d.target, d.machineScope, label), func(t *testing.T) {
				home, _, nativeRoot, nativePath, legacyPath, lockPath, configPath, fps := cliSatisfactionFixture(t, d)
				statePath := cliSeedHookSatisfaction(t, nativeRoot, d, d.doc, fps, cliSatisfactionIncomplete)
				ownerLockPath := seedCliOwnershipLock(t, nativeRoot)

				sharedFile := d.target == "windsurf" && d.machineScope
				nativeBefore, nativeInfo := statSnapshot(t, nativePath)
				stateBefore, stateInfo := statSnapshot(t, statePath)
				lockBefore, lockInfo := statSnapshot(t, lockPath)
				configBefore, configInfo := statSnapshot(t, configPath)
				stateRoot := os.Getenv("XDG_STATE_HOME")
				stateSnapshot := cliFilesystemSnapshot(t, stateRoot)
				nativeTree := cliTree(t, nativeRoot)
				cacheRoot := filepath.Join(home, "cache")
				cacheSnapshot := cliFilesystemSnapshot(t, cacheRoot)

				out, err := executeCommand("sync", "--check")
				t.Logf("sync --check output:\n%s", out)
				if err != nil {
					t.Fatalf("sync --check on incomplete satisfaction: %v\n%s", err, out)
				}
				if !sharedFile {
					if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
						t.Fatalf("sync --check created the legacy file: %v", statErr)

					}
				}
				assertStatUnchanged(t, "--check native", nativePath, nativeBefore, nativeInfo)
				assertStatUnchanged(t, "--check state", statePath, stateBefore, stateInfo)
				assertStatUnchanged(t, "--check lock", lockPath, lockBefore, lockInfo)
				assertStatUnchanged(t, "--check config", configPath, configBefore, configInfo)
				assertCliFilesystemSnapshot(t, "--check state root", stateRoot, stateSnapshot)
				assertCliTreeUnchanged(t, "--check native root", nativeRoot, nativeTree)
				assertCliFilesystemSnapshot(t, "--check cache", cacheRoot, cacheSnapshot)

				out, err = executeCommand(mutating...)
				t.Logf("%s output:\n%s", label, out)
				if err != nil {
					t.Fatalf("%s satisfaction refresh: %v\n%s", label, err, out)
				}
				if !sharedFile {
					if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
						t.Fatalf("%s created legacy output: %v", label, statErr)
					}
				}
				assertStatUnchanged(t, label+" native", nativePath, nativeBefore, nativeInfo)
				assertStatUnchanged(t, label+" lock", lockPath, lockBefore, lockInfo)
				assertStatUnchanged(t, label+" config", configPath, configBefore, configInfo)
				stateAfter, stateInfoAfter := statSnapshot(t, statePath)
				if reflect.DeepEqual(stateBefore, stateAfter) || stateInfoAfter.Mode().Perm() != stateInfo.Mode().Perm() {
					t.Fatalf("%s did not refresh the existing satisfaction metadata", label)
				}
				assertCliStateOnlyRecordChanged(t, label, stateRoot, ownerLockPath, statePath, stateSnapshot, cliFilesystemSnapshot(t, stateRoot))
				assertCliTreeUnchanged(t, label+" native root", nativeRoot, nativeTree)
				assertCliFilesystemSnapshot(t, label+" cache", cacheRoot, cacheSnapshot)
				assertCliCompleteHookSatisfaction(t, nativeRoot, d, fps)

				stateSnapshotAfter := cliFilesystemSnapshot(t, stateRoot)
				out, err = executeCommand("sync", "--check")
				if err != nil {
					t.Fatalf("post-refresh sync --check: %v\n%s", err, out)
				}
				if !sharedFile {
					if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
						t.Fatalf("post-refresh sync --check created legacy output: %v", statErr)
					}
				}
				assertStatUnchanged(t, "post-refresh check native", nativePath, nativeBefore, nativeInfo)
				assertStatUnchanged(t, "post-refresh check state", statePath, stateAfter, stateInfoAfter)
				assertStatUnchanged(t, "post-refresh check lock", lockPath, lockBefore, lockInfo)
				assertStatUnchanged(t, "post-refresh check config", configPath, configBefore, configInfo)
				assertCliFilesystemSnapshot(t, "post-refresh check state root", stateRoot, stateSnapshotAfter)
				assertCliTreeUnchanged(t, "post-refresh check native root", nativeRoot, nativeTree)
				assertCliFilesystemSnapshot(t, "post-refresh check cache", cacheRoot, cacheSnapshot)
				assertCliCompleteHookSatisfaction(t, nativeRoot, d, fps)

			})
		}
	}
}

// spec: hook-native-location-tolerance/satisfaction-checks-leave-all-evidence-unchanged
func TestSyncCorruptSatisfactionRefusesWithoutMutation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, d := range cliNativeDocs() {
		t.Run(fmt.Sprintf("%s_machine=%v", d.target, d.machineScope), func(t *testing.T) {
			_, _, nativeRoot, nativePath, legacyPath, lockPath, configPath, fps := cliSatisfactionFixture(t, d)
			statePath := cliSeedHookSatisfaction(t, nativeRoot, d, d.doc, fps, cliSatisfactionCorrupt)
			seedCliOwnershipLock(t, nativeRoot)
			sharedFile := d.target == "windsurf" && d.machineScope
			var legacyBefore []byte
			var legacyInfo os.FileInfo
			var legacyTree []string
			if !sharedFile {
				writeFile(t, legacyPath, cliTwinAndSiblingDoc(d))
				legacyBefore, legacyInfo = statSnapshot(t, legacyPath)
				legacyTree = cliTree(t, filepath.Dir(legacyPath))
			}
			nativeBefore, nativeInfo := statSnapshot(t, nativePath)
			stateBefore, stateInfo := statSnapshot(t, statePath)
			lockBefore, lockInfo := statSnapshot(t, lockPath)
			configBefore, configInfo := statSnapshot(t, configPath)
			stateRoot := os.Getenv("XDG_STATE_HOME")
			stateTree := cliTree(t, stateRoot)
			nativeTree := cliTree(t, nativeRoot)

			for _, args := range [][]string{{"sync"}, {"sync", "--locked"}, {"sync", "--check"}} {
				label := strings.Join(args, " ")
				out, err := executeCommand(args...)
				t.Logf("%s output:\n%s", label, out)
				if err == nil {
					t.Fatalf("%s succeeded on corrupt satisfaction evidence:\n%s", label, out)
				}
				combined := out + err.Error()
				if !strings.Contains(combined, "hook.native-tolerance-refused") && !strings.Contains(combined, "ownership.state-corrupt") {
					t.Fatalf("%s: refusal missing a stable code:\n%s\n%v", label, out, err)
				}
				if strings.Contains(combined, cliSatisfactionSentinel) {
					t.Fatalf("%s: refusal leaked the rejected value:\n%s", label, combined)
				}
				assertStatUnchanged(t, label+" native", nativePath, nativeBefore, nativeInfo)
				assertStatUnchanged(t, label+" state", statePath, stateBefore, stateInfo)
				assertStatUnchanged(t, label+" lock", lockPath, lockBefore, lockInfo)
				assertStatUnchanged(t, label+" config", configPath, configBefore, configInfo)
				assertCliTreeUnchanged(t, label+" state root", stateRoot, stateTree)
				assertCliTreeUnchanged(t, label+" native root", nativeRoot, nativeTree)
				if !sharedFile {
					assertStatUnchanged(t, label+" legacy", legacyPath, legacyBefore, legacyInfo)
					assertCliTreeUnchanged(t, label+" legacy dir", filepath.Dir(legacyPath), legacyTree)
				}
			}
		})
	}
}

// spec: hook-native-location-tolerance/satisfaction-checks-leave-all-evidence-unchanged
func TestSyncSatisfactionCheckDoesNotCreateStateRoot(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, d := range cliNativeDocs() {
		t.Run(fmt.Sprintf("%s_machine=%v", d.target, d.machineScope), func(t *testing.T) {
			homeDir, _, nativeRoot, nativePath, legacyPath, lockPath, configPath, fps := cliSatisfactionFixture(t, d)

			stateHome := filepath.Join(homeDir, "no-state-root")
			t.Setenv("XDG_STATE_HOME", stateHome)
			writeFile(t, nativePath, d.doc)
			store, err := ownership.Open(nativeRoot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Proof(d.rel); !errors.Is(err, ownership.ErrStateMissing) {
				t.Fatalf("readonly proof = %v, want state missing", err)
			}
			nativeBefore, nativeInfo := statSnapshot(t, nativePath)
			lockBefore, lockInfo := statSnapshot(t, lockPath)
			configBefore, configInfo := statSnapshot(t, configPath)
			nativeTree := cliTree(t, nativeRoot)
			stateSnapshot := cliFilesystemSnapshot(t, stateHome)
			cacheRoot := filepath.Join(homeDir, "cache")
			cacheSnapshot := cliFilesystemSnapshot(t, cacheRoot)
			sharedFile := d.target == "windsurf" && d.machineScope

			out, err := executeCommand("sync", "--check")
			t.Logf("sync --check output:\n%s", out)
			if err != nil || strings.Contains(out, "out of sync") {
				t.Fatalf("sync --check did not recognize handwritten Native satisfaction: %v\n%s", err, out)
			}
			if _, statErr := os.Lstat(stateHome); !os.IsNotExist(statErr) {
				t.Fatalf("sync --check created the state root %s (err=%v)", stateHome, statErr)
			}
			if !sharedFile {
				if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
					t.Fatalf("sync --check created the legacy file (err=%v)", statErr)
				}
			}
			assertStatUnchanged(t, "--check native", nativePath, nativeBefore, nativeInfo)
			assertStatUnchanged(t, "--check lock", lockPath, lockBefore, lockInfo)
			assertStatUnchanged(t, "--check config", configPath, configBefore, configInfo)
			assertCliTreeUnchanged(t, "--check native root", nativeRoot, nativeTree)
			assertCliFilesystemSnapshot(t, "--check state root", stateHome, stateSnapshot)
			assertCliFilesystemSnapshot(t, "--check cache", cacheRoot, cacheSnapshot)

			out, err = executeCommand("sync")
			if err != nil {
				t.Fatalf("sync satisfaction control: %v\n%s", err, out)
			}
			if !sharedFile {
				if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
					t.Fatalf("sync created legacy output: %v", statErr)
				}
			}
			assertStatUnchanged(t, "sync native", nativePath, nativeBefore, nativeInfo)
			assertStatUnchanged(t, "sync lock", lockPath, lockBefore, lockInfo)
			assertStatUnchanged(t, "sync config", configPath, configBefore, configInfo)
			assertCliTreeUnchanged(t, "sync native root", nativeRoot, nativeTree)
			store, err = ownership.Open(nativeRoot)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := store.Proof(d.rel)
			if err != nil || proof.ContentDigest() != ownership.Digest(nativeBefore) {
				t.Fatalf("sync satisfaction proof = %v / %q", err, proof.ContentDigest())
			}
			assertCliCompleteHookSatisfaction(t, nativeRoot, d, fps)
			assertCliSatisfactionUnmanaged(t, "sync", nativeRoot, d, fps)
		})
	}
}
