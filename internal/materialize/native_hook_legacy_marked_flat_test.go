package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func geminiMarkedFlatEntry(command, matcher string) string {
	entry := `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : ` + nativeHookJSONJSONString(command)
	if matcher != "" {
		entry += `, "matcher" : ` + nativeHookJSONJSONString(matcher)
	}
	return entry + " }"
}

func geminiMarkedFlatCanonical(command, matcher string) string {
	entry := `{"_managed_by":"agent-marketplace","command":` + nativeHookJSONJSONString(command)
	if matcher != "" {
		entry += `,"matcher":` + nativeHookJSONJSONString(matcher)
	}
	return entry + `,"type":"command"}`
}

func geminiMarkedTransitionDocument(beforeToolEntry string) []byte {
	return []byte(`{
  /* keep handwritten settings and order */
  "unrelated" : { "keep" : "d75-setting" },
  "hooks" : {
    // handwritten native group stays first
    "AfterTool" : [
      { "matcher" : "^manual$", "hooks" : [ { "type" : "command", "command" : "echo d75-manual" } ] }
    ],
    "BeforeAgent" : [
      ` + geminiMarkedFlatEntry("echo d75-historical-prompt", "") + `
    ],
    "BeforeTool" : [
      ` + beforeToolEntry + `
    ]
  }
}
`)
}

func evalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return filepath.Join(resolved, filepath.Base(path))
}

func writeGeminiMarkedSettings(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
}

func geminiMarkedTransitionHookChanges(changes []HookChange, file string) (removals, additions []HookChange) {
	for _, change := range changes {
		if change.File != file || change.Target != hcc.TargetGemini {
			continue
		}
		if change.After == nil {
			removals = append(removals, change)
		} else {
			additions = append(additions, change)
		}
	}
	return removals, additions
}

