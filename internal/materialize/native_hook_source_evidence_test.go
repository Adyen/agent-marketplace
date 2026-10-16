package materialize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// sharedSourceCommand is the literal hook command both selections share.
const sharedSourceCommand = "echo shared-proof"

const (
	nativeHookContributorAlphaFP          = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	nativeHookContributorAlphaAlternateFP = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	nativeHookContributorBetaFP           = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// namedHookFixture builds an independent local-directory source carrying one
// PreToolUse command hook that runs the shared literal command and returns
// the resolved artifact under the given resource name.
func namedHookFixture(t *testing.T, source config.Source, target hcc.Target, name string) (resolver.ResolvedArtifact, string) {
	t.Helper()
	definition := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + sharedSourceCommand + `"}]}]}}`
	resolved, sourceRoot := namedHookFixtures(t, source, target, name, definition)
	if len(resolved) != 1 {
		t.Fatalf("expected one ingested hook, got %d", len(resolved))
	}
	return resolved[0], sourceRoot
}

func namedHookFixtures(t *testing.T, source config.Source, target hcc.Target, name, definition string) ([]resolver.ResolvedArtifact, string) {
	t.Helper()
	return sourceBackedHookFixtures(t, "claude", source, target, name, []byte(definition), nil)
}

func sourceBackedHookFixtures(t testing.TB, sourceHarness string, source config.Source, target hcc.Target, name string, definition []byte, supportFiles map[string][]byte) ([]resolver.ResolvedArtifact, string) {
	t.Helper()
	src := t.TempDir()
	hooksDir := filepath.Join(src, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definitionPath := filepath.Join(hooksDir, "hooks.json")
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, data := range supportFiles {
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			t.Fatalf("unsafe source support path %q", rel)
		}
		path := filepath.Join(src, clean)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := catalog.Load(src, sourceHarness)
	if err != nil {
		t.Fatalf("%s catalog ingest: %v", sourceHarness, err)
	}
	var resolved []resolver.ResolvedArtifact
	for _, entry := range loaded.Entries {
		if entry.Kind != ir.KindHook {
			continue
		}
		entry.Name = name
		resolved = append(resolved, resolver.ResolvedArtifact{
			Entry:       entry,
			Source:      source,
			RegistryURL: src,
			CatalogKey:  src,
			V4Kind:      config.V4SourceDirectory,
			Harnesses:   []hcc.Target{target},
		})
	}
	if len(resolved) == 0 {
		t.Fatalf("%s catalog ingest produced no hooks", sourceHarness)
	}
	return resolved, src
}

func ingestedHookFixture(t testing.TB, source config.Source, target hcc.Target, name string, hook *ir.Hook, supportFiles map[string][]byte) resolver.ResolvedArtifact {
	t.Helper()
	if !reflect.DeepEqual(hook.Ext, (ir.Hook{}).Ext) {
		t.Fatal("hook extensions require a source-format fixture")
	}
	sourceHarness := hook.HookSource
	if sourceHarness == "" {
		sourceHarness = "claude"
	}
	handler := map[string]any{"type": hook.Type}
	if hook.Command != "" {
		handler["command"] = hook.Command
	}
	if hook.Prompt != "" {
		handler["prompt"] = hook.Prompt
	}
	if hook.Timeout != nil {
		handler["timeout"] = *hook.Timeout
	}
	group := map[string]any{"hooks": []any{handler}}
	if hook.Matcher != "" {
		group["matcher"] = hook.Matcher
	}
	for key, value := range hook.Extra {
		if hook.GroupExtraKeys[key] {
			group[key] = value
		} else {
			handler[key] = value
		}
	}
	definition, err := json.Marshal(map[string]any{hook.Event: []any{group}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := sourceBackedHookFixtures(t, sourceHarness, source, target, name, definition, supportFiles)
	if len(resolved) != 1 {
		t.Fatalf("expected one ingested hook, got %d", len(resolved))
	}
	return resolved[0]

}

// namedFingerprint computes the version-1 source-hook fingerprint for a
// namedHookFixture selection.
func namedFingerprint(t *testing.T, src, name, target, scope string) string {
	t.Helper()
	digest, err := HookSourceFingerprint(HookSourceFingerprintInput{
		SourceIdentity: "local:" + filepath.Clean(src),
		Name:           name,
		Target:         target,
		Scope:          scope,
		SourceRoot:     src,
		DefinitionRel:  "hooks/hooks.json",
	})
	if err != nil {
		t.Fatalf("source fingerprint: %v", err)
	}
	return digest
}

// sharedNativeSeed returns the target's documented native hook document with
// one rule that runs the shared literal command, hand-authored in each
// target's own native shape.
func sharedNativeSeed(t *testing.T, target hcc.Target, machine bool) nativeSeed {
	t.Helper()
	switch target {
	case hcc.TargetClaude:
		rule := `{"hooks":[{"command":"` + sharedSourceCommand + `","type":"command"}],"matcher":"Bash"}`
		return nativeSeed{event: "PreToolUse", rule: rule,
			doc: `{"hooks":{"PreToolUse":[` + rule + `]}}` + "\n"}
	case hcc.TargetCursor:
		rule := `{"command":"` + sharedSourceCommand + `","matcher":"^Shell$","type":"command"}`
		return nativeSeed{event: "preToolUse", rule: rule,
			doc: `{"version":1,"hooks":{"preToolUse":[` + rule + `]}}` + "\n"}
	case hcc.TargetFactory:
		rule := `{"hooks":[{"command":"` + sharedSourceCommand + `","type":"command"}],"matcher":"Execute"}`
		return nativeSeed{event: "PreToolUse", rule: rule,
			doc: `{"PreToolUse":[` + rule + `]}` + "\n"}
	case hcc.TargetWindsurf:
		rule := `{"command":"` + sharedSourceCommand + `"}`
		if machine {
			return nativeSeed{event: "pre_run_command", rule: rule,
				doc: `{"hooks":{"post_cascade_response":[{"command":"./hand.sh","show_output":true}],"pre_run_command":[` + rule + `]}}` + "\n"}
		}
		return nativeSeed{event: "pre_run_command", rule: rule,
			doc: `{"hooks":{"pre_run_command":[` + rule + `]}}` + "\n"}
	}
	t.Fatalf("no shared native seed for %s", target)
	return nativeSeed{}
}

func fileSnapshot(t *testing.T, path string) (contents []byte, mode os.FileMode, mtime time.Time) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data, info.Mode(), info.ModTime()
}

// assertFileUnchanged fails when the file's bytes, mode, or mtime differ from
// the snapshot.
func assertFileUnchanged(t *testing.T, path string, before []byte, mode os.FileMode, mtime time.Time) {
	t.Helper()
	data, m, mt := fileSnapshot(t, path)
	if string(data) != string(before) || m != mode || !mt.Equal(mtime) {
		t.Fatalf("%s changed bytes, mode, or mtime", path)
	}
}

// legacyHookRuleCount parses a legacy hooks.json fragment file and returns the
// flattened managed rules per event.
func legacyHookRules(t *testing.T, path string) map[string][]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("legacy file: %v", err)
	}
	var doc map[string][]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("legacy file is not a hook event map: %v", err)
	}
	out := map[string][]map[string]any{}
	for event, raws := range doc {
		for _, raw := range raws {
			var rule map[string]any
			if err := json.Unmarshal(raw, &rule); err != nil {
				t.Fatalf("legacy rule is not an object: %v", err)
			}
			out[event] = append(out[event], rule)
		}
	}
	return out
}

