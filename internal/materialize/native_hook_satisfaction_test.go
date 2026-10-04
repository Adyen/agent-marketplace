package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const rejectedSatisfactionSentinel = "/tmp/rejected-hook --token=not-a-real-secret"
const preservedLegacySentinel = "echo preserved-handwritten-sentinel"

func nativeHookSatisfaction(t *testing.T, event, rule string, sources []ownership.HookSourceEvidence) ownership.HookSatisfaction {
	t.Helper()
	entry := nativeHookProofEntry(t, event, rule, sources[0].Resource, sources[0].SourceDigest)
	return ownership.HookSatisfaction{
		Container:      entry.Container,
		IdentityDigest: entry.IdentityDigest,
		ValueDigest:    entry.ValueDigest,
		Sources:        append([]ownership.HookSourceEvidence(nil), sources...),
	}
}

func writeNativeSatisfactionState(t *testing.T, nativeRoot string, layout nativeHookLayout, recordedNative []byte, entries []ownership.Entry, satisfactions []ownership.HookSatisfaction) *ownership.Store {
	t.Helper()
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatalf("open native ownership store: %v", err)
	}
	if entries == nil {
		entries = []ownership.Entry{}
	}
	if satisfactions == nil {
		satisfactions = []ownership.HookSatisfaction{}
	}
	record := ownership.Record{
		Version:       1,
		CanonicalRoot: store.CanonicalRoot(),
		RootKey:       store.RootKey(),
		Generation:    1,
		UpdatedAt:     "2026-10-03T00:00:00Z",
		Files: map[string]ownership.FileState{
			layout.rel: {
				ContentDigest:     ownership.Digest(recordedNative),
				Entries:           entries,
				HookSatisfactions: satisfactions,
			},
		},
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("marshal native state fixture: %v", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(store.StateFile()), 0o700); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	if err := os.WriteFile(store.StateFile(), data, 0o600); err != nil {
		t.Fatalf("write native state fixture: %v", err)
	}
	return store
}

func namedHookPairFixture(t *testing.T, source config.Source, target hcc.Target, name string) ([]resolver.ResolvedArtifact, string) {
	t.Helper()
	pre, sourceRoot := namedHookFixture(t, source, target, name)
	definition := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + sharedSourceCommand + `"}]}],"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + sharedSourceCommand + `"}]}]}}`
	if err := os.WriteFile(pre.Path, []byte(definition), 0o644); err != nil {
		t.Fatalf("write paired hook definition: %v", err)
	}
	post := pre
	post.Resource = ir.NewHookWithMatcher(name, "PostToolUse", "Bash", sharedSourceCommand, "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	return []resolver.ResolvedArtifact{pre, post}, sourceRoot
}

func assertNoMaterializeErrors(t *testing.T, report *Report) {
	t.Helper()
	if report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("unexpected materialization errors: %+v", report)
	}
}

func assertNoMaterializeChanges(t *testing.T, report *Report) {
	t.Helper()
	for _, root := range report.PerRoot {
		if len(root.Written) != 0 || len(root.Removed) != 0 {
			t.Fatalf("unexpected materialization changes: %+v", root)
		}
	}
}

func assertNativeSatisfactionUnmanaged(t *testing.T, store *ownership.Store, root string, target hcc.Target, machine bool, id ownership.EntryID) {
	t.Helper()
	entries, err := InspectNativeHooks(store, root, target, machine)
	if err != nil {
		t.Fatalf("InspectNativeHooks: %v", err)
	}
	for _, entry := range entries {
		if entry.ID == id {
			if entry.Status != ownership.StatusUnmanaged || entry.SourceDigest != "" {
				t.Fatalf("satisfaction-only native entry = %+v, want unmanaged without a source digest", entry)
			}
			return
		}
	}
	t.Fatalf("native inspector did not return satisfaction identity %+v: %+v", id, entries)
}

func assertFactoryLegacyHooks(t *testing.T, path, command string, expected map[string]int) {
	t.Helper()
	rules := legacyHookRules(t, path)
	for event, count := range expected {
		if len(rules[event]) != count {
			t.Fatalf("legacy event %s has %d handlers, want %d: %+v", event, len(rules[event]), count, rules)
		}
	}
	for event, handlers := range rules {
		if len(handlers) == 0 {
			continue
		}
		if _, ok := expected[event]; !ok {
			t.Fatalf("unexpected legacy event %s: %+v", event, rules)
		}
		for _, handler := range handlers {
			if handler[managedByField] != "agent-marketplace" || handler["matcher"] != "Execute" {
				t.Fatalf("legacy handler = %+v, want marked Execute handler", handler)
			}
			hooks, ok := handler["hooks"].([]any)
			if !ok || len(hooks) != 1 {
				t.Fatalf("legacy handler hooks = %#v", handler["hooks"])
			}
			hook, ok := hooks[0].(map[string]any)
			if !ok || hook["type"] != "command" || hook["command"] != command {
				t.Fatalf("legacy command hook = %#v, want %q", hooks[0], command)
			}
		}
	}
}