func seedGeminiMarkedBaseline(t *testing.T, repo, home string) {
	t.Helper()
	seedNativeHookJSONLock(t, filepath.Join(repo, ".gemini"))
	seedNativeHookJSONLock(t, filepath.Join(home, ".gemini"))
	sentinel := filepath.Join(home, "state", "agent-marketplace", "d75-unrelated-sentinel")
	if err := os.WriteFile(sentinel, []byte("d75-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	geminiMarkedManualGroupA = `{ "matcher" : "^alpha$", "hooks" : [ { "type" : "command", "command" : "echo d75-manual-alpha" } ] }`
	geminiMarkedManualGroupB = `{ "matcher" : "^beta$", "hooks" : [ { "type" : "command", "command" : "echo d75-manual-beta" } ] }`
)

func geminiMarkedSelectedGroup(command string) string {
	return `{"hooks":[{"command":` + nativeHookJSONJSONString(command) + `,"type":"command"}],"matcher":"^run_shell_command$"}`
}

// spec: hook-resources/marked-gemini-flat-hooks-transition-at-both-scopes
func TestGeminiLegacyMarkedFlatHooksTransitionInPlace(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	seed := geminiMarkedTransitionDocument(geminiMarkedManualGroupA + ",\n      " + geminiMarkedFlatEntry("echo d75-historical-tool", "Bash") + ",\n      " + geminiMarkedManualGroupB)
	repoFile := filepath.Join(evalPath(t, repo), ".gemini", "settings.json")
	machineFile := filepath.Join(evalPath(t, home), ".gemini", "settings.json")
	writeGeminiMarkedSettings(t, repoFile, seed)
	writeGeminiMarkedSettings(t, machineFile, seed)
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	repoHook := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo d75-selected", "")
	machineHook := additiveSafetyProvider(t, hcc.TargetGemini, true, "echo d75-machine-selected", "")

	seedGeminiMarkedBaseline(t, repo, home)
	repoBefore, repoMode, repoMtime := fileSnapshot(t, repoFile)
	machineBefore, machineMode, machineMtime := fileSnapshot(t, machineFile)
	stateBefore := nativeSupportPersistentSnapshot(t, repo, home)

	preview, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{repoHook, machineHook}, WithMaterializationModes(modes), WithDryRun())
	if err != nil || diag.HasErrors(preview.Diags) {
		t.Fatalf("dry-run preview: %v / %v", err, preview.Diags)
	}
	if !diag.HasCode(preview.Diags, diag.CodeMaterializeHookReviewPending) {
		t.Fatalf("dry-run preview diagnostics = %v, want %s", preview.Diags, diag.CodeMaterializeHookReviewPending)
	}
	wantRemovalBefore := map[string]map[string]string{
		repoFile: {
			"BeforeAgent": geminiMarkedFlatCanonical("echo d75-historical-prompt", ""),
			"BeforeTool":  geminiMarkedFlatCanonical("echo d75-historical-tool", "Bash"),
		},
		machineFile: {
			"BeforeAgent": geminiMarkedFlatCanonical("echo d75-historical-prompt", ""),
			"BeforeTool":  geminiMarkedFlatCanonical("echo d75-historical-tool", "Bash"),
		},
	}
	for _, file := range []string{repoFile, machineFile} {
		removals, additions := geminiMarkedTransitionHookChanges(preview.HookChanges, file)
		if len(additions) == 0 {
			t.Fatalf("dry-run preview for %s listed no native additions: %+v", file, preview.HookChanges)
		}
		if len(removals) != 2 {
			t.Fatalf("dry-run preview for %s removals = %+v, want the two marked flat entries", file, removals)
		}
		for _, removal := range removals {
			want, ok := wantRemovalBefore[file][removal.Event]
			if !ok || string(removal.Before) != want || removal.After != nil {
				t.Fatalf("dry-run preview removal row = %+v, want canonical marked flat before-image for %s", removal, removal.Event)
			}
			if removal.Event == "BeforeTool" && removal.Matcher != "Bash" {
				t.Fatalf("dry-run preview removal matcher = %q, want the legacy Bash matcher", removal.Matcher)
			}
		}
	}
	assertFileUnchanged(t, repoFile, repoBefore, repoMode, repoMtime)
	assertFileUnchanged(t, machineFile, machineBefore, machineMode, machineMtime)
	assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))

	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{repoHook, machineHook}, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("transition Run: %v / %v", err, report.Diags)
	}
	if !diag.HasCode(report.Diags, diag.CodeMaterializeHookReview) {
		t.Fatalf("transition diagnostics = %v, want %s", report.Diags, diag.CodeMaterializeHookReview)
	}
	for _, file := range []string{repoFile, machineFile} {
		removals, additions := geminiMarkedTransitionHookChanges(report.HookChanges, file)
		if len(removals) != 2 {
			t.Fatalf("completed review for %s removals = %+v, want the two marked flat entries", file, removals)
		}
		var selected string
		if file == repoFile {
			selected = "echo d75-selected"
		} else {
			selected = "echo d75-machine-selected"
		}
		if len(additions) != 1 || string(additions[0].After) != geminiMarkedSelectedGroup(selected) || additions[0].Event != "BeforeTool" || additions[0].Matcher != "^run_shell_command$" {
			t.Fatalf("completed review for %s additions = %+v, want the exact selected native group", file, additions)
		}
	}
	for _, file := range []string{repoFile, machineFile} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range []string{
			"/* keep handwritten settings and order */",
			"// handwritten native group stays first",
			`"unrelated" : { "keep" : "d75-setting" }`,
			`"matcher" : "^manual$"`,
			"echo d75-manual",
		} {
			if !bytes.Contains(data, []byte(fragment)) {
				t.Fatalf("transition dropped handwritten content %q: %s", fragment, data)
			}
		}
		if bytes.Contains(data, []byte("_managed_by")) || bytes.Contains(data, []byte("d75-historical")) || bytes.Contains(data, []byte(`"BeforeAgent"`)) {
			t.Fatalf("transition left marked flat entries behind: %s", data)
		}
		var selected string
		if file == repoFile {
			selected = "echo d75-selected"
		} else {
			selected = "echo d75-machine-selected"
		}
		indexA := bytes.Index(data, []byte(geminiMarkedManualGroupA))
		indexB := bytes.Index(data, []byte(geminiMarkedManualGroupB))
		indexSelected := bytes.Index(data, []byte(geminiMarkedSelectedGroup(selected)))
		if indexA < 0 || indexB < 0 || indexSelected < 0 || indexA >= indexB || indexB >= indexSelected {
			t.Fatalf("mixed BeforeTool order changed (manualA=%d manualB=%d selected=%d): %s", indexA, indexB, indexSelected, data)
		}
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("transition changed mode of %s: %v", file, info)
		}
	}
	repoPlans, repoConflicts := prepareHookSupport([]skillTarget{{skill: repoHook, target: hcc.TargetGemini}})
	machinePlans, machineConflicts := prepareHookSupport([]skillTarget{{skill: machineHook, target: hcc.TargetGemini, machineScope: true}})
	if len(repoConflicts) != 0 || len(machineConflicts) != 0 || repoPlans[0].err != nil || machinePlans[0].err != nil || repoPlans[0].sourceDigest == "" || machinePlans[0].sourceDigest == "" {
		t.Fatalf("captured source digests = %+v / %+v, conflicts %v / %v", repoPlans, machinePlans, repoConflicts, machineConflicts)
	}
	for _, tc := range []struct {
		root         string
		command      string
		sourceDigest string
	}{
		{filepath.Join(repo, ".gemini"), "echo d75-selected", repoPlans[0].sourceDigest},
		{filepath.Join(home, ".gemini"), "echo d75-machine-selected", machinePlans[0].sourceDigest},
	} {
		data, err := os.ReadFile(filepath.Join(tc.root, "settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(data, []byte(tc.command)) != 1 {
			t.Fatalf("selected command count in %s = %d: %s", tc.root, bytes.Count(data, []byte(tc.command)), data)
		}
		store := nativeHookMetadataStore(t, tc.root)
		proof := nativeHookMetadataProof(t, store, "settings.json")
		if proof.OwnedCount() != 1 || len(proof.HookSatisfactions()) != 0 {
			t.Fatalf("transition proof for %s = %v / %+v, want one source-bound owned hook", tc.root, proof.OwnedIDs(), proof.HookSatisfactions())
		}
		id := proof.OwnedIDs()[0]
		entry, ok := proof.Entry(id)
		if !ok || id.Kind != ownership.KindHook || id.Container != "BeforeTool" || entry.Resource != repoHook.Name || entry.SourceDigest != tc.sourceDigest {
			t.Fatalf("transition proof entry = %+v / %+v, want captured digest %q", id, entry, tc.sourceDigest)
		}
	}
	repoAfter, _, _ := fileSnapshot(t, repoFile)
	machineAfter, _, _ := fileSnapshot(t, machineFile)

	second, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{repoHook, machineHook}, WithMaterializationModes(modes))
	if err != nil || diag.HasErrors(second.Diags) {
		t.Fatalf("repeat Run: %v / %v", err, second.Diags)
	}
	if len(second.HookChanges) != 0 || diag.HasCode(second.Diags, diag.CodeMaterializeHookReview) {
		t.Fatalf("repeat transition produced another review: %+v / %v", second.HookChanges, second.Diags)
	}
	if data, err := os.ReadFile(repoFile); err != nil || !bytes.Equal(data, repoAfter) {
		t.Fatalf("repeat transition changed %s: %s / %v", repoFile, data, err)
	}
	if data, err := os.ReadFile(machineFile); err != nil || !bytes.Equal(data, machineAfter) {
		t.Fatalf("repeat transition changed %s: %s / %v", machineFile, data, err)
	}
}

