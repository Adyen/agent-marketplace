package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

// cliSharedHookCommand is the literal hook command both selected hook
// resources run.
const cliSharedHookCommand = "echo shared-proof"

// cliHookNames are the two resource names a hooks.json carrying two identical
// PreToolUse command rules generates (<event>-<type>, then -2).
var cliHookNames = []string{"PreToolUse-command", "PreToolUse-command-2"}

// cliNativeDoc is one (target, scope) native file shape for the CLI matrix:
// the target's documented file, its owning root, and the recorded event.
type cliNativeDoc struct {
	target       string
	machineScope bool
	// rootDir is the ownership/native root relative to the scope base.
	rootDir string
	rel     string
	event   string
	rule    string
	doc     string
}

func cliNativeDocs() []cliNativeDoc {
	claudeRule := `{"hooks":[{"command":"` + cliSharedHookCommand + `","type":"command"}],"matcher":"Bash"}`
	cursorRule := `{"command":"` + cliSharedHookCommand + `","matcher":"^Shell$","type":"command"}`
	factoryRule := `{"hooks":[{"command":"` + cliSharedHookCommand + `","type":"command"}],"matcher":"Execute"}`
	windsurfRule := `{"command":"` + cliSharedHookCommand + `"}`
	return []cliNativeDoc{
		{target: "claude", machineScope: false, rootDir: ".claude", rel: "settings.json", event: "PreToolUse", rule: claudeRule,
			doc: `{"hooks":{"PreToolUse":[` + claudeRule + `]}}` + "\n"},
		{target: "claude", machineScope: true, rootDir: ".claude", rel: "settings.json", event: "PreToolUse", rule: claudeRule,
			doc: `{"hooks":{"PreToolUse":[` + claudeRule + `]}}` + "\n"},
		{target: "cursor", machineScope: false, rootDir: ".cursor", rel: "hooks.json", event: "preToolUse", rule: cursorRule,
			doc: `{"version":1,"hooks":{"preToolUse":[` + cursorRule + `]}}` + "\n"},
		{target: "cursor", machineScope: true, rootDir: ".cursor", rel: "hooks.json", event: "preToolUse", rule: cursorRule,
			doc: `{"version":1,"hooks":{"preToolUse":[` + cursorRule + `]}}` + "\n"},
		{target: "factory", machineScope: false, rootDir: ".factory", rel: "hooks.json", event: "PreToolUse", rule: factoryRule,
			doc: `{"PreToolUse":[` + factoryRule + `]}` + "\n"},
		{target: "factory", machineScope: true, rootDir: ".factory", rel: "hooks.json", event: "PreToolUse", rule: factoryRule,
			doc: `{"PreToolUse":[` + factoryRule + `]}` + "\n"},
		{target: "windsurf", machineScope: false, rootDir: ".", rel: ".devin/hooks.json", event: "pre_run_command", rule: windsurfRule,
			doc: `{"hooks":{"pre_run_command":[` + windsurfRule + `]}}` + "\n"},
		// Windsurf machine scope is the shared file itself; keep a handwritten
		// sibling rule to prove it survives.
		{target: "windsurf", machineScope: true, rootDir: filepath.Join(".codeium", "windsurf"), rel: "hooks.json", event: "pre_run_command", rule: windsurfRule,
			doc: `{"hooks":{"post_cascade_response":[{"command":"./hand.sh","show_output":true}],"pre_run_command":[` + windsurfRule + `]}}` + "\n"},
	}
}

// cliLegacyHookPath is where legacy output for (target, scope) would be
// written; for Windsurf machine scope it is the shared file itself.
func cliLegacyHookPath(d cliNativeDoc, repoRoot, home string) string {
	if d.target == "windsurf" {
		if d.machineScope {
			return filepath.Join(home, ".codeium", "windsurf", "hooks.json")
		}
		return filepath.Join(repoRoot, ".windsurf", "hooks.json")
	}
	base := repoRoot
	if d.machineScope {
		base = home
	}
	return filepath.Join(base, "."+d.target, "hooks", "hooks.json")
}