func assertWindsurfMachineHooks(t *testing.T, path string, managedLegacy bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Windsurf shared file: %v", err)
	}
	var document struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode Windsurf shared file: %v", err)
	}
	pre := document.Hooks["pre_run_command"]
	wantCount := 1
	wantManaged := 0
	if managedLegacy {
		wantCount = 2
		wantManaged = 1
	}
	if len(pre) != wantCount {
		t.Fatalf("Windsurf pre_run_command handlers = %#v, want %d", pre, wantCount)
	}
	var managed, handwritten int
	for _, handler := range pre {
		if handler["command"] != sharedSourceCommand {
			t.Fatalf("Windsurf handler = %#v, want the shared native command", handler)
		}
		if handler[managedByField] == "agent-marketplace" {
			managed++
		} else {
			handwritten++
		}
	}
	if managed != wantManaged || handwritten != 1 {
		t.Fatalf("Windsurf managed/handwritten handlers = %d/%d, want %d/1", managed, handwritten, wantManaged)
	}
	post := document.Hooks["post_cascade_response"]
	if len(post) != 1 || post[0]["command"] != "./hand.sh" || post[0]["show_output"] != true {
		t.Fatalf("Windsurf unrelated handler changed: %#v", post)
	}
}

func satisfactionPathTree(t *testing.T, root string) []string {
	t.Helper()
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		t.Fatalf("stat tree %s: %v", root, err)
	}
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
		t.Fatalf("walk tree %s: %v", root, err)
	}
	sort.Strings(paths)
	return paths
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
		t.Fatalf("unexpected path %s (err=%v)", path, err)
	}
}

func captureNativeSatisfactionLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