// Two selected hook resources whose compilations share one canonical native
// handler are BOTH satisfied when the durable record proves each of them on
// the recorded entry: one through scalar evidence, the rest through
// source_evidence tuples. Neither selection recreates legacy output, and
// native, legacy, and ownership-state bytes stay untouched.
// spec: hook-native-location-tolerance/several-selected-sources-share-one-clean-native-entry
// spec: hook-native-location-tolerance/scalar-source-evidence-remains-compatible
func TestNativeSharedSourceEvidence(t *testing.T) {
	for _, tc := range allNativeHookLayouts() {
		name := fmt.Sprintf("%s_machine=%v", tc.target, tc.machineScope)
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			home := t.TempDir()
			t.Setenv("HOME", home)
			repoRoot := t.TempDir()
			source := config.RepoCommitted
			scope := HookScopeRepository
			if tc.machineScope {
				source = config.UserPersonal
				scope = HookScopeMachine
			}
			alpha, alphaSrc := namedHookFixture(t, source, tc.target, "alpha")
			beta, betaSrc := namedHookFixture(t, source, tc.target, "beta")
			layout, ok := nativeHookLayoutFor(tc.target, tc.machineScope)
			if !ok {
				t.Fatalf("%s layout missing", tc.target)
			}
			nativeRoot, ok := nativeHookRootFor(tc.target, tc.machineScope, repoRoot, home)
			if !ok {
				t.Fatalf("%s native root missing", tc.target)
			}
			seed := sharedNativeSeed(t, tc.target, tc.machineScope)
			// One owned entry carries alpha's pair in the scalar fields and
			// beta's pair as an evidence tuple.
			entry := nativeHookProofEntry(t, seed.event, seed.rule, "alpha",
				namedFingerprint(t, alphaSrc, "alpha", string(tc.target), scope))
			entry.SourceEvidence = []ownership.HookSourceEvidence{
				{Resource: "beta", SourceDigest: namedFingerprint(t, betaSrc, "beta", string(tc.target), scope)},
			}
			seedNativeProof(t, nativeRoot, layout, []byte(seed.doc), entry)

			nativePath := filepath.Join(nativeRoot, filepath.FromSlash(layout.rel))
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			store, err := ownership.Open(nativeRoot)
			if err != nil {
				t.Fatal(err)
			}
			statePath := store.StateFile()
			stateBefore, stateMode, stateMtime := fileSnapshot(t, statePath)

			report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{alpha, beta})
			if err != nil {
				t.Fatalf("sync: %v", err)
			}
			if diag.HasErrors(report.Diags) {
				t.Fatalf("sync reported error diagnostics: %#v", report.Diags)
			}
			for _, root := range report.PerRoot {
				if len(root.Written) != 0 || len(root.Removed) != 0 {
					t.Fatalf("sync wrote or removed paths under shared proof: %+v", root)
				}
			}
			// Neither selection recreated legacy output. Windsurf machine
			// shares one file, so "no legacy write" there means the shared
			// file is byte-identical.
			if tc.target == hcc.TargetWindsurf && tc.machineScope {
				after, err := os.ReadFile(nativePath)
				if err != nil || string(after) != string(nativeBefore) {
					t.Fatalf("shared windsurf file changed:\nbefore: %s\nafter:  %s", nativeBefore, after)
				}
			} else if data, readErr := os.ReadFile(legacyHookFileFor(tc.target, tc.machineScope, repoRoot, home)); readErr == nil {
				t.Fatalf("legacy hook output recreated for a shared native entry: %s", data)
			}
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, statePath, stateBefore, stateMode, stateMtime)

			dryReport, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{alpha, beta}, WithDryRun())
			if err != nil {
				t.Fatalf("sync --check: %v", err)
			}
			if diag.HasErrors(dryReport.Diags) {
				t.Fatalf("sync --check reported error diagnostics: %#v", dryReport.Diags)
			}
			if !dryReport.InSync() {
				t.Fatalf("sync --check reports pending writes: %+v", dryReport.PerRoot)
			}
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, statePath, stateBefore, stateMode, stateMtime)
		})
	}
}