// spec: hook-resources/marked-gemini-flat-hooks-transition-at-both-scopes
func TestGeminiLegacyMarkedFlatHooksTransitionRecognizesFullLegacyShape(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	entries := []string{
		`{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-empty-matcher", "matcher" : "" }`,
		`{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-escaped", "matcher" : "^a\"b$" }`,
		`{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-zero-timeout", "timeout" : 0 }`,
		`{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-timeout", "timeout" : 600, "matcher" : "Bash" }`,
	}
	nativeFile := filepath.Join(repo, ".gemini", "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, geminiMarkedTransitionDocument(strings.Join(entries, ",\n      ")))
	hook := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo d75-selected", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("transition Run: %v / %v", err, report.Diags)
	}
	data, err := os.ReadFile(nativeFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("_managed_by")) || bytes.Contains(data, []byte("d75-empty-matcher")) || bytes.Contains(data, []byte("d75-escaped")) || bytes.Contains(data, []byte("d75-timeout")) || bytes.Contains(data, []byte(`"BeforeAgent"`)) {
		t.Fatalf("recognized legacy shapes were not all transitioned: %s", data)
	}
	if bytes.Count(data, []byte("echo d75-selected")) != 1 {
		t.Fatalf("selected command count = %d: %s", bytes.Count(data, []byte("echo d75-selected")), data)
	}
	removals, _ := geminiMarkedTransitionHookChanges(report.HookChanges, evalPath(t, nativeFile))
	if len(removals) != 5 {
		t.Fatalf("review removals = %+v, want the four BeforeTool entries plus the BeforeAgent entry", removals)
	}
}