// spec: hook-native-location-tolerance/handwritten-native-satisfaction-prevents-legacy-recreation
func TestNativeSatisfactionPreventsLegacyRecreation(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, tc := range allNativeHookLayouts() {
		t.Run(fmt.Sprintf("%s_machine=%t", tc.target, tc.machineScope), func(t *testing.T) {
			repoRoot := t.TempDir()
			source := config.RepoCommitted
			scope := HookScopeRepository
			if tc.machineScope {
				source = config.UserPersonal
				scope = HookScopeMachine
			}
			resolved, sourceRoot := namedHookFixture(t, source, tc.target, "alpha")
			layout, ok := nativeHookLayoutFor(tc.target, tc.machineScope)
			if !ok {
				t.Fatalf("native layout missing for %s", tc.target)
			}
			nativeRoot, ok := nativeHookRootFor(tc.target, tc.machineScope, repoRoot, home)
			if !ok {
				t.Fatalf("native root missing for %s", tc.target)
			}
			seed := sharedNativeSeed(t, tc.target, tc.machineScope)
			nativeBytes := []byte(seed.doc)
			nativePath := writeNativeFile(t, nativeRoot, layout, nativeBytes)
			fingerprint := namedFingerprint(t, sourceRoot, resolved.Name, string(tc.target), scope)
			satisfaction := nativeHookSatisfaction(t, seed.event, seed.rule, []ownership.HookSourceEvidence{{Resource: resolved.Name, SourceDigest: fingerprint}})
			store := writeNativeSatisfactionState(t, nativeRoot, layout, nativeBytes, []ownership.Entry{}, []ownership.HookSatisfaction{satisfaction})
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())

			proof, err := store.Proof(layout.rel)
			if err != nil {
				t.Fatalf("Proof: %v", err)
			}
			if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 {
				t.Fatalf("satisfaction proof became owned: %d/%v", proof.OwnedCount(), proof.OwnedIDs())
			}
			if records := proof.HookSatisfactions(); len(records) != 1 || records[0].ID() != satisfaction.ID() {
				t.Fatalf("satisfaction proof = %+v", records)
			}
			assertNativeSatisfactionUnmanaged(t, store, nativeRoot, tc.target, tc.machineScope, satisfaction.ID())

			report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
			if err != nil {
				t.Fatalf("sync: %v", err)
			}
			assertNoMaterializeErrors(t, report)
			legacyPath := legacyHookFileFor(tc.target, tc.machineScope, repoRoot, home)
			if tc.target == hcc.TargetWindsurf && tc.machineScope {
				if legacyPath != nativePath {
					t.Fatalf("Windsurf shared path = %q, native path = %q", legacyPath, nativePath)
				}
				assertWindsurfMachineHooks(t, nativePath, false)
			} else {
				assertPathAbsent(t, legacyPath)
			}
			assertNoMaterializeChanges(t, report)
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)

			dryReport, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}, WithDryRun())
			if err != nil {
				t.Fatalf("sync --check: %v", err)
			}
			if !dryReport.InSync() {
				t.Fatalf("sync --check reports pending legacy writes: %+v", dryReport.PerRoot)
			}
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
		})
	}

	t.Run("no_satisfaction_factory", func(t *testing.T) {
		runNativeSatisfactionNoEvidence(t, config.RepoCommitted, hcc.TargetFactory, false, home)
	})
	t.Run("no_satisfaction_windsurf_machine", func(t *testing.T) {
		runNativeSatisfactionNoEvidence(t, config.UserPersonal, hcc.TargetWindsurf, true, home)
	})
	t.Run("marked_legacy_twin", func(t *testing.T) {
		repoRoot := t.TempDir()
		resolved, sourceRoot := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "alpha")
		layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
		nativeRoot := filepath.Join(repoRoot, ".factory")
		seed := sharedNativeSeed(t, hcc.TargetFactory, false)
		nativeBytes := []byte(seed.doc)
		nativePath := writeNativeFile(t, nativeRoot, layout, nativeBytes)
		if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}); err != nil {
			t.Fatalf("seed legacy twin with normal sync: %v", err)
		}
		legacyPath := legacyHookFileFor(hcc.TargetFactory, false, repoRoot, home)
		assertFactoryLegacyHooks(t, legacyPath, sharedSourceCommand, map[string]int{"PreToolUse": 1})
		legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyPath)

		fingerprint := namedFingerprint(t, sourceRoot, resolved.Name, string(hcc.TargetFactory), HookScopeRepository)
		satisfaction := nativeHookSatisfaction(t, seed.event, seed.rule, []ownership.HookSourceEvidence{{Resource: resolved.Name, SourceDigest: fingerprint}})
		store := writeNativeSatisfactionState(t, nativeRoot, layout, nativeBytes, []ownership.Entry{}, []ownership.HookSatisfaction{satisfaction})
		nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
		stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
		report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
		if err != nil {
			t.Fatalf("sync with complete native satisfaction: %v", err)
		}
		assertNoMaterializeErrors(t, report)
		assertNoMaterializeChanges(t, report)
		assertFileUnchanged(t, legacyPath, legacyBefore, legacyMode, legacyMtime)
		assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
		assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	})
}

func runNativeSatisfactionNoEvidence(t *testing.T, source config.Source, target hcc.Target, machine bool, home string) {
	t.Helper()
	repoRoot := t.TempDir()
	scope := HookScopeRepository
	if machine {
		scope = HookScopeMachine
	}
	resolved, sourceRoot := namedHookFixture(t, source, target, "alpha")
	layout, _ := nativeHookLayoutFor(target, machine)
	nativeRoot, _ := nativeHookRootFor(target, machine, repoRoot, home)
	seed := sharedNativeSeed(t, target, machine)
	nativeBytes := []byte(seed.doc)
	nativePath := writeNativeFile(t, nativeRoot, layout, nativeBytes)
	store := writeNativeSatisfactionState(t, nativeRoot, layout, nativeBytes, []ownership.Entry{}, []ownership.HookSatisfaction{})
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatalf("sync without satisfaction evidence: %v", err)
	}
	assertNoMaterializeErrors(t, report)
	legacyPath := legacyHookFileFor(target, machine, repoRoot, home)
	if target == hcc.TargetWindsurf && machine {
		assertWindsurfMachineHooks(t, legacyPath, true)
	} else {
		assertFactoryLegacyHooks(t, legacyPath, sharedSourceCommand, map[string]int{"PreToolUse": 1})
		assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
	}
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	fingerprint := namedFingerprint(t, sourceRoot, resolved.Name, string(target), scope)
	entry := nativeHookProofEntry(t, seed.event, seed.rule, resolved.Name, fingerprint)
	assertNativeSatisfactionUnmanaged(t, store, nativeRoot, target, machine, entry.ID())
}