// A recorded entry whose evidence does not cover a selected resource proves
// nothing for it: the unproven contributor keeps exactly its own legacy
// output and the native entry is never adopted or rewritten.
// spec: hook-native-location-tolerance/unproven-contributor-retains-legacy-behavior
func TestNativeSharedSourceEvidenceUnprovenContributor(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	alpha, alphaSrc := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "alpha")
	beta, betaSrc := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "beta")

	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	seed := sharedNativeSeed(t, hcc.TargetFactory, false)
	fpA := namedFingerprint(t, alphaSrc, "alpha", "factory", HookScopeRepository)
	fpB := namedFingerprint(t, betaSrc, "beta", "factory", HookScopeRepository)

	cases := map[string]func(e *ownership.Entry){
		// Only alpha's scalar evidence is recorded; beta has no tuple.
		"missing_beta_tuple": func(e *ownership.Entry) {},
		// A tuple exists for beta but under the wrong fingerprint.
		"wrong_beta_fingerprint": func(e *ownership.Entry) {
			e.SourceEvidence = []ownership.HookSourceEvidence{{Resource: "beta",
				SourceDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}}
		},
		// A tuple exists under beta's fingerprint but another resource name.
		"wrong_beta_resource": func(e *ownership.Entry) {
			e.SourceEvidence = []ownership.HookSourceEvidence{{Resource: "gamma", SourceDigest: fpB}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			nr := filepath.Join(root, ".factory")
			entry := nativeHookProofEntry(t, seed.event, seed.rule, "alpha", fpA)
			mutate(&entry)
			seedNativeProof(t, nr, layout, []byte(seed.doc), entry)
			nativePath := filepath.Join(nr, "hooks.json")
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			store, err := ownership.Open(nr)
			if err != nil {
				t.Fatal(err)
			}
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())

			if _, err := runLegacyToleranceFixture(context.Background(), root, []resolver.ResolvedArtifact{alpha, beta}); err != nil {
				t.Fatalf("sync: %v", err)
			}
			// Exactly one unproven hook handler lands in legacy output: the
			// shared command under the source event with its marker.
			rules := legacyHookRules(t, filepath.Join(nr, "hooks", "hooks.json"))
			if len(rules) != 1 || len(rules["PreToolUse"]) != 1 {
				t.Fatalf("expected exactly one unproven legacy handler, got %#v", rules)
			}
			rule := rules["PreToolUse"][0]
			if rule["_managed_by"] != "agent-marketplace" || rule["matcher"] != "Execute" {
				t.Fatalf("unproven legacy handler malformed: %#v", rule)
			}
			hooks, _ := rule["hooks"].([]any)
			if len(hooks) != 1 {
				t.Fatalf("unproven legacy handler has %d commands", len(hooks))
			}
			if cmd, _ := hooks[0].(map[string]any)["command"].(string); cmd != sharedSourceCommand {
				t.Fatalf("unproven legacy command = %q, want %q", cmd, sharedSourceCommand)
			}
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
		})
	}
}