// cliFingerprint computes the version-1 source fingerprint for one of the
// generated hook names under (target, scope).
func cliFingerprint(t *testing.T, origin, working, name, target string, machineScope bool) string {
	t.Helper()
	scope := materialize.HookScopeRepository
	if machineScope {
		scope = materialize.HookScopeMachine
	}
	fp, err := materialize.HookSourceFingerprint(materialize.HookSourceFingerprintInput{
		SourceIdentity: "git:" + config.NormalizeGitURL(origin) + "@main:",
		Name:           name,
		Target:         target,
		Scope:          scope,
		SourceRoot:     working,
		DefinitionRel:  "hooks/hooks.json",
	})
	if err != nil {
		t.Fatalf("source fingerprint %s: %v", name, err)
	}
	return fp
}

// seedSourceEvidenceWire injects one raw {"resource","source_digest"} tuple
// into an existing valid scalar ownership record via plain encoding/json on
// the decoded document, so this file never references the typed evidence API.
func seedSourceEvidenceWire(t *testing.T, statePath, resource, digest string) {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	files, _ := rec["files"].(map[string]any)
	if len(files) != 1 {
		t.Fatalf("record has %d files, want 1", len(files))
	}
	for _, fs := range files {
		entries, _ := fs.(map[string]any)["entries"].([]any)
		if len(entries) != 1 {
			t.Fatalf("record has %d entries, want 1", len(entries))
		}
		entry, _ := entries[0].(map[string]any)
		entry["source_evidence"] = []any{map[string]any{"resource": resource, "source_digest": digest}}
	}
	out, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func statSnapshot(t *testing.T, path string) (data []byte, info os.FileInfo) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b, fi
}

func assertStatUnchanged(t *testing.T, phase, path string, before []byte, info os.FileInfo) {
	t.Helper()
	data, after := statSnapshot(t, path)
	if !bytes.Equal(data, before) || after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
		t.Fatalf("%s changed %s (bytes, mode, or mtime)", phase, path)
	}
}

// Two selected hook resources that share one canonical native handler are
// both satisfied when ownership records valid evidence for each — under sync,
// sync --locked, and sync --check, for every tolerance target in both scopes.
// No legacy output appears; native, state, config, and lock files keep their
// bytes, mode, and mtime.
// spec: hook-native-location-tolerance/several-selected-sources-share-one-clean-native-entry
// spec: hook-native-location-tolerance/read-only-shared-proof-checks-leave-state-untouched
func TestSyncToleratesSharedNativeHookSources(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, d := range cliNativeDocs() {
		t.Run(fmt.Sprintf("%s_machine=%v", d.target, d.machineScope), func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))

			repoRoot := filepath.Join(t.TempDir(), "repo")
			if err := os.MkdirAll(repoRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			fps := cliSharedHookFingerprints(t, repoRoot, homeDir, d)

			base := repoRoot
			if d.machineScope {
				base = homeDir
			}
			nativeRoot := filepath.Join(base, d.rootDir)
			entry := nativeHookProofEntry(t, d.event, d.rule, cliHookNames[0], fps[cliHookNames[0]])
			seedNativeHookProof(t, nativeRoot, d.rel, d.doc, entry)
			store, err := ownership.Open(nativeRoot)
			if err != nil {
				t.Fatal(err)
			}
			statePath := store.StateFile()
			seedSourceEvidenceWire(t, statePath, cliHookNames[1], fps[cliHookNames[1]])

			nativePath := filepath.Join(nativeRoot, filepath.FromSlash(d.rel))
			legacyPath := cliLegacyHookPath(d, repoRoot, homeDir)
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			if d.machineScope {
				configPath = filepath.Join(homeDir, ".agents", "config.yaml")
				lockPath = filepath.Join(homeDir, ".agents", "lock.yaml")
			}

			nativeBefore, nativeInfo := statSnapshot(t, nativePath)
			stateBefore, stateInfo := statSnapshot(t, statePath)
			lockBefore, lockInfo := statSnapshot(t, lockPath)
			configBefore, configInfo := statSnapshot(t, configPath)

			checkAll := func(phase string) {
				t.Helper()
				if d.target == "windsurf" && d.machineScope {
					// The shared file must keep its seeded bytes exactly.
					assertStatUnchanged(t, phase+" shared file", nativePath, nativeBefore, nativeInfo)
				} else if data, readErr := os.ReadFile(legacyPath); readErr == nil {
					t.Fatalf("%s recreated legacy hook output: %s", phase, data)
				}
				assertStatUnchanged(t, phase+" native", nativePath, nativeBefore, nativeInfo)
				assertStatUnchanged(t, phase+" state", statePath, stateBefore, stateInfo)
				assertStatUnchanged(t, phase+" lock", lockPath, lockBefore, lockInfo)
				assertStatUnchanged(t, phase+" config", configPath, configBefore, configInfo)
			}

			out, err := executeCommand("sync")
			if err != nil {
				t.Fatalf("sync: %v\n%s", err, out)
			}
			t.Logf("sync output:\n%s", out)
			if strings.Contains(out, "out of sync") || strings.Contains(out, "error") {
				t.Fatalf("sync output indicates pending or error work:\n%s", out)
			}
			checkAll("sync")

			out, err = executeCommand("sync", "--locked")
			if err != nil {
				t.Fatalf("sync --locked: %v\n%s", err, out)
			}
			t.Logf("sync --locked output:\n%s", out)
			if strings.Contains(out, "error") {
				t.Fatalf("sync --locked output indicates an error:\n%s", out)
			}
			checkAll("sync --locked")

			out, err = executeCommand("sync", "--check")
			if err != nil {
				t.Fatalf("sync --check: %v\n%s", err, out)
			}
			t.Logf("sync --check output:\n%s", out)
			if strings.Contains(out, "out of sync") || strings.Contains(out, "error") {
				t.Fatalf("sync --check reports pending writes:\n%s", out)
			}
			checkAll("sync --check")
		})
	}
}