// spec: hook-native-location-tolerance/satisfaction-proof-matches-every-contributor-exactly
func TestNativeSatisfactionMatchesEveryContributorExactly(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	home := t.TempDir()
	t.Setenv("HOME", home)
	alpha, alphaRoot := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "alpha")
	beta, betaRoot := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "beta")
	seed := sharedNativeSeed(t, hcc.TargetFactory, false)
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	fingerprintAlpha := namedFingerprint(t, alphaRoot, "alpha", string(hcc.TargetFactory), HookScopeRepository)
	fingerprintBeta := namedFingerprint(t, betaRoot, "beta", string(hcc.TargetFactory), HookScopeRepository)

	t.Run("complete_exact_pairs", func(t *testing.T) {
		repoRoot := t.TempDir()
		nativeRoot := filepath.Join(repoRoot, ".factory")
		nativeBytes := []byte(seed.doc)
		nativePath := writeNativeFile(t, nativeRoot, layout, nativeBytes)
		satisfaction := nativeHookSatisfaction(t, seed.event, seed.rule, []ownership.HookSourceEvidence{
			{Resource: "alpha", SourceDigest: fingerprintAlpha},
			{Resource: "beta", SourceDigest: fingerprintBeta},
		})
		store := writeNativeSatisfactionState(t, nativeRoot, layout, nativeBytes, []ownership.Entry{}, []ownership.HookSatisfaction{satisfaction})
		nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
		stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
		report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{alpha, beta})
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		assertNoMaterializeErrors(t, report)
		assertPathAbsent(t, legacyHookFileFor(hcc.TargetFactory, false, repoRoot, home))
		assertNoMaterializeChanges(t, report)
		assertNativeSatisfactionUnmanaged(t, store, nativeRoot, hcc.TargetFactory, false, satisfaction.ID())
		assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
		assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	})

	wrongTarget := namedFingerprint(t, alphaRoot, "alpha", string(hcc.TargetCursor), HookScopeRepository)
	wrongScope := namedFingerprint(t, alphaRoot, "alpha", string(hcc.TargetFactory), HookScopeMachine)
	cases := []struct {
		name     string
		selected []resolver.ResolvedArtifact
		event    string
		rule     string
		sources  []ownership.HookSourceEvidence
		want     map[string]int
	}{
		{
			name:     "crossed_resource_fingerprint_pairs",
			selected: []resolver.ResolvedArtifact{alpha, beta},
			event:    seed.event,
			rule:     seed.rule,
			sources: []ownership.HookSourceEvidence{
				{Resource: "alpha", SourceDigest: fingerprintBeta},
				{Resource: "beta", SourceDigest: fingerprintAlpha},
			},
			want: map[string]int{"PreToolUse": 2},
		},
		{
			name:     "unrecorded_beta_resource",
			selected: []resolver.ResolvedArtifact{beta},
			event:    seed.event,
			rule:     seed.rule,
			sources:  []ownership.HookSourceEvidence{{Resource: "alpha", SourceDigest: fingerprintAlpha}},
			want:     map[string]int{"PreToolUse": 1},
		},
		{
			name:     "wrong_fingerprint",
			selected: []resolver.ResolvedArtifact{alpha},
			event:    seed.event,
			rule:     seed.rule,
			sources:  []ownership.HookSourceEvidence{{Resource: "alpha", SourceDigest: fingerprintBeta}},
			want:     map[string]int{"PreToolUse": 1},
		},
		{
			name:     "wrong_resource",
			selected: []resolver.ResolvedArtifact{alpha},
			event:    seed.event,
			rule:     seed.rule,
			sources:  []ownership.HookSourceEvidence{{Resource: "gamma", SourceDigest: fingerprintAlpha}},
			want:     map[string]int{"PreToolUse": 1},
		},
		{
			name:     "wrong_target_fingerprint",
			selected: []resolver.ResolvedArtifact{alpha},
			event:    seed.event,
			rule:     seed.rule,
			sources:  []ownership.HookSourceEvidence{{Resource: "alpha", SourceDigest: wrongTarget}},
			want:     map[string]int{"PreToolUse": 1},
		},
		{
			name:     "wrong_scope_fingerprint",
			selected: []resolver.ResolvedArtifact{alpha},
			event:    seed.event,
			rule:     seed.rule,
			sources:  []ownership.HookSourceEvidence{{Resource: "alpha", SourceDigest: wrongScope}},
			want:     map[string]int{"PreToolUse": 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			nativeRoot := filepath.Join(repoRoot, ".factory")
			nativeBytes := []byte(seed.doc)
			nativePath := writeNativeFile(t, nativeRoot, layout, nativeBytes)
			satisfaction := nativeHookSatisfaction(t, tc.event, tc.rule, tc.sources)
			store := writeNativeSatisfactionState(t, nativeRoot, layout, nativeBytes, []ownership.Entry{}, []ownership.HookSatisfaction{satisfaction})
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
			report, err := Run(context.Background(), repoRoot, tc.selected)
			if err != nil {
				t.Fatalf("sync: %v", err)
			}
			assertNoMaterializeErrors(t, report)
			assertFactoryLegacyHooks(t, legacyHookFileFor(hcc.TargetFactory, false, repoRoot, home), sharedSourceCommand, tc.want)
			assertNativeSatisfactionUnmanaged(t, store, nativeRoot, hcc.TargetFactory, false,
				nativeHookProofEntry(t, seed.event, seed.rule, "alpha", fingerprintAlpha).ID())
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
		})
	}
}