// Partial or invalid shared proof can never suppress a selection: --check
// first proves the unproven contributor is pending without writing anything,
// then a mutating sync writes exactly that contributor's legacy handler while
// native, state, config, and lock stay untouched. Corrupt evidence fails
// closed with the stable tolerance code under all three commands.
// A clean matching tuple cannot suppress a selection while ANOTHER recorded
// entry matching that selection is missing or drifted: the proof is
// incomplete, so legacy output proceeds and nothing is adopted.
// spec: hook-native-location-tolerance/partial-shared-proof-cannot-suppress-a-selection
func TestNativeSharedSourceEvidencePartialProof(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	// alpha's scalar fingerprint only has to be well-formed here: alpha is
	// not selected in these scenarios.
	const fpA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secondRule := `{"hooks":[{"command":"echo second-proof","type":"command"}],"matcher":"Write"}`
	cases := map[string]func(t *testing.T, nr string, layout nativeHookLayout, seed nativeSeed, fpB string){
		// The writer recorded a second beta rule that is no longer in the
		// file; beta's clean entry cannot override the missing one.
		"missing_matching_entry": func(t *testing.T, nr string, layout nativeHookLayout, seed nativeSeed, fpB string) {
			clean := nativeHookProofEntry(t, seed.event, seed.rule, "alpha", fpA)
			clean.SourceEvidence = []ownership.HookSourceEvidence{{Resource: "beta", SourceDigest: fpB}}
			seedNativeProof(t, nr, layout, []byte(seed.doc), clean,
				nativeHookProofEntry(t, "PostToolUse", secondRule, "beta", fpB))
		},
		// The second recorded beta rule exists but drifted.
		"drifted_matching_entry": func(t *testing.T, nr string, layout nativeHookLayout, seed nativeSeed, fpB string) {
			drifted := `{"hooks":[{"command":"echo second-proof","type":"command"}],"matcher":"Write","extra":true}`
			body := `{"PostToolUse":[` + drifted + `],"PreToolUse":[` + seed.rule + `]}` + "\n"
			clean := nativeHookProofEntry(t, seed.event, seed.rule, "alpha", fpA)
			clean.SourceEvidence = []ownership.HookSourceEvidence{{Resource: "beta", SourceDigest: fpB}}
			seedNativeProof(t, nr, layout, []byte(body), clean,
				nativeHookProofEntry(t, "PostToolUse", secondRule, "beta", fpB))
		},
	}
	for name, seedFn := range cases {
		t.Run(name, func(t *testing.T) {
			repoRoot := t.TempDir()
			beta, betaSrc := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "beta")
			nativeRoot := filepath.Join(repoRoot, ".factory")
			layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
			seed := sharedNativeSeed(t, hcc.TargetFactory, false)
			fpB := namedFingerprint(t, betaSrc, "beta", "factory", HookScopeRepository)
			seedFn(t, nativeRoot, layout, seed, fpB)
			nativePath := filepath.Join(nativeRoot, "hooks.json")
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			store, err := ownership.Open(nativeRoot)
			if err != nil {
				t.Fatal(err)
			}
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())

			// The mutating command writes ONLY the unproven contributor's handler.
			if _, err := runLegacyToleranceFixture(context.Background(), repoRoot, []resolver.ResolvedArtifact{beta}); err != nil {
				t.Fatalf("sync: %v", err)
			}
			rules := legacyHookRules(t, filepath.Join(nativeRoot, "hooks", "hooks.json"))
			if len(rules) != 1 || len(rules["PreToolUse"]) != 1 {
				t.Fatalf("partial proof produced unexpected legacy handlers: %#v", rules)
			}
			if cmd, _ := rules["PreToolUse"][0]["hooks"].([]any)[0].(map[string]any)["command"].(string); cmd != sharedSourceCommand {
				t.Fatalf("legacy handler command = %q", cmd)
			}
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
		})
	}
}