// cliSharedHookFingerprints runs the registry/lock/list half of the fixture
// and returns each generated hook name's fingerprint for (target, scope).
func cliSharedHookFingerprints(t *testing.T, repoRoot, home string, d cliNativeDoc) map[string]string {
	t.Helper()
	origin, working := createBareRepoWithWorkingClone(t, "hooks.git")
	writeFile(t, filepath.Join(working, ".claude-plugin", "plugin.json"), `{"name":"container","version":"1.0.0"}`)
	rule := `{"matcher":"Bash","hooks":[{"type":"command","command":"` + cliSharedHookCommand + `"}]}`
	writeFile(t, filepath.Join(working, "hooks", "hooks.json"),
		`{"hooks":{"PreToolUse":[`+rule+`,`+rule+`]}}`)
	commitAndPushAll(t, working, "Add shared hooks")

	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	lockArgs := []string{"lock"}
	if d.machineScope {
		configPath = filepath.Join(home, ".agents", "config.yaml")
		lockArgs = []string{"lock", "--scope", "user"}
	}
	writeConfig(t, configPath, "harnesses: ["+d.target+"]\nregistries:\n  - url: "+origin+"\n    ref: main\n    source_harness: claude\n    default: true\n")
	withWorkingDir(t, repoRoot)

	if output, err := executeCommand(lockArgs...); err != nil {
		t.Fatalf("lock: %v\n%s", err, output)
	}
	output, err := executeCommand("list", "--kind", "hook")
	if err != nil {
		t.Fatalf("list hooks: %v\n%s", err, output)
	}
	_, rows := parseTable(t, output)
	found := map[string]bool{}
	for _, row := range rows {
		fields := strings.Split(row, "|")
		if len(fields) > 2 && fields[1] == "hook" {
			found[strings.TrimSpace(fields[2])] = true
		}
	}
	fps := map[string]string{}
	for _, name := range cliHookNames {
		if !found[name] {
			t.Fatalf("expected generated hook names %v, got:\n%s", cliHookNames, output)
		}
		fps[name] = cliFingerprint(t, origin, working, name, d.target, d.machineScope)
	}
	if fps[cliHookNames[0]] == fps[cliHookNames[1]] {
		t.Fatalf("installed names must differ, so the fingerprints must differ: %q", fps[cliHookNames[0]])
	}
	return fps
}