// spec: hook-native-location-tolerance/partial-mixed-evidence-suppresses-nothing
func TestNativeSatisfactionPartialMixedEvidenceSuppressesNothing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	resolved, sourceRoot := namedHookPairFixture(t, config.RepoCommitted, hcc.TargetFactory, "beta")
	fingerprint := namedFingerprint(t, sourceRoot, "beta", string(hcc.TargetFactory), HookScopeRepository)
	seed := sharedNativeSeed(t, hcc.TargetFactory, false)
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	fullNative := []byte(`{"PreToolUse":[` + seed.rule + `],"PostToolUse":[` + seed.rule + `]}` + "\n")
	preOnly := []byte(`{"PreToolUse":[` + seed.rule + `]}` + "\n")
	postDrift := `{"hooks":[{"command":"echo drifted","type":"command"}],"matcher":"Execute"}`
	preAndDriftedPost := []byte(`{"PreToolUse":[` + seed.rule + `],"PostToolUse":[` + postDrift + `]}` + "\n")
	preEntry := nativeHookProofEntry(t, seed.event, seed.rule, "beta", fingerprint)
	postEntry := nativeHookProofEntry(t, "PostToolUse", seed.rule, "beta", fingerprint)
	preSatisfaction := nativeHookSatisfaction(t, seed.event, seed.rule, []ownership.HookSourceEvidence{{Resource: "beta", SourceDigest: fingerprint}})
	postSatisfaction := nativeHookSatisfaction(t, "PostToolUse", seed.rule, []ownership.HookSourceEvidence{{Resource: "beta", SourceDigest: fingerprint}})

	t.Run("complete_control", func(t *testing.T) {
		repoRoot := t.TempDir()
		nativeRoot := filepath.Join(repoRoot, ".factory")
		nativePath := writeNativeFile(t, nativeRoot, layout, fullNative)
		store := writeNativeSatisfactionState(t, nativeRoot, layout, fullNative, []ownership.Entry{}, []ownership.HookSatisfaction{preSatisfaction, postSatisfaction})
		nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
		stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
		report, err := Run(context.Background(), repoRoot, resolved)
		if err != nil {
			t.Fatalf("sync complete proof control: %v", err)
		}
		assertNoMaterializeErrors(t, report)
		assertPathAbsent(t, legacyHookFileFor(hcc.TargetFactory, false, repoRoot, home))
		assertNoMaterializeChanges(t, report)
		assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
		assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	})

	cases := []struct {
		name          string
		current       []byte
		entries       []ownership.Entry
		satisfactions []ownership.HookSatisfaction
	}{
		{name: "all_satisfaction_partial", current: preOnly, entries: []ownership.Entry{}, satisfactions: []ownership.HookSatisfaction{preSatisfaction, postSatisfaction}},
		{name: "mixed_owned_satisfaction_changed_value", current: preAndDriftedPost, entries: []ownership.Entry{preEntry}, satisfactions: []ownership.HookSatisfaction{postSatisfaction}},
		{name: "matching_owned_entry_missing_sat_clean", current: preOnly, entries: []ownership.Entry{postEntry}, satisfactions: []ownership.HookSatisfaction{preSatisfaction}},
		{name: "matching_satisfaction_missing_owned_clean", current: preOnly, entries: []ownership.Entry{preEntry}, satisfactions: []ownership.HookSatisfaction{postSatisfaction}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			nativeRoot := filepath.Join(repoRoot, ".factory")
			nativePath := writeNativeFile(t, nativeRoot, layout, tc.current)
			store := writeNativeSatisfactionState(t, nativeRoot, layout, fullNative, tc.entries, tc.satisfactions)
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
			report, err := Run(context.Background(), repoRoot, resolved)
			if err != nil {
				t.Fatalf("sync partial proof: %v", err)
			}
			assertNoMaterializeErrors(t, report)
			assertFactoryLegacyHooks(t, legacyHookFileFor(hcc.TargetFactory, false, repoRoot, home), sharedSourceCommand,
				map[string]int{"PreToolUse": 1, "PostToolUse": 1})
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
		})
	}
}