// Invalid source evidence poisons the record for its target and scope only:
// the affected target refuses with the stable tolerance code and writes
// nothing, while an independent selected target reconciles normally.
// spec: hook-native-location-tolerance/invalid-additional-evidence-refuses-without-mutation
func TestNativeSharedSourceEvidenceInvalidState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoRoot := t.TempDir()
	resolved, src := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "alpha")
	resolved.Harnesses = []hcc.Target{hcc.TargetFactory, hcc.TargetCursor}

	nativeRoot := filepath.Join(repoRoot, ".factory")
	layout, _ := nativeHookLayoutFor(hcc.TargetFactory, false)
	seed := sharedNativeSeed(t, hcc.TargetFactory, false)
	seedNativeProof(t, nativeRoot, layout, []byte(seed.doc),
		nativeHookProofEntry(t, seed.event, seed.rule, "alpha",
			namedFingerprint(t, src, "alpha", "factory", HookScopeRepository)))

	// Corrupt the record: an evidence tuple carrying a sentinel payload as
	// its resource name, which Apply would never write.
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	sentinel := "rm -rf /sentinel-payload"
	corrupt := strings.Replace(string(data), `"source_digest"`,
		`"source_evidence":[{"resource":`+fmt.Sprintf("%q", sentinel)+`,"source_digest":"sha256:zz"}],"source_digest"`, 1)
	if !strings.Contains(corrupt, "source_evidence") {
		t.Fatal("corruption did not land")
	}
	if err := os.WriteFile(store.StateFile(), []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	nativePath := filepath.Join(nativeRoot, "hooks.json")
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
		t.Fatalf("missing %s diagnostic: %#v", diag.CodeHookNativeToleranceRefused, report.Diags)
	}
	for _, d := range report.Diags {
		if strings.Contains(fmt.Sprintf("%v", d), sentinel) {
			t.Fatalf("diagnostic leaks rejected evidence: %v", d)
		}
	}
	assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	if data, readErr := os.ReadFile(filepath.Join(nativeRoot, "hooks", "hooks.json")); readErr == nil {
		t.Fatalf("corrupt proof still suppressed legacy output: %s", data)
	}
	// Cursor has no native file and no corrupt state: it reconciles.
	cursorNativePath := filepath.Join(repoRoot, ".cursor", "hooks.json")
	cursorBytes, err := os.ReadFile(cursorNativePath)
	if err != nil {
		t.Fatalf("independent native target did not reconcile: %v", err)
	}
	var cursorDocument map[string]any
	if err := json.Unmarshal(cursorBytes, &cursorDocument); err != nil {
		t.Fatalf("decode independent Cursor hooks: %v", err)
	}
	if cursorDocument["version"] != float64(1) {
		t.Fatalf("Cursor hook version = %v, want 1", cursorDocument["version"])
	}
	cursorHooks, ok := cursorDocument["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("Cursor hooks member = %T, want object", cursorDocument["hooks"])
	}
	cursorEntries, ok := cursorHooks["preToolUse"].([]any)
	if !ok || len(cursorEntries) != 1 {
		t.Fatalf("Cursor preToolUse entries = %#v, want one", cursorHooks["preToolUse"])
	}
	cursorEntry, ok := cursorEntries[0].(map[string]any)
	if !ok || cursorEntry["command"] != sharedSourceCommand || cursorEntry["matcher"] != "^Shell$" || cursorEntry["type"] != "command" {
		t.Fatalf("Cursor Native hook = %#v", cursorEntries[0])
	}
	cursorStore, err := ownership.Open(filepath.Join(repoRoot, ".cursor"))
	if err != nil {
		t.Fatal(err)
	}
	cursorProof, err := cursorStore.Proof("hooks.json")
	if err != nil || cursorProof.OwnedCount() != 1 || len(cursorProof.HookSatisfactions()) != 0 {
		t.Fatalf("independent Cursor ownership proof = %+v / %v", cursorProof, err)
	}
	cursorOwned, ok := cursorProof.Entry(cursorProof.OwnedIDs()[0])
	if !ok || cursorOwned.Resource != "alpha" || cursorOwned.SourceDigest != namedFingerprint(t, src, "alpha", string(hcc.TargetCursor), HookScopeRepository) {
		t.Fatalf("independent Cursor proof entry = %+v", cursorOwned)
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, ".cursor", "hooks", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("independent target wrote a legacy Cursor file: %v", err)
	}
}