// spec: hook-native-location-tolerance/invalid-additional-evidence-refuses-without-mutation
// spec: hook-native-location-tolerance/read-only-shared-proof-checks-leave-state-untouched
func TestSyncSharedNativeProofRefusesPartialOrInvalidEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	doc := cliNativeDocs()[4] // factory, repository scope

	newFixture := func(t *testing.T) (homeDir, repoRoot, nativePath, legacyPath, statePath, lockPath, configPath string, fps map[string]string) {
		homeDir = t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
		repoRoot = filepath.Join(t.TempDir(), "repo")
		fps = cliSharedHookFingerprints(t, repoRoot, homeDir, doc)
		nativeRoot := filepath.Join(repoRoot, doc.rootDir)
		entry := nativeHookProofEntry(t, doc.event, doc.rule, cliHookNames[0], fps[cliHookNames[0]])
		seedNativeHookProof(t, nativeRoot, doc.rel, doc.doc, entry)
		store, err := ownership.Open(nativeRoot)
		if err != nil {
			t.Fatal(err)
		}
		statePath = store.StateFile()
		nativePath = filepath.Join(nativeRoot, doc.rel)
		legacyPath = filepath.Join(nativeRoot, "hooks", "hooks.json")
		lockPath = filepath.Join(repoRoot, ".agents", "lock.yaml")
		configPath = filepath.Join(repoRoot, ".agents", "config.yaml")
		return
	}
	assertCompleteOwnedSources := func(t *testing.T, repoRoot string, fps map[string]string) {
		t.Helper()
		store, err := ownership.Open(filepath.Join(repoRoot, doc.rootDir))
		if err != nil {
			t.Fatal(err)
		}
		proof, err := store.Proof(doc.rel)
		if err != nil {
			t.Fatal(err)
		}
		ids := proof.OwnedIDs()
		if proof.OwnedCount() != 1 || len(ids) != 1 || len(proof.HookSatisfactions()) != 0 {
			t.Fatalf("refreshed proof = owned %v / satisfactions %v", ids, proof.HookSatisfactions())
		}
		entry, ok := proof.Entry(ids[0])
		if !ok || entry.Resource != cliHookNames[0] || entry.SourceDigest != fps[cliHookNames[0]] || len(entry.SourceEvidence) != 1 ||
			entry.SourceEvidence[0] != (ownership.HookSourceEvidence{Resource: cliHookNames[1], SourceDigest: fps[cliHookNames[1]]}) {
			t.Fatalf("refreshed contributor evidence = %+v", entry)
		}
	}
	assertOneNativeGroup := func(t *testing.T, nativePath string) {
		t.Helper()
		data, err := os.ReadFile(nativePath)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string][]json.RawMessage
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("Native document: %v", err)
		}
		if len(document) != 1 || len(document[doc.event]) != 1 {
			t.Fatalf("Native document = %#v, want exactly one current hook group", document)
		}
	}

	// Healthy control: both contributors proven on the wire, all three
	// commands quiet and nothing written.
	t.Run("healthy_shared_proof", func(t *testing.T) {
		_, _, nativePath, legacyPath, statePath, lockPath, configPath, fps := newFixture(t)
		seedSourceEvidenceWire(t, statePath, cliHookNames[1], fps[cliHookNames[1]])
		nativeBefore, nativeInfo := statSnapshot(t, nativePath)
		stateBefore, stateInfo := statSnapshot(t, statePath)
		lockBefore, lockInfo := statSnapshot(t, lockPath)
		configBefore, configInfo := statSnapshot(t, configPath)
		for _, args := range [][]string{{"sync"}, {"sync", "--locked"}, {"sync", "--check"}} {
			label := strings.Join(args, " ")
			out, err := executeCommand(args...)
			t.Logf("%s output:\n%s", label, out)
			if err != nil {
				t.Fatalf("%s: %v\n%s", label, err, out)
			}
			if data, readErr := os.ReadFile(legacyPath); readErr == nil {
				t.Fatalf("%s wrote legacy output under a complete proof: %s", label, data)
			}
			assertStatUnchanged(t, label+" native", nativePath, nativeBefore, nativeInfo)
			assertStatUnchanged(t, label+" state", statePath, stateBefore, stateInfo)
			assertStatUnchanged(t, label+" lock", lockPath, lockBefore, lockInfo)
			assertStatUnchanged(t, label+" config", configPath, configBefore, configInfo)
		}
	})

	for _, mutating := range [][]string{{"sync"}, {"sync", "--locked"}} {
		label := strings.Join(mutating, " ")
		t.Run("partial_proof_refreshes_native_metadata/"+label, func(t *testing.T) {
			home, repoRoot, nativePath, legacyPath, statePath, lockPath, configPath, fps := newFixture(t)
			nativeRoot := filepath.Join(repoRoot, doc.rootDir)
			store, err := ownership.Open(nativeRoot)
			if err != nil {
				t.Fatal(err)
			}
			ownerLockPath := store.LockPath()
			nativeBefore, nativeInfo := statSnapshot(t, nativePath)
			stateBefore, stateInfo := statSnapshot(t, statePath)
			lockBefore, lockInfo := statSnapshot(t, lockPath)
			configBefore, configInfo := statSnapshot(t, configPath)
			stateRoot := filepath.Join(home, "state")
			stateSnapshot := cliFilesystemSnapshot(t, stateRoot)
			nativeTree := cliTree(t, nativeRoot)
			assertOneNativeGroup(t, nativePath)
			cacheRoot := filepath.Join(home, "cache")
			cacheTree := cliFilesystemSnapshot(t, cacheRoot)

			out, err := executeCommand("sync", "--check")
			t.Logf("sync --check output:\n%s", out)
			if err != nil || strings.Contains(out, "out of sync") {
				t.Fatalf("metadata-only partial proof check: %v\n%s", err, out)
			}
			if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
				t.Fatalf("sync --check created the legacy file: %v", statErr)

			}
			assertStatUnchanged(t, "--check native", nativePath, nativeBefore, nativeInfo)
			assertStatUnchanged(t, "--check state", statePath, stateBefore, stateInfo)
			assertStatUnchanged(t, "--check lock", lockPath, lockBefore, lockInfo)
			assertStatUnchanged(t, "--check config", configPath, configBefore, configInfo)
			assertCliFilesystemSnapshot(t, "--check state root", stateRoot, stateSnapshot)
			assertCliTreeUnchanged(t, "--check native root", nativeRoot, nativeTree)
			assertCliFilesystemSnapshot(t, "--check cache", cacheRoot, cacheTree)

			out, err = executeCommand(mutating...)
			if err != nil {
				t.Fatalf("%s metadata refresh: %v\n%s", label, err, out)
			}
			t.Logf("%s output:\n%s", label, out)
			if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
				t.Fatalf("%s created legacy output: %v", label, statErr)
			}
			assertStatUnchanged(t, label+" native", nativePath, nativeBefore, nativeInfo)
			assertOneNativeGroup(t, nativePath)
			assertStatUnchanged(t, label+" lock", lockPath, lockBefore, lockInfo)
			assertStatUnchanged(t, label+" config", configPath, configBefore, configInfo)
			stateAfter, stateInfoAfter := statSnapshot(t, statePath)
			stateSnapshotAfter := cliFilesystemSnapshot(t, stateRoot)
			if bytes.Equal(stateBefore, stateAfter) || stateInfoAfter.Mode().Perm() != stateInfo.Mode().Perm() {
				t.Fatalf("%s did not update only the existing ownership metadata", label)
			}
			assertCliStateOnlyRecordChanged(t, label, stateRoot, ownerLockPath, statePath, stateSnapshot, stateSnapshotAfter)
			assertCliTreeUnchanged(t, label+" native root", nativeRoot, nativeTree)
			assertCliFilesystemSnapshot(t, label+" cache", cacheRoot, cacheTree)
			assertCompleteOwnedSources(t, repoRoot, fps)

			out, err = executeCommand("sync", "--check")
			if err != nil || strings.Contains(out, "out of sync") {
				t.Fatalf("post-refresh sync --check: %v\n%s", err, out)
			}
			assertStatUnchanged(t, "post-refresh check native", nativePath, nativeBefore, nativeInfo)
			assertOneNativeGroup(t, nativePath)
			assertStatUnchanged(t, "post-refresh check state", statePath, stateAfter, stateInfoAfter)
			assertStatUnchanged(t, "post-refresh check lock", lockPath, lockBefore, lockInfo)
			assertStatUnchanged(t, "post-refresh check config", configPath, configBefore, configInfo)
			assertCliFilesystemSnapshot(t, "post-refresh check state root", stateRoot, stateSnapshotAfter)
			assertCliTreeUnchanged(t, "post-refresh check native root", nativeRoot, nativeTree)
			assertCliFilesystemSnapshot(t, "post-refresh check cache", cacheRoot, cacheTree)
			if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
				t.Fatalf("post-refresh check created legacy output: %v", statErr)
			}
			assertCompleteOwnedSources(t, repoRoot, fps)
		})
	}

	t.Run("native_value_absent_reports_pending_and_repairs", func(t *testing.T) {
		home, repoRoot, nativePath, legacyPath, statePath, lockPath, configPath, fps := newFixture(t)
		nativeRoot := filepath.Join(repoRoot, doc.rootDir)
		if err := os.WriteFile(nativePath, []byte("{\"PreToolUse\":[]}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		nativeBefore, nativeInfo := statSnapshot(t, nativePath)
		stateBefore, stateInfo := statSnapshot(t, statePath)
		lockBefore, lockInfo := statSnapshot(t, lockPath)
		configBefore, configInfo := statSnapshot(t, configPath)
		stateRoot := filepath.Join(home, "state")
		stateSnapshot := cliFilesystemSnapshot(t, stateRoot)
		nativeTree := cliTree(t, nativeRoot)
		cacheRoot := filepath.Join(home, "cache")
		cacheTree := cliFilesystemSnapshot(t, cacheRoot)

		out, err := executeCommand("sync", "--check")
		t.Logf("sync --check output:\n%s", out)
		if err == nil || !strings.Contains(out, "out of sync") && !strings.Contains(out, "missing") {
			t.Fatalf("sync --check did not report the pending Native write: %v\n%s", err, out)
		}
		if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
			t.Fatalf("sync --check created legacy output: %v", statErr)
		}
		assertStatUnchanged(t, "--check native", nativePath, nativeBefore, nativeInfo)
		assertStatUnchanged(t, "--check state", statePath, stateBefore, stateInfo)
		assertStatUnchanged(t, "--check lock", lockPath, lockBefore, lockInfo)
		assertStatUnchanged(t, "--check config", configPath, configBefore, configInfo)
		assertCliFilesystemSnapshot(t, "--check state root", stateRoot, stateSnapshot)
		assertCliTreeUnchanged(t, "--check native root", nativeRoot, nativeTree)
		assertCliFilesystemSnapshot(t, "--check cache", cacheRoot, cacheTree)

		out, err = executeCommand("sync")
		if err != nil {
			t.Fatalf("sync Native repair: %v\n%s", err, out)
		}
		rules := parseLegacyHooks(t, nativePath)
		if len(rules) != 1 {
			t.Fatalf("repaired Native rules = %#v, want exactly one current rule", rules)
		}
		var expected map[string]any
		if err := json.Unmarshal([]byte(doc.rule), &expected); err != nil {
			t.Fatal(err)
		}
		actualBytes, err := json.Marshal(rules[0])
		if err != nil {
			t.Fatal(err)
		}
		expectedBytes, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actualBytes, expectedBytes) {
			t.Fatalf("repaired Native rule = %s, want %s", actualBytes, expectedBytes)
		}
		if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
			t.Fatalf("sync created legacy output: %v", statErr)
		}
		stateAfter, stateInfoAfter := statSnapshot(t, statePath)
		if bytes.Equal(stateBefore, stateAfter) || stateInfoAfter.Mode().Perm() != stateInfo.Mode().Perm() {
			t.Fatal("sync did not update the existing ownership evidence")
		}
		_, nativeInfoAfter := statSnapshot(t, nativePath)
		if nativeInfoAfter.Mode().Perm() != nativeInfo.Mode().Perm() {
			t.Fatalf("Native permissions changed: %v -> %v", nativeInfo.Mode(), nativeInfoAfter.Mode())
		}
		assertStatUnchanged(t, "sync lock", lockPath, lockBefore, lockInfo)
		assertStatUnchanged(t, "sync config", configPath, configBefore, configInfo)
		assertCliTreeUnchanged(t, "sync native root", nativeRoot, nativeTree)
		assertCliFilesystemSnapshot(t, "sync cache", cacheRoot, cacheTree)
		assertCompleteOwnedSources(t, repoRoot, fps)

		stateSnapshotAfter := cliFilesystemSnapshot(t, stateRoot)
		nativeAfter, nativeInfoAfter := statSnapshot(t, nativePath)
		out, err = executeCommand("sync", "--check")
		if err != nil || strings.Contains(out, "out of sync") {
			t.Fatalf("post-repair sync --check: %v\n%s", err, out)
		}
		assertStatUnchanged(t, "post-repair check native", nativePath, nativeAfter, nativeInfoAfter)
		assertStatUnchanged(t, "post-repair check state", statePath, stateAfter, stateInfoAfter)
		assertStatUnchanged(t, "post-repair check lock", lockPath, lockBefore, lockInfo)
		assertStatUnchanged(t, "post-repair check config", configPath, configBefore, configInfo)
		assertCliFilesystemSnapshot(t, "post-repair check state root", stateRoot, stateSnapshotAfter)
		assertCliTreeUnchanged(t, "post-repair check native root", nativeRoot, nativeTree)
		assertCliFilesystemSnapshot(t, "post-repair check cache", cacheRoot, cacheTree)
		if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
			t.Fatalf("post-repair check created legacy output: %v", statErr)
		}
		assertCompleteOwnedSources(t, repoRoot, fps)
	})

	// Invalid evidence: corrupt the durable record on the wire; every command
	// fails closed with the stable tolerance code and changes nothing.
	t.Run("invalid_evidence_refuses_without_mutation", func(t *testing.T) {
		_, _, nativePath, legacyPath, statePath, lockPath, configPath, fps := newFixture(t)
		seedSourceEvidenceWire(t, statePath, cliHookNames[1], fps[cliHookNames[1]])
		stateData, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		sentinel := "rm -rf /sentinel-payload"
		corrupt := strings.Replace(string(stateData), `"source_digest"`,
			`"source_evidence":[{"resource":`+fmt.Sprintf("%q", sentinel)+`,"source_digest":"sha256:zz"}],"source_digest"`, 1)
		if corrupt == string(stateData) {
			t.Fatal("corruption did not land")
		}
		if err := os.WriteFile(statePath, []byte(corrupt), 0o600); err != nil {
			t.Fatal(err)
		}

		nativeBefore, nativeInfo := statSnapshot(t, nativePath)
		stateBefore, stateInfo := statSnapshot(t, statePath)
		lockBefore, lockInfo := statSnapshot(t, lockPath)
		configBefore, configInfo := statSnapshot(t, configPath)

		for _, args := range [][]string{{"sync"}, {"sync", "--locked"}, {"sync", "--check"}} {
			label := strings.Join(args, " ")
			out, err := executeCommand(args...)
			t.Logf("%s output:\n%s", label, out)
			if err == nil {
				t.Fatalf("%s succeeded on corrupt shared evidence:\n%s", label, out)
			}
			combined := out + err.Error()
			if !strings.Contains(combined, "hook.native-tolerance-refused") && !strings.Contains(combined, "ownership.state-corrupt") {
				t.Fatalf("%s: refusal missing a stable code:\n%s\n%v", label, out, err)
			}
			if strings.Contains(combined, sentinel) {
				t.Fatalf("%s: refusal leaked the rejected value:\n%s", label, combined)
			}
			if data, readErr := os.ReadFile(legacyPath); readErr == nil {
				t.Fatalf("%s wrote legacy output under corrupt evidence: %s", label, data)
			}
			assertStatUnchanged(t, label+" native", nativePath, nativeBefore, nativeInfo)
			assertStatUnchanged(t, label+" state", statePath, stateBefore, stateInfo)
			assertStatUnchanged(t, label+" lock", lockPath, lockBefore, lockInfo)
			assertStatUnchanged(t, label+" config", configPath, configBefore, configInfo)
		}
	})
}

// parseLegacyHooks flattens a legacy hooks.json fragment into its rule
// objects across all events.
func parseLegacyHooks(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("legacy file: %v", err)
	}
	var doc map[string][]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("legacy file is not a hook event map: %v", err)
	}
	var out []map[string]any
	for event, raws := range doc {
		if event != "PreToolUse" {
			t.Fatalf("unexpected legacy event %q", event)
		}
		for _, raw := range raws {
			var rule map[string]any
			if err := json.Unmarshal(raw, &rule); err != nil {
				t.Fatalf("legacy rule is not an object: %v", err)
			}
			out = append(out, rule)
		}
	}
	return out
}