// spec: hook-native-location-tolerance/invalid-satisfaction-evidence-refuses-without-mutation
func TestNativeSatisfactionInvalidEvidenceRefusesWithoutMutation(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := t.TempDir()
	resolved, sourceRoot := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "alpha")
	resolved.Harnesses = []hcc.Target{hcc.TargetFactory, hcc.TargetCursor}
	factoryRoot := filepath.Join(repoRoot, ".factory")
	factoryLayout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	factorySeed := sharedNativeSeed(t, hcc.TargetFactory, false)
	factoryNative := []byte(factorySeed.doc)
	factoryNativePath := writeNativeFile(t, factoryRoot, factoryLayout, factoryNative)
	factoryLegacy := legacyHookFileFor(hcc.TargetFactory, false, repoRoot, home)
	if err := os.MkdirAll(filepath.Dir(factoryLegacy), 0o755); err != nil {
		t.Fatal(err)
	}
	legacySeed := []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + sharedSourceCommand + `"}],"_managed_by":"agent-marketplace"}],"PostToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"` + preservedLegacySentinel + `"}]}]}` + "\n")
	if err := os.WriteFile(factoryLegacy, legacySeed, 0o644); err != nil {
		t.Fatal(err)
	}
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, factoryLegacy)
	if !strings.Contains(string(legacyBefore), sharedSourceCommand) || !strings.Contains(string(legacyBefore), preservedLegacySentinel) {
		t.Fatal("invalid-state fixture is missing its marked twin or handwritten sentinel")
	}
	fingerprint := namedFingerprint(t, sourceRoot, "alpha", string(hcc.TargetFactory), HookScopeRepository)
	invalid := nativeHookSatisfaction(t, factorySeed.event, factorySeed.rule, []ownership.HookSourceEvidence{{Resource: "alpha", SourceDigest: fingerprint}})
	invalid.Sources[0] = ownership.HookSourceEvidence{Resource: rejectedSatisfactionSentinel, SourceDigest: "sha256:zz"}
	factoryStore := writeNativeSatisfactionState(t, factoryRoot, factoryLayout, factoryNative, []ownership.Entry{}, []ownership.HookSatisfaction{invalid})
	factoryNativeBefore, factoryNativeMode, factoryNativeMtime := fileSnapshot(t, factoryNativePath)
	factoryStateBefore, factoryStateMode, factoryStateMtime := fileSnapshot(t, factoryStore.StateFile())
	stateTreeBefore := satisfactionPathTree(t, stateHome)
	repoTreeBefore := satisfactionPathTree(t, repoRoot)
	cursorRoot, _ := nativeHookRootFor(hcc.TargetCursor, false, repoRoot, home)
	cursorStore, err := ownership.Open(cursorRoot)
	if err != nil {
		t.Fatal(err)
	}
	cursorLegacy := legacyHookFileFor(hcc.TargetCursor, false, repoRoot, home)
	assertPathAbsent(t, cursorStore.StateFile())
	assertPathAbsent(t, cursorLegacy)
	logs := captureNativeSatisfactionLogs(t)

	dryReport, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved}, WithDryRun())
	if err != nil {
		t.Fatalf("sync --check with invalid satisfaction: %v", err)
	}
	if !diag.HasCode(dryReport.Diags, diag.CodeHookNativeToleranceRefused) {
		t.Fatalf("missing native tolerance refusal: %+v", dryReport.Diags)
	}
	if strings.Contains(fmt.Sprint(dryReport.Diags), rejectedSatisfactionSentinel) || strings.Contains(logs.String(), rejectedSatisfactionSentinel) {
		t.Fatal("invalid satisfaction value leaked into diagnostics or logs")
	}
	assertFileUnchanged(t, factoryNativePath, factoryNativeBefore, factoryNativeMode, factoryNativeMtime)
	assertFileUnchanged(t, factoryStore.StateFile(), factoryStateBefore, factoryStateMode, factoryStateMtime)
	assertPathAbsent(t, cursorStore.StateFile())
	assertPathAbsent(t, cursorLegacy)
	assertFileUnchanged(t, factoryLegacy, legacyBefore, legacyMode, legacyMtime)
	if fmt.Sprint(stateTreeBefore) != fmt.Sprint(satisfactionPathTree(t, stateHome)) ||
		fmt.Sprint(repoTreeBefore) != fmt.Sprint(satisfactionPathTree(t, repoRoot)) {
		t.Fatal("dry run created filesystem or state-root residue")
	}

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatalf("sync with invalid satisfaction: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
		t.Fatalf("missing native tolerance refusal: %+v", report.Diags)
	}
	if strings.Contains(fmt.Sprint(report.Diags), rejectedSatisfactionSentinel) || strings.Contains(logs.String(), rejectedSatisfactionSentinel) {
		t.Fatal("invalid satisfaction value leaked into diagnostics or logs")
	}
	assertFileUnchanged(t, factoryLegacy, legacyBefore, legacyMode, legacyMtime)
	cursorData, cursorErr := os.ReadFile(cursorLegacy)
	var cursorDocument map[string]json.RawMessage
	if cursorErr != nil || json.Unmarshal(cursorData, &cursorDocument) != nil || cursorDocument["hooks"] == nil || !strings.Contains(string(cursorData), sharedSourceCommand) {
		t.Fatalf("independent Cursor did not reconcile its hook: %v", cursorErr)
	}
	assertFileUnchanged(t, factoryNativePath, factoryNativeBefore, factoryNativeMode, factoryNativeMtime)
	assertFileUnchanged(t, factoryStore.StateFile(), factoryStateBefore, factoryStateMode, factoryStateMtime)

	t.Run("dry_run_without_state_root", func(t *testing.T) {
		emptyStateHome := t.TempDir()
		t.Setenv("XDG_STATE_HOME", emptyStateHome)
		root := t.TempDir()
		selection, _ := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "alpha")
		layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
		nativeRoot := filepath.Join(root, ".factory")
		seed := sharedNativeSeed(t, hcc.TargetFactory, false)
		nativeBytes := []byte(seed.doc)
		nativePath := writeNativeFile(t, nativeRoot, layout, nativeBytes)
		store, err := ownership.Open(nativeRoot)
		if err != nil {
			t.Fatal(err)
		}
		stateTree := satisfactionPathTree(t, emptyStateHome)
		rootTree := satisfactionPathTree(t, root)
		nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
		legacyPath := legacyHookFileFor(hcc.TargetFactory, false, root, home)
		assertPathAbsent(t, store.StateFile())
		dry, err := Run(context.Background(), root, []resolver.ResolvedArtifact{selection}, WithDryRun())
		if err != nil {
			t.Fatalf("sync --check without prior state: %v", err)
		}
		if dry.InSync() {
			t.Fatal("dry run without proof did not report the legacy write it would perform")
		}
		assertPathAbsent(t, store.StateFile())
		assertPathAbsent(t, legacyPath)
		assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
		if fmt.Sprint(stateTree) != fmt.Sprint(satisfactionPathTree(t, emptyStateHome)) ||
			fmt.Sprint(rootTree) != fmt.Sprint(satisfactionPathTree(t, root)) {
			t.Fatal("dry run created files, state directories, locks, or temporary residue")
		}
	})
}