func nativeHookContributorFactory(t *testing.T) (string, outputKey, string, ownedJSONFileSpec) {
	t.Helper()
	repoRoot, _, _ := nativeHookJSONTestEnvironment(t)
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	key := outputKey{target: tc.target, machineScope: tc.machineScope}
	destination, spec, err := nativeHookJSONLayout(key, repoRoot)
	if err != nil {
		t.Fatal("Factory hook layout did not resolve")
	}
	return repoRoot, key, destination.Root, spec
}

func TestNativeHookContributorEvidencePersists(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	makeWork := func(t *testing.T, reverse bool, command string) (*ownedJSONAggregateWork, ownership.EntryID) {
		t.Helper()
		alpha := nativeHookJSONWork(t, tc, "alpha", command).entries[0]
		alpha.sourceDigest = nativeHookContributorAlphaFP
		alpha.sourceEvidence = []ownership.HookSourceEvidence{{Resource: "alpha", SourceDigest: nativeHookContributorAlphaAlternateFP}}
		beta := nativeHookJSONWork(t, tc, "beta", command).entries[0]
		beta.sourceDigest = nativeHookContributorBetaFP
		beta.sourceEvidence = []ownership.HookSourceEvidence{
			{Resource: "alpha", SourceDigest: nativeHookContributorAlphaFP},
			{Resource: "alpha", SourceDigest: nativeHookContributorAlphaAlternateFP},
			{Resource: "beta", SourceDigest: nativeHookContributorBetaFP},
		}
		alphaInput := append([]ownership.HookSourceEvidence(nil), alpha.sourceEvidence...)
		betaInput := append([]ownership.HookSourceEvidence(nil), beta.sourceEvidence...)
		work := &ownedJSONAggregateWork{}
		if reverse {
			work.add(beta)
			work.add(alpha)
		} else {
			work.add(alpha)
			work.add(beta)
		}
		if !reflect.DeepEqual(alpha.sourceEvidence, alphaInput) || !reflect.DeepEqual(beta.sourceEvidence, betaInput) {
			t.Fatal("aggregate work mutated a contributor's source-evidence input")
		}
		return work, alpha.id()
	}
	orders := []struct {
		name    string
		reverse bool
	}{{name: "forward"}, {name: "reverse", reverse: true}}
	var firstNative string
	for _, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			repoRoot, key, root, spec := nativeHookContributorFactory(t)
			work, alphaID := makeWork(t, order.reverse, nativeHookJSONSelectedCommand)
			view := work.entries[0].ownershipEntry()
			storedSources := append([]ownership.HookSourceEvidence(nil), work.entries[0].sourceEvidence...)
			view.SourceEvidence[0].Resource = "mutated"
			if !reflect.DeepEqual(work.entries[0].sourceEvidence, storedSources) {
				t.Fatal("ownership entry source evidence aliases aggregate work")
			}
			report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, work, nativeHookJSONOptions(), false)
			if report == nil || diag.HasErrors(report.Diags) {
				t.Fatalf("native hook reconciliation failed: %+v", report)
			}
			path := filepath.Join(root, filepath.FromSlash(spec.rel))
			nativeBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("reconciled Factory hook file is missing")
			}
			commands := nativeHookJSONCommands(t, nativeBytes, spec.rel, spec.container, tc.event, tc.shape)
			if len(commands) != 1 || commands[0] != nativeHookJSONSelectedCommand || strings.Contains(string(nativeBytes), "_managed_by") {
				t.Fatalf("native Factory hook = %s, want one unmarked shared command", nativeBytes)
			}
			store, err := ownership.Open(root)
			if err != nil {
				t.Fatal("open Factory ownership store")
			}
			proof, err := store.Proof(spec.rel)
			if err != nil {
				t.Fatalf("ownership proof: %v", err)
			}
			ids := proof.OwnedIDs()
			if proof.OwnedCount() != 1 || len(ids) != 1 || ids[0] != alphaID {
				t.Fatalf("owned hook identities = %v, want one shared identity", ids)
			}
			got, ok := proof.Entry(ids[0])
			wantSources := []ownership.HookSourceEvidence{
				{Resource: "alpha", SourceDigest: nativeHookContributorAlphaAlternateFP},
				{Resource: "beta", SourceDigest: nativeHookContributorBetaFP},
			}
			if !ok || got.Resource != "alpha" || got.SourceDigest != nativeHookContributorAlphaFP || !reflect.DeepEqual(got.SourceEvidence, wantSources) {
				t.Fatalf("recorded contributor evidence = %+v, want alpha scalar and sorted remaining pairs", got)
			}
			if !proof.HasHookSource(ids[0], "alpha", nativeHookContributorAlphaFP) || !proof.HasHookSource(ids[0], "alpha", nativeHookContributorAlphaAlternateFP) ||
				!proof.HasHookSource(ids[0], "beta", nativeHookContributorBetaFP) || proof.HasHookSource(ids[0], "alpha", nativeHookContributorBetaFP) ||
				proof.HasHookSource(ids[0], "gamma", nativeHookContributorAlphaFP) {
				t.Fatal("recorded source evidence did not preserve exact contributor pairs")
			}
			if firstNative == "" {
				firstNative = string(nativeBytes)
			} else if string(nativeBytes) != firstNative {
				t.Fatalf("contributor insertion order changed native hook bytes: %s", nativeBytes)
			}
			if !order.reverse {
				t.Run("changed_beta_command_replaces_source_proof", func(t *testing.T) {
					before := append([]byte(nil), nativeBytes...)
					updatedCommand := "echo d9-updated-command"
					beta := nativeHookJSONWork(t, tc, "beta", updatedCommand).entries[0]
					beta.sourceDigest = nativeHookContributorBetaFP
					onlyBeta := &ownedJSONAggregateWork{}
					onlyBeta.add(beta)
					updated := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repoRoot, onlyBeta, nativeHookJSONOptions(), false)
					if updated == nil || diag.HasErrors(updated.Diags) {
						t.Fatalf("changed-command reconciliation failed: %+v", updated)
					}
					after, err := os.ReadFile(path)
					if err != nil || string(after) == string(before) {
						t.Fatalf("changed command did not rewrite native bytes: %v", err)
					}
					commands := nativeHookJSONCommands(t, after, spec.rel, spec.container, tc.event, tc.shape)
					if len(commands) != 1 || commands[0] != updatedCommand || strings.Contains(string(after), nativeHookJSONSelectedCommand) {
						t.Fatalf("rewritten Factory hooks = %v, want only %q", commands, updatedCommand)
					}
					proof, err := store.Proof(spec.rel)
					if err != nil {
						t.Fatalf("ownership proof: %v", err)
					}
					ids := proof.OwnedIDs()
					if proof.OwnedCount() != 1 || len(ids) != 1 || ids[0] == alphaID {
						t.Fatalf("rewritten owned identities = %v, want only the changed hook", ids)
					}
					got, ok := proof.Entry(ids[0])
					if !ok || got.Resource != "beta" || got.SourceDigest != nativeHookContributorBetaFP || len(got.SourceEvidence) != 0 {
						t.Fatalf("rewritten proof adopted stale contributors: %+v", got)
					}
				})
			}
		})
	}
}