// spec: hook-resources/unmarked-gemini-flat-hooks-are-refused-without-adoption
func TestGeminiLegacyMarkedFlatHooksRefuseUnrecognizedEntries(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		payload string
	}{
		{name: "unmarked flat", entry: `{ "type" : "command", "command" : "echo d75-unmarked" }`, payload: "echo d75-unmarked"},
		{name: "other owner", entry: `{ "_managed_by" : "other-tool", "type" : "command", "command" : "echo d75-other-owner" }`, payload: "echo d75-other-owner"},
		{name: "case-aliased marker", entry: `{ "_Managed_By" : "agent-marketplace", "type" : "command", "command" : "echo d75-casealias" }`, payload: "echo d75-casealias"},
		{name: "marked non-string command", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : [ "echo d75-nonstring" ] }`, payload: "echo d75-nonstring"},
		{name: "marked null command", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : null, "matcher" : "d75-nullcmd" }`, payload: "d75-nullcmd"},
		{name: "marked wrong type", entry: `{ "_managed_by" : "agent-marketplace", "type" : "prompt", "command" : "echo d75-wrong-type" }`, payload: "echo d75-wrong-type"},
		{name: "marked unknown key", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-extra", "extra" : true }`, payload: "echo d75-extra"},
		{name: "marked hooks case alias", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-hookalias", "Hooks" : [ ] }`, payload: "echo d75-hookalias"},
		{name: "marked null matcher", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-nullmatch", "matcher" : null }`, payload: "echo d75-nullmatch"},
		{name: "marked array matcher", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-arraymatch", "matcher" : [ "Bash" ] }`, payload: "echo d75-arraymatch"},
		{name: "marked object matcher", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-objectmatch", "matcher" : { "tool" : "Bash" } }`, payload: "echo d75-objectmatch"},
		{name: "marked bool matcher", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-boolmatch", "matcher" : true }`, payload: "echo d75-boolmatch"},
		{name: "marked number matcher", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-nummatch", "matcher" : 7 }`, payload: "echo d75-nummatch"},
		{name: "marked matcher case alias", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-matchalias", "Matcher" : "Bash" }`, payload: "echo d75-matchalias"},
		{name: "marked negative timeout", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-negtime", "timeout" : -1 }`, payload: "echo d75-negtime"},
		{name: "marked string timeout", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-strtime", "timeout" : "30" }`, payload: "echo d75-strtime"},
		{name: "marked null timeout", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-nulltime", "timeout" : null }`, payload: "echo d75-nulltime"},
		{name: "marked fractional timeout", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-fractime", "timeout" : 1.5 }`, payload: "echo d75-fractime"},
		{name: "marked bool timeout", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-booltime", "timeout" : true }`, payload: "echo d75-booltime"},
		{name: "marked object timeout", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-objtime", "timeout" : { } }`, payload: "echo d75-objtime"},
		{name: "marked array timeout", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-arrtime", "timeout" : [ 30 ] }`, payload: "echo d75-arrtime"},
		{name: "marked timeout case alias", entry: `{ "_managed_by" : "agent-marketplace", "type" : "command", "command" : "echo d75-timealias", "Timeout" : 30 }`, payload: "echo d75-timealias"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, home, _ := nativeHookJSONTestEnvironment(t)
			seed := geminiMarkedTransitionDocument(tc.entry + ",\n      " + geminiMarkedFlatEntry("echo d75-historical-tool", "Bash"))
			nativeFile := filepath.Join(repo, ".gemini", "settings.json")
			writeGeminiMarkedSettings(t, nativeFile, seed)
			seedGeminiMarkedBaseline(t, repo, home)
			before, mode, mtime := fileSnapshot(t, nativeFile)
			stateBefore := nativeSupportPersistentSnapshot(t, repo, home)
			logs := captureNativeHookJSONLogs(t)
			hook := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo d75-selected", "")
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !diag.HasErrors(report.Diags) {
				t.Fatalf("unrecognized flat entry was accepted or adopted: %+v", report.Diags)
			}
			assertFileUnchanged(t, nativeFile, before, mode, mtime)
			assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))
			assertNativeHookJSONNoPayload(t, report, logs, tc.payload)
		})
	}
}

// spec: hook-resources/unmarked-gemini-flat-hooks-are-refused-without-adoption
func TestGeminiMarkedNativeGroupKeepsHandwrittenSemantics(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	markedGroup := `{ "_managed_by" : "agent-marketplace", "matcher" : "^manual$", "hooks" : [ { "type" : "command", "command" : "echo d75-grouped" } ] }`
	seed := geminiMarkedTransitionDocument(geminiMarkedFlatEntry("echo d75-historical-tool", "Bash"))
	seed = bytes.Replace(seed, []byte(`"AfterTool" : [
      { "matcher" : "^manual$", "hooks" : [ { "type" : "command", "command" : "echo d75-manual" } ] }
    ]`), []byte(`"AfterTool" : [
      `+markedGroup+`
    ]`), 1)
	nativeFile := filepath.Join(repo, ".gemini", "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, seed)
	seedGeminiMarkedBaseline(t, repo, home)
	hook := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo d75-selected", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report.Diags)
	}
	data, err := os.ReadFile(nativeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(markedGroup)) || !bytes.Contains(data, []byte("echo d75-selected")) || bytes.Count(data, []byte("_managed_by")) != 1 {
		t.Fatalf("marked handwritten native group was not preserved exactly once: %s", data)
	}
	store := nativeHookMetadataStore(t, filepath.Join(repo, ".gemini"))
	proof := nativeHookMetadataProof(t, store, "settings.json")
	for _, id := range proof.OwnedIDs() {
		if id.Kind == ownership.KindHook && id.Container == "AfterTool" {
			t.Fatalf("marked handwritten native group was adopted: %+v", id)
		}
	}
}

// spec: hook-resources/marked-gemini-flat-hooks-transition-at-both-scopes
func TestGeminiLegacyMarkedFlatHandwrittenSatisfactionStaysUnowned(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	handwrittenIdentical := `{ "matcher" : "^run_shell_command$", "hooks" : [ { "type" : "command", "command" : "echo d75-selected" } ] }`
	seed := geminiMarkedTransitionDocument(geminiMarkedFlatEntry("echo d75-historical-tool", "Bash") + ",\n      " + handwrittenIdentical)
	nativeFile := filepath.Join(repo, ".gemini", "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, seed)
	seedGeminiMarkedBaseline(t, repo, home)
	hook := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo d75-selected", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report.Diags)
	}
	data, err := os.ReadFile(nativeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(handwrittenIdentical)) || bytes.Contains(data, []byte("_managed_by")) || bytes.Count(data, []byte("echo d75-selected")) != 1 {
		t.Fatalf("satisfied handwritten group was not preserved exactly once: %s", data)
	}
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: hook, target: hcc.TargetGemini}})
	if len(conflicts) != 0 || plans[0].err != nil || plans[0].sourceDigest == "" {
		t.Fatalf("captured source digest = %+v / %v", plans, conflicts)
	}
	store := nativeHookMetadataStore(t, filepath.Join(repo, ".gemini"))
	proof := nativeHookMetadataProof(t, store, "settings.json")
	if proof.OwnedCount() != 0 || len(proof.HookSatisfactions()) != 1 {
		t.Fatalf("satisfaction proof = %d owned / %+v satisfactions, want zero owned and one satisfaction", proof.OwnedCount(), proof.HookSatisfactions())
	}
	satisfaction := proof.HookSatisfactions()[0]
	wantSources := map[string]string{hook.Name: plans[0].sourceDigest}
	if len(satisfaction.Sources) != 1 || satisfaction.Sources[0].SourceDigest != wantSources[satisfaction.Sources[0].Resource] {
		t.Fatalf("satisfaction sources = %+v, want current captured digest %q for %q", satisfaction.Sources, plans[0].sourceDigest, hook.Name)
	}
	before, mode, mtime := fileSnapshot(t, nativeFile)
	second, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(second.Diags) {
		t.Fatalf("repeat Run: %v / %v", err, second.Diags)
	}
	if len(second.HookChanges) != 0 {
		t.Fatalf("repeat run reported changes: %+v", second.HookChanges)
	}
	assertFileUnchanged(t, nativeFile, before, mode, mtime)
	deselected, err := Run(context.Background(), repo, nil, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(deselected.Diags) {
		t.Fatalf("deselected Run: %v / %v", err, deselected.Diags)
	}
	data, err = os.ReadFile(nativeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(handwrittenIdentical)) || bytes.Contains(data, []byte("_managed_by")) || bytes.Contains(data, []byte("d75-historical")) {
		t.Fatalf("deselection removed the handwritten group: %s", data)
	}
}

func geminiMarkedTransitionWork(t *testing.T, repo string, incomplete bool) (outputKey, *ownedJSONAggregateWork) {
	t.Helper()
	key := outputKey{target: hcc.TargetGemini}
	_, file, err := nativeHookJSONLayout(key, repo)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ownedJSONHookEntries(file.rel, map[string][]json.RawMessage{
		"BeforeTool": {json.RawMessage(geminiMarkedSelectedGroup("echo d75-selected"))},
	}, "selected")
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		entries[i].sourceDigest = "sha256:" + strings.Repeat("b", 64)
	}
	work := &ownedJSONAggregateWork{}
	for _, entry := range entries {
		work.add(entry)
	}
	if incomplete {
		work.markIncomplete(file.rel)
	}
	return key, work
}

// spec: hook-resources/gemini-legacy-preview-and-failed-preparation-preserve-bytes
func TestGeminiLegacyMarkedFlatIncompletePreservesLegacy(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	seed := geminiMarkedTransitionDocument(geminiMarkedFlatEntry("echo d75-historical-tool", "Bash"))
	nativeFile := filepath.Join(repo, ".gemini", "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, seed)
	seedGeminiMarkedBaseline(t, repo, home)
	before, mode, mtime := fileSnapshot(t, nativeFile)
	stateBefore := nativeSupportPersistentSnapshot(t, repo, home)
	key, work := geminiMarkedTransitionWork(t, repo, true)
	report := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repo, work, nativeHookReconcileOptionsFor(nil), false)
	if !diag.HasErrors(report.Diags) {
		t.Fatalf("incomplete reconciliation was not refused: %+v", report.Diags)
	}
	assertFileUnchanged(t, nativeFile, before, mode, mtime)
	assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))
	if len(report.HookChanges) != 0 {
		t.Fatalf("incomplete reconciliation reported hook changes: %+v", report.HookChanges)
	}
}

// spec: hook-resources/gemini-legacy-preview-and-failed-preparation-preserve-bytes
func TestGeminiLegacyMarkedFlatCorruptProofRefuses(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	nativeRoot := filepath.Join(repo, ".gemini")
	nativeFile := filepath.Join(nativeRoot, "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, geminiMarkedTransitionDocument(geminiMarkedManualGroupA))
	hook := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo d75-selected", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("seed Run: %v / %v", err, report.Diags)
	}
	store := nativeHookMetadataStore(t, nativeRoot)
	if err := os.WriteFile(store.StateFile(), []byte("{ not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeGeminiMarkedSettings(t, nativeFile, geminiMarkedTransitionDocument(geminiMarkedFlatEntry("echo d75-historical-tool", "Bash")))
	before, mode, mtime := fileSnapshot(t, nativeFile)
	stateBefore := nativeSupportPersistentSnapshot(t, repo, home)
	logs := captureNativeHookJSONLogs(t)
	report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil {
		t.Fatalf("Run with corrupt proof: %v", err)
	}
	if !diag.HasErrors(report.Diags) || !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
		t.Fatalf("corrupt ownership evidence did not refuse with the fixed code: %+v", report.Diags)
	}
	assertFileUnchanged(t, nativeFile, before, mode, mtime)
	assertNativeSupportSnapshot(t, stateBefore, nativeSupportPersistentSnapshot(t, repo, home))
	if len(report.HookChanges) != 0 {
		t.Fatalf("refused transition reported hook changes: %+v", report.HookChanges)
	}
	assertNativeHookJSONNoPayload(t, report, logs, "echo d75-historical-tool", "echo d75-selected")
}

// spec: hook-resources/gemini-legacy-preview-and-failed-preparation-preserve-bytes
func TestGeminiLegacyMarkedFlatPublishFaultKeepsBytesAndRecovers(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	nativeFile := filepath.Join(repo, ".gemini", "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, geminiMarkedTransitionDocument(geminiMarkedFlatEntry("echo d75-historical-tool", "Bash")))
	seedGeminiMarkedBaseline(t, repo, home)
	before, mode, mtime := fileSnapshot(t, nativeFile)
	stateFile := nativeHookMetadataStore(t, filepath.Join(repo, ".gemini")).StateFile()
	if _, err := os.Lstat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("unexpected ownership state before transition: %v", err)
	}
	key, work := geminiMarkedTransitionWork(t, repo, false)

	options := nativeHookReconcileOptionsFor(nil)
	var mcpDiagnostics []diag.Diagnostic
	finalize := nativeHookActivationFinalizer(key, repo, nil, false, func() {}, &mcpDiagnostics)
	injected := errors.New("interrupted marked flat transition")
	options.nativeHookFinalize = func(ctx context.Context, store *ownership.Store, output outputKey, outDir string, aggregate ownedJSONFileSpec, abs string, raw []byte, readErr error, doc *jsonc.Document, plan ownedJSONPatch, proof ownership.Proof, proofErr error, incomplete, dryRun bool) (nativeHookTransactionExtras, error) {
		extras, err := finalize(ctx, store, output, outDir, aggregate, abs, raw, readErr, doc, plan, proof, proofErr, incomplete, dryRun)
		extras.commitOptions = append(extras.commitOptions, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
			if point.Stage == dependencytxn.FaultBeforePublish && point.Index == 0 {
				return injected
			}
			return nil
		}))
		return extras, err
	}
	refused := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repo, work, options, false)
	if !diag.HasErrors(refused.Diags) {
		t.Fatalf("faulted transition diagnostics = %v, want a publication error", refused.Diags)
	}
	assertFileUnchanged(t, nativeFile, before, mode, mtime)
	if _, err := os.Lstat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("faulted transition wrote ownership state: %v", err)
	}
	if len(refused.HookChanges) != 0 {
		t.Fatalf("faulted publication reported completed hook changes: %+v", refused.HookChanges)
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencytxn.Recover(context.Background(), stateDir); err != nil {
		t.Fatalf("recover interrupted transition: %v", err)
	}
	converged := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repo, work, nativeHookReconcileOptionsFor(nil), false)
	if diag.HasErrors(converged.Diags) {
		t.Fatalf("converged reconciliation diagnostics = %v", converged.Diags)
	}
	data, err := os.ReadFile(nativeFile)
	if err != nil || bytes.Contains(data, []byte("_managed_by")) || !bytes.Contains(data, []byte("echo d75-selected")) || !bytes.Contains(data, []byte("echo d75-manual")) {
		t.Fatalf("recovered transition = %s / %v", data, err)
	}
}

// spec: hook-resources/gemini-legacy-preview-and-failed-preparation-preserve-bytes
func TestGeminiLegacyMarkedFlatConcurrentChangeRefuses(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	nativeFile := filepath.Join(repo, ".gemini", "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, geminiMarkedTransitionDocument(geminiMarkedFlatEntry("echo d75-historical-tool", "Bash")))
	seedGeminiMarkedBaseline(t, repo, home)
	stateFile := nativeHookMetadataStore(t, filepath.Join(repo, ".gemini")).StateFile()
	if _, err := os.Lstat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("unexpected ownership state before transition: %v", err)
	}
	concurrent := []byte(`{"hooks":{"BeforeTool":[{"_managed_by":"agent-marketplace","type":"command","command":"echo d75-concurrent"}]}}` + "\n")
	key, work := geminiMarkedTransitionWork(t, repo, false)

	options := nativeHookReconcileOptionsFor(nil)
	var mcpDiagnostics []diag.Diagnostic
	finalize := nativeHookActivationFinalizer(key, repo, nil, false, func() {}, &mcpDiagnostics)
	options.nativeHookFinalize = func(ctx context.Context, store *ownership.Store, output outputKey, outDir string, aggregate ownedJSONFileSpec, abs string, raw []byte, readErr error, doc *jsonc.Document, plan ownedJSONPatch, proof ownership.Proof, proofErr error, incomplete, dryRun bool) (nativeHookTransactionExtras, error) {
		extras, err := finalize(ctx, store, output, outDir, aggregate, abs, raw, readErr, doc, plan, proof, proofErr, incomplete, dryRun)
		extras.commitOptions = append(extras.commitOptions, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
			if point.Stage == dependencytxn.FaultAfterLocks {
				return os.WriteFile(abs, concurrent, 0o640)
			}
			return nil
		}))
		return extras, err
	}
	refused := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repo, work, options, false)
	conflict := diag.HasCode(refused.Diags, diag.CodeOwnershipGenerationConflict)
	for _, d := range refused.Diags {
		if strings.Contains(d.Message, "generation conflict") {
			conflict = true
		}
	}
	if !conflict {
		t.Fatalf("concurrent transition diagnostics = %v, want a generation conflict", refused.Diags)
	}
	if got, err := os.ReadFile(nativeFile); err != nil || !bytes.Equal(got, concurrent) {
		t.Fatalf("concurrent native change was overwritten: %s / %v", got, err)
	}
	if _, err := os.Lstat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("conflicted transition wrote ownership state: %v", err)
	}
	if len(refused.HookChanges) != 0 {
		t.Fatalf("conflicted transition reported hook changes: %+v", refused.HookChanges)
	}
}

