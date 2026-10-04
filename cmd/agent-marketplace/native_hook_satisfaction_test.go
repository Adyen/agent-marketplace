package main

import (
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

func assertCliSingleLegacyHandler(t *testing.T, d cliNativeDoc, legacyPath string) {
	t.Helper()
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("legacy file: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("legacy file is not JSON: %v", err)
	}
	container := doc
	if d.target != "factory" {
		nested, ok := doc["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("legacy file missing hooks member: %s", data)
		}
		container = nested
	}
	event := "PreToolUse"
	if d.target == "windsurf" {
		event = "pre_run_command"
	}
	if len(container) != 1 {
		t.Fatalf("legacy file carries %d events, want only %s: %s", len(container), event, data)
	}
	raws, ok := container[event].([]any)
	if !ok || len(raws) != 1 {
		t.Fatalf("legacy event %s = %#v, want exactly one handler", event, container[event])
	}
	rule, ok := raws[0].(map[string]any)
	if !ok {
		t.Fatalf("legacy handler is not an object: %#v", raws[0])
	}
	want := map[string]any{"_managed_by": "agent-marketplace"}
	switch d.target {
	case "claude":
		want["command"] = cliSharedHookCommand
		want["matcher"] = "Bash"
		want["type"] = "command"
	case "cursor":
		want["command"] = cliSharedHookCommand
		want["event"] = "PreToolUse"
		want["matcher"] = "Bash"
		want["type"] = "command"
	case "factory":
		want["matcher"] = "Bash"
		want["hooks"] = []any{map[string]any{"command": cliSharedHookCommand, "type": "command"}}
	default:
		want["command"] = cliSharedHookCommand
	}
	if !reflect.DeepEqual(rule, want) {
		t.Fatalf("legacy handler = %#v, want exactly %#v", rule, want)
	}
}

// spec: hook-native-location-tolerance/handwritten-native-satisfaction-prevents-legacy-recreation
// spec: hook-native-location-tolerance/satisfaction-checks-leave-all-evidence-unchanged
func TestSyncRecognizesUnownedNativeSatisfaction(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, d := range cliNativeDocs() {
		for _, twin := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_machine=%v_twin=%v", d.target, d.machineScope, twin), func(t *testing.T) {
				_, _, nativeRoot, nativePath, legacyPath, lockPath, configPath, fps := cliSatisfactionFixture(t, d)
				doc := d.doc
				sharedFile := d.target == "windsurf" && d.machineScope
				var legacyBefore []byte
				var legacyInfo os.FileInfo
				if twin {
					writeFile(t, nativePath, doc)
					out, err := executeCommand("sync")
					if err != nil {
						t.Fatalf("twin-seeding sync: %v\n%s", err, out)
					}
					if sharedFile {
						current, readErr := os.ReadFile(nativePath)
						if readErr != nil {
							t.Fatal(readErr)
						}
						doc = string(current)
					} else {
						legacyBefore, legacyInfo = statSnapshot(t, legacyPath)
					}
				}
				statePath := cliSeedHookSatisfaction(t, nativeRoot, d, doc, fps, cliSatisfactionComplete)
				nativeBefore, nativeInfo := statSnapshot(t, nativePath)
				stateBefore, stateInfo := statSnapshot(t, statePath)
				lockBefore, lockInfo := statSnapshot(t, lockPath)
				configBefore, configInfo := statSnapshot(t, configPath)
				stateRoot := os.Getenv("XDG_STATE_HOME")
				stateTree := cliTree(t, stateRoot)
				nativeTree := cliTree(t, nativeRoot)

				checkAll := func(phase string) {
					t.Helper()
					assertStatUnchanged(t, phase+" native", nativePath, nativeBefore, nativeInfo)
					assertStatUnchanged(t, phase+" state", statePath, stateBefore, stateInfo)
					assertStatUnchanged(t, phase+" lock", lockPath, lockBefore, lockInfo)
					assertStatUnchanged(t, phase+" config", configPath, configBefore, configInfo)
					assertCliTreeUnchanged(t, phase+" state root", stateRoot, stateTree)
					assertCliTreeUnchanged(t, phase+" native root", nativeRoot, nativeTree)
					if twin && !sharedFile {
						assertStatUnchanged(t, phase+" legacy twin", legacyPath, legacyBefore, legacyInfo)
					} else if !sharedFile {
						if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
							t.Fatalf("%s created legacy hook output at %s (err=%v)", phase, legacyPath, statErr)
						}
					}
					assertCliSatisfactionUnmanaged(t, phase, nativeRoot, d, fps)
				}

				for _, args := range [][]string{{"sync"}, {"sync", "--locked"}, {"sync", "--check"}} {
					label := strings.Join(args, " ")
					out, err := executeCommand(args...)
					t.Logf("%s output:\n%s", label, out)
					if err != nil {
						t.Fatalf("%s: %v\n%s", label, err, out)
					}
					if strings.Contains(out, "out of sync") || strings.Contains(out, "error") {
						t.Fatalf("%s reports pending or error work under complete satisfaction:\n%s", label, out)
					}
					checkAll(label)
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
				_, _, nativeRoot, nativePath, legacyPath, lockPath, configPath, fps := cliSatisfactionFixture(t, d)
				statePath := cliSeedHookSatisfaction(t, nativeRoot, d, d.doc, fps, cliSatisfactionIncomplete)
				sharedFile := d.target == "windsurf" && d.machineScope
				nativeBefore, nativeInfo := statSnapshot(t, nativePath)
				stateBefore, stateInfo := statSnapshot(t, statePath)
				lockBefore, lockInfo := statSnapshot(t, lockPath)
				configBefore, configInfo := statSnapshot(t, configPath)
				stateRoot := os.Getenv("XDG_STATE_HOME")
				stateTree := cliTree(t, stateRoot)
				nativeTree := cliTree(t, nativeRoot)

				out, err := executeCommand("sync", "--check")
				t.Logf("sync --check output:\n%s", out)
				if err == nil {
					t.Fatalf("sync --check succeeded despite the unproven hook:\n%s", out)
				}
				if !strings.Contains(out, "out of sync") && !strings.Contains(out, "missing") {
					t.Fatalf("sync --check does not report the pending legacy write:\n%s", out)
				}
				if !sharedFile {
					if _, statErr := os.Lstat(legacyPath); !os.IsNotExist(statErr) {
						t.Fatalf("sync --check created the legacy file (err=%v)", statErr)
					}
				}
				assertStatUnchanged(t, "--check native", nativePath, nativeBefore, nativeInfo)
				assertStatUnchanged(t, "--check state", statePath, stateBefore, stateInfo)
				assertStatUnchanged(t, "--check lock", lockPath, lockBefore, lockInfo)
				assertStatUnchanged(t, "--check config", configPath, configBefore, configInfo)
				assertCliTreeUnchanged(t, "--check state root", stateRoot, stateTree)
				assertCliTreeUnchanged(t, "--check native root", nativeRoot, nativeTree)

				var handwrittenRaw json.RawMessage
				if sharedFile {
					var beforeDoc struct {
						Hooks map[string][]json.RawMessage `json:"hooks"`
					}
					if err := json.Unmarshal(nativeBefore, &beforeDoc); err != nil {
						t.Fatalf("seeded shared file: %v", err)
					}
					for _, raw := range beforeDoc.Hooks["pre_run_command"] {
						var obj map[string]any
						if err := json.Unmarshal(raw, &obj); err != nil {
							t.Fatalf("seeded shared handler: %v", err)
						}
						if _, marked := obj["_managed_by"]; !marked {
							handwrittenRaw = raw
						}
					}
					if len(handwrittenRaw) == 0 {
						t.Fatal("seeded shared file has no unmarked pre_run_command entry")
					}
				}

				out, err = executeCommand(mutating...)
				t.Logf("%s output:\n%s", label, out)
				if err != nil {
					t.Fatalf("%s: %v\n%s", label, err, out)
				}
				if sharedFile {
					data, readErr := os.ReadFile(nativePath)
					if readErr != nil {
						t.Fatal(readErr)
					}
					var shared struct {
						Hooks map[string][]json.RawMessage `json:"hooks"`
					}
					if err := json.Unmarshal(data, &shared); err != nil {
						t.Fatalf("shared file: %v", err)
					}
					pre := shared.Hooks["pre_run_command"]
					if len(pre) != 2 {
						t.Fatalf("shared pre_run_command = %s, want handwritten rule then one managed twin", pre)
					}
					var first, second, post map[string]any
					if err := json.Unmarshal(pre[0], &first); err != nil {
						t.Fatalf("shared first handler: %v", err)
					}
					if err := json.Unmarshal(pre[1], &second); err != nil {
						t.Fatalf("shared second handler: %v", err)
					}
					if !reflect.DeepEqual(first, map[string]any{"command": cliSharedHookCommand}) {
						t.Fatalf("shared handwritten handler = %#v, want exactly {command:%q}", first, cliSharedHookCommand)
					}
					if !reflect.DeepEqual(second, map[string]any{"_managed_by": "agent-marketplace", "command": cliSharedHookCommand}) {
						t.Fatalf("shared managed twin = %#v, want exactly {_managed_by:agent-marketplace,command:%q}", second, cliSharedHookCommand)
					}
					if string(pre[0]) != string(handwrittenRaw) {
						t.Fatalf("shared handwritten bytes changed: %s -> %s", handwrittenRaw, pre[0])
					}
					posts := shared.Hooks["post_cascade_response"]
					if len(posts) != 1 {
						t.Fatalf("shared post_cascade_response = %s, want the single unrelated sibling", posts)
					}
					if err := json.Unmarshal(posts[0], &post); err != nil {
						t.Fatalf("shared sibling handler: %v", err)
					}
					if !reflect.DeepEqual(post, map[string]any{"command": "./hand.sh", "show_output": true}) {
						t.Fatalf("unrelated shared handler = %#v, want exactly {command:./hand.sh,show_output:true}", post)
					}
				} else {
					assertCliSingleLegacyHandler(t, d, legacyPath)
					assertStatUnchanged(t, label+" native", nativePath, nativeBefore, nativeInfo)
				}
				assertStatUnchanged(t, label+" state", statePath, stateBefore, stateInfo)
				assertStatUnchanged(t, label+" lock", lockPath, lockBefore, lockInfo)
				assertStatUnchanged(t, label+" config", configPath, configBefore, configInfo)
				assertCliSatisfactionUnmanaged(t, label, nativeRoot, d, fps)
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
			homeDir, _, nativeRoot, nativePath, legacyPath, lockPath, configPath, _ := cliSatisfactionFixture(t, d)
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
			sharedFile := d.target == "windsurf" && d.machineScope

			out, err := executeCommand("sync", "--check")
			t.Logf("sync --check output:\n%s", out)
			if err == nil {
				t.Fatalf("sync --check succeeded without evidence for two selected hooks:\n%s", out)
			}
			if !strings.Contains(out, "out of sync") && !strings.Contains(out, "missing") {
				t.Fatalf("sync --check does not report the pending legacy write:\n%s", out)
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
		})
	}
}