func TestNativeHookContributorEvidenceCompatibility(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")

	first := nativeHookJSONWork(t, tc, "alpha", nativeHookJSONSelectedCommand).entries[0]
	second := nativeHookJSONWork(t, tc, "beta", nativeHookJSONSelectedCommand).entries[0]
	firstRaw := string(first.raw)
	legacy := &ownedJSONAggregateWork{}
	legacy.add(first)
	legacy.add(second)
	got := legacy.entries[0].ownershipEntry()
	if len(legacy.entries) != 1 || legacy.entries[0].resource != "alpha" || string(legacy.entries[0].raw) != firstRaw ||
		got.Resource != "alpha" || got.SourceDigest != "" || len(got.SourceEvidence) != 0 {
		t.Fatalf("empty-proof hook duplicate changed first winner or gained proof: %+v / %+v", legacy.entries, got)
	}

	mcp := ownedJSONEntry{
		rel: "mcp_config.json", kind: ownership.KindMcpServer, container: "mcpServers", key: "server",
		raw: []byte(`{"command":"first","type":"stdio"}`), identity: ownership.McpIdentityDigest("server"), resource: "first-resource",
		sourceDigest: nativeHookContributorAlphaFP, sourceEvidence: []ownership.HookSourceEvidence{{Resource: "alpha", SourceDigest: nativeHookContributorAlphaFP}},
	}
	secondMCP := mcp
	secondMCP.raw = []byte(`{"command":"second","type":"stdio"}`)
	secondMCP.resource = "second-resource"
	secondMCP.sourceDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	work := &ownedJSONAggregateWork{}
	work.add(mcp)
	work.add(secondMCP)
	got = work.entries[0].ownershipEntry()
	if len(work.entries) != 1 || work.entries[0].resource != "first-resource" || string(work.entries[0].raw) != string(mcp.raw) ||
		got.Resource != "first-resource" || got.SourceDigest != "" || len(got.SourceEvidence) != 0 {
		t.Fatalf("keyed MCP duplicate changed first winner or carried hook metadata: %+v / %+v", work.entries, got)
	}
}