// spec: hook-resources/marked-gemini-flat-hooks-transition-at-both-scopes
func TestGeminiLegacyMarkedFlatTransitionsJointlyWithMCPUpdate(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	nativeRoot := filepath.Join(repo, ".gemini")
	nativeFile := filepath.Join(nativeRoot, "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, []byte(`{
  "unrelated" : { "keep" : "d75-setting" },
  "mcpServers" : { "handwritten" : { "command" : "manual-bin" } },
  "hooks" : { "AfterTool" : [ { "matcher" : "^manual$", "hooks" : [ { "type" : "command", "command" : "echo d75-manual" } ] } ] }
}
`))
	mcpV1 := geminiMCPProvider(t, "managed-server", "server-v1")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{mcpV1}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("MCP v1 seed Run: %v / %v", err, report.Diags)
	}
	writeGeminiMarkedSettings(t, nativeFile, []byte(`{
  // handwritten settings stay
  "unrelated" : { "keep" : "d75-setting" },
  "mcpServers" : {
    "handwritten" : { "command" : "manual-bin" },
    "managed-server" : { "command" : "server-v1", "args" : [ "--stdio" ] }
  },
  "hooks" : {
    "AfterTool" : [ { "matcher" : "^manual$", "hooks" : [ { "type" : "command", "command" : "echo d75-manual" } ] } ],
    "BeforeTool" : [ `+geminiMarkedFlatEntry("echo d75-historical-tool", "Bash")+` ]
  }
}
`))
	hook := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo d75-selected", "")
	mcpV2 := geminiMCPProvider(t, "managed-server", "server-v2")
	joint, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook, mcpV2}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(joint.Diags) {
		t.Fatalf("joint Run: %v / %v", err, joint.Diags)
	}
	data, err := os.ReadFile(nativeFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"// handwritten settings stay",
		`"unrelated" : { "keep" : "d75-setting" }`,
		`"command" : "manual-bin"`,
		`"command" : "echo d75-manual"`,
		"server-v2",
		"echo d75-selected",
	} {
		if !bytes.Contains(data, []byte(fragment)) {
			t.Fatalf("joint transition dropped %q: %s", fragment, data)
		}
	}
	if bytes.Contains(data, []byte("_managed_by")) || bytes.Contains(data, []byte("d75-historical")) || bytes.Contains(data, []byte("server-v1")) {
		t.Fatalf("joint transition left stale content: %s", data)
	}
	store := nativeHookMetadataStore(t, nativeRoot)
	proof := nativeHookMetadataProof(t, store, "settings.json")
	if proof.OwnedCount() != 2 {
		t.Fatalf("joint proof = %+v, want the hook and the MCP server", proof.OwnedIDs())
	}
	var hookOwned, mcpOwned bool
	for _, id := range proof.OwnedIDs() {
		switch {
		case id.Kind == ownership.KindHook && id.Container == "BeforeTool":
			hookOwned = true
		case id.Kind == ownership.KindMcpServer && id.Container == "mcpServers" && id.Key == "managed-server":
			mcpOwned = true
		}
	}
	if !hookOwned || !mcpOwned {
		t.Fatalf("joint proof missing hook or MCP entry: %+v", proof.OwnedIDs())
	}
}

// spec: hook-resources/gemini-legacy-preview-and-failed-preparation-preserve-bytes
func TestGeminiLegacyMarkedFlatJointPublishFaultKeepsBoth(t *testing.T) {
	repo, home, _ := nativeHookJSONTestEnvironment(t)
	nativeRoot := filepath.Join(repo, ".gemini")
	nativeFile := filepath.Join(nativeRoot, "settings.json")
	writeGeminiMarkedSettings(t, nativeFile, []byte(`{
  "mcpServers" : { "handwritten" : { "command" : "manual-bin" } }
}
`))
	mcpV1 := geminiMCPProvider(t, "managed-server", "server-v1")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{mcpV1}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("MCP v1 seed Run: %v / %v", err, report.Diags)
	}
	writeGeminiMarkedSettings(t, nativeFile, []byte(`{
  "mcpServers" : {
    "handwritten" : { "command" : "manual-bin" },
    "managed-server" : { "command" : "server-v1", "args" : [ "--stdio" ] }
  },
  "hooks" : { "BeforeTool" : [ `+geminiMarkedFlatEntry("echo d75-historical-tool", "Bash")+` ] }
}
`))
	seedGeminiMarkedBaseline(t, repo, home)
	before, mode, mtime := fileSnapshot(t, nativeFile)
	stateFile := nativeHookMetadataStore(t, nativeRoot).StateFile()
	stateBefore, stateMode, stateMtime := fileSnapshot(t, stateFile)

	key, work := geminiMarkedTransitionWork(t, repo, false)
	_, file, err := nativeHookJSONLayout(key, repo)
	if err != nil {
		t.Fatal(err)
	}
	mcpEntry := ownedJSONEntry{
		rel: file.rel, container: "mcpServers", key: "managed-server", kind: ownership.KindMcpServer,
		identity: ownership.McpIdentityDigest("managed-server"), resource: "managed-server", raw: json.RawMessage(`{"command":"server-v2","args":["--stdio"]}`),
	}
	options := nativeHookReconcileOptionsFor(nil)
	var mcpDiagnostics []diag.Diagnostic
	finalize := nativeHookActivationFinalizer(key, repo, []ownedJSONEntry{mcpEntry}, false, func() {}, &mcpDiagnostics)
	injected := errors.New("interrupted joint transition")
	options.nativeHookFinalize = func(ctx context.Context, store *ownership.Store, output outputKey, outDir string, aggregate ownedJSONFileSpec, abs string, raw []byte, readErr error, doc *jsonc.Document, plan ownedJSONPatch, proof ownership.Proof, proofErr error, incomplete, dryRun bool) (nativeHookTransactionExtras, error) {
		extras, err := finalize(ctx, store, output, outDir, aggregate, abs, raw, readErr, doc, plan, proof, proofErr, incomplete, dryRun)
		extras.commitOptions = append(extras.commitOptions, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
			if point.Stage == dependencytxn.FaultBeforePublish && point.Index == 0 {
				return injected
			}
			return nil
		}))
		return extras, err
	}
	refused := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repo, work, options, false)
	if !diag.HasErrors(refused.Diags) {
		t.Fatalf("faulted joint transition diagnostics = %v, want a publication error", refused.Diags)
	}
	assertFileUnchanged(t, nativeFile, before, mode, mtime)
	assertFileUnchanged(t, stateFile, stateBefore, stateMode, stateMtime)
	if len(refused.HookChanges) != 0 {
		t.Fatalf("faulted joint publication reported completed changes: %+v", refused.HookChanges)
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencytxn.Recover(context.Background(), stateDir); err != nil {
		t.Fatalf("recover joint transition: %v", err)
	}

	converged := reconcileNativeHookJSON(context.Background(), nativeHookJSONReport(), key, repo, work, nativeHookReconcileOptionsFor(nil), false)
	if diag.HasErrors(converged.Diags) {
		t.Fatalf("converged joint reconciliation diagnostics = %v", converged.Diags)
	}
	data, err := os.ReadFile(nativeFile)
	if err != nil || bytes.Contains(data, []byte("_managed_by")) || bytes.Contains(data, []byte("server-v1")) || !bytes.Contains(data, []byte("server-v2")) || !bytes.Contains(data, []byte("echo d75-selected")) {
		t.Fatalf("converged joint file = %s / %v", data, err)
	}
}