// spec: hook-native-location-tolerance/verified-mirror-revision-uses-its-own-fingerprint
// spec: hook-native-location-tolerance/unverified-or-changed-mirror-revision-never-satisfies
func TestNativeSatisfactionMirrorFingerprint(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	const verifiedDigest = "sha256:abababababababababababababababababababababababababababababababab"
	const changedDigest = "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	cases := []struct {
		name        string
		selectionFP string
		wantLegacy  bool
	}{
		{name: "matching_verified_revision", selectionFP: verifiedDigest},
		{name: "changed_verified_revision", selectionFP: changedDigest, wantLegacy: true},
		{name: "missing_verified_revision", selectionFP: "", wantLegacy: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			resolved, _ := mirrorHookFixture(t, hcc.TargetFactory, verifiedDigest)
			nativeRoot := filepath.Join(repoRoot, ".factory")
			layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
			rule := `{"matcher":"Execute","hooks":[{"type":"command","command":"./run.sh"}]}`
			seed := nativeSeed{event: "PreToolUse", rule: rule, doc: `{"PreToolUse":[` + rule + `]}` + "\n"}
			nativeBytes := []byte(seed.doc)
			nativePath := writeNativeFile(t, nativeRoot, layout, nativeBytes)
			fingerprint := mirrorFingerprint(t, resolved, string(hcc.TargetFactory), HookScopeRepository)
			satisfaction := nativeHookSatisfaction(t, seed.event, seed.rule, []ownership.HookSourceEvidence{{Resource: resolved.Name, SourceDigest: fingerprint}})
			store := writeNativeSatisfactionState(t, nativeRoot, layout, nativeBytes, []ownership.Entry{}, []ownership.HookSatisfaction{satisfaction})
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
			selection := resolved
			selection.MarketplaceDigest = tc.selectionFP
			report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{selection})
			if err != nil {
				t.Fatalf("sync: %v", err)
			}
			assertNoMaterializeErrors(t, report)
			legacyPath := legacyHookFileFor(hcc.TargetFactory, false, repoRoot, "")
			if tc.wantLegacy {
				assertFactoryLegacyHooks(t, legacyPath, "./run.sh", map[string]int{"PreToolUse": 1})
			} else {
				assertPathAbsent(t, legacyPath)
				assertNoMaterializeChanges(t, report)
			}
			assertNativeSatisfactionUnmanaged(t, store, nativeRoot, hcc.TargetFactory, false, satisfaction.ID())
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
		})
	}
}