func TestNativeHookContributorEvidenceMalformedTupleUsesOwnershipValidation(t *testing.T) {
	tc := nativeHookJSONCaseByName(t, "factory_repo")
	_, _, root, spec := nativeHookContributorFactory(t)
	work := nativeHookJSONWork(t, tc, "alpha", nativeHookJSONSelectedCommand)
	body := []byte(`{"PreToolUse":[` + string(work.entries[0].raw) + `]}` + "\n")
	path := filepath.Join(root, filepath.FromSlash(spec.rel))
	entry := nativeHookProofEntry(t, tc.event, string(work.entries[0].raw), "alpha", nativeHookContributorAlphaFP)
	layout := nativeHookLayout{rel: spec.rel}
	seedNativeProof(t, root, layout, body, entry)
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(spec.rel)
	if err != nil {
		t.Fatal(err)
	}
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, path)
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
	invalid := work.entries[0]
	invalid.sourceDigest = nativeHookContributorAlphaFP
	invalid.sourceEvidence = []ownership.HookSourceEvidence{{Resource: "", SourceDigest: nativeHookContributorBetaFP}}
	invalidWork := &ownedJSONAggregateWork{}
	invalidWork.add(invalid)
	invalidEntry := invalidWork.entries[0].ownershipEntry()
	if len(invalidEntry.SourceEvidence) != 1 || invalidEntry.SourceEvidence[0].Resource != "" {
		t.Fatalf("invalid tuple was dropped before state validation: %+v", invalidEntry.SourceEvidence)
	}
	_, err = store.Apply(ownership.Mutation{
		Rel: spec.rel, AbsFile: path, NextBytes: []byte(`{"PreToolUse":[]}` + "\n"),
		Entries: []ownership.Entry{invalidEntry}, Reason: ownership.ReasonSync,
		ExpectPriorGeneration: proof.Generation(), ExpectPriorDigest: ownership.Digest(body),
	})
	if !errors.Is(err, ownership.ErrUnsafeStateValue) {
		t.Fatalf("invalid source tuple error = %v, want ErrUnsafeStateValue", err)
	}
	assertFileUnchanged(t, path, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
}