// spec: hook-native-location-tolerance/satisfaction-only-state-remains-unowned
func TestNativeSatisfactionSurvivesOwnedMutation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	resolved, sourceRoot := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "alpha")
	fingerprint := namedFingerprint(t, sourceRoot, "alpha", string(hcc.TargetFactory), HookScopeRepository)
	seed := sharedNativeSeed(t, hcc.TargetFactory, false)
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	postBefore := `{"hooks":[{"command":"echo post-before","type":"command"}],"matcher":"Execute"}`
	postAfter := `{"hooks":[{"command":"echo post-after","type":"command"}],"matcher":"Execute"}`
	baseDoc := []byte(`{"PreToolUse":[` + seed.rule + `],"PostToolUse":[` + postBefore + `]}` + "\n")
	updatedDoc := []byte(`{"PreToolUse":[` + seed.rule + `],"PostToolUse":[` + postAfter + `]}` + "\n")
	removedDoc := []byte(`{"PreToolUse":[` + seed.rule + `]}` + "\n")

	for _, tc := range []struct {
		name    string
		next    []byte
		entries []ownership.Entry
	}{
		{name: "owned_update", next: updatedDoc, entries: []ownership.Entry{nativeHookProofEntry(t, "PostToolUse", postAfter, "other", "")}},
		{name: "owned_removal", next: removedDoc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			nativeRoot := filepath.Join(repoRoot, ".factory")
			nativePath := writeNativeFile(t, nativeRoot, layout, baseDoc)
			satisfaction := nativeHookSatisfaction(t, seed.event, seed.rule,
				[]ownership.HookSourceEvidence{{Resource: "alpha", SourceDigest: fingerprint}})
			owned := nativeHookProofEntry(t, "PostToolUse", postBefore, "other", "")
			store := writeNativeSatisfactionState(t, nativeRoot, layout, baseDoc,
				[]ownership.Entry{owned}, []ownership.HookSatisfaction{satisfaction})

			priorProof, err := store.Proof(layout.rel)
			if err != nil {
				t.Fatalf("Proof: %v", err)
			}
			if _, err := store.Apply(ownership.Mutation{
				Rel: layout.rel, AbsFile: nativePath, NextBytes: tc.next,
				Entries: tc.entries, Reason: ownership.ReasonSync,
				ExpectPriorGeneration: priorProof.Generation(),
				ExpectPriorDigest:     ownership.Digest(baseDoc),
			}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			mutated, err := os.ReadFile(nativePath)
			if err != nil || !bytes.Equal(mutated, tc.next) {
				t.Fatalf("mutated native file = %q (err %v)", mutated, err)
			}

			proof, err := store.Proof(layout.rel)
			if err != nil {
				t.Fatalf("Proof after owned mutation: %v", err)
			}
			if got := proof.HookSatisfactions(); !reflect.DeepEqual(got, []ownership.HookSatisfaction{satisfaction}) {
				t.Fatalf("satisfaction evidence after owned mutation = %+v, want %+v", got, satisfaction)
			}
			if proof.OwnedCount() != len(tc.entries) {
				t.Fatalf("owned count = %d, want %d", proof.OwnedCount(), len(tc.entries))
			}
			nativeSnap, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			stateSnap, stateMode, stateMtime := fileSnapshot(t, store.StateFile())

			report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
			if err != nil {
				t.Fatalf("sync after owned mutation: %v", err)
			}
			assertNoMaterializeErrors(t, report)
			assertPathAbsent(t, legacyHookFileFor(hcc.TargetFactory, false, repoRoot, home))
			assertNoMaterializeChanges(t, report)
			assertNativeSatisfactionUnmanaged(t, store, nativeRoot, hcc.TargetFactory, false, satisfaction.ID())
			assertFileUnchanged(t, nativePath, nativeSnap, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateSnap, stateMode, stateMtime)
		})
	}
}
