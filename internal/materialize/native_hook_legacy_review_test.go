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

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// spec: hook-resources/changed-hook-commands-are-listed-with-exact-values
// spec: hook-resources/check-reports-pending-hook-changes-without-writing
// spec: hook-resources/unchanged-hooks-produce-no-review
func TestNativeHookLegacyRemovalReportsReview(t *testing.T) {
	for _, name := range []string{"claude_repo", "claude_machine", "cursor_repo", "cursor_machine", "factory_repo", "factory_machine", "windsurf_repo"} {
		t.Run(name, func(t *testing.T) {
			repo, home, codexHome := nativeHookJSONTestEnvironment(t)
			layout := nativeHookJSONCaseByName(t, name)
			_, _, _, nativePath := nativeHookMetadataTarget(t, layout, repo, home, codexHome)
			legacyPath := legacyHookFileFor(layout.target, layout.machineScope, repo, home)
			provider := additiveSafetyProvider(t, layout.target, layout.machineScope, "echo zqx-native-current-"+name, "")

			report := runHookReview(t, repo, layout.target, layout.machineScope, false, provider)
			if len(report.HookChanges) != 1 {
				t.Fatalf("seed sync hook changes = %#v, want the added native row", report.HookChanges)
			}

			oldCommand := "echo zqx-legacy-old-" + name
			legacyDoc, _ := nativeHookLegacyDocument(layout.target, layout.container, layout.event, layout.matcher, oldCommand, false, true)
			additiveSafetyWrite(t, legacyPath, string(legacyDoc))
			if err := os.Chmod(legacyPath, 0o640); err != nil {
				t.Fatal(err)
			}
			legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyPath)
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			fsBefore := nativeSupportSnapshot(t, repo, home)
			wantMatcher := layout.matcher
			if layout.target == hcc.TargetWindsurf {
				wantMatcher = ""
			}
			canonicalLegacy := canonicalTransactionTestPath(t, legacyPath)

			dry := runHookReview(t, repo, layout.target, layout.machineScope, true, provider)
			if len(dry.HookChanges) != 1 {
				t.Fatalf("dry-run hook changes = %#v, want exactly one legacy removal row", dry.HookChanges)
			}
			row := dry.HookChanges[0]
			if row.Target != layout.target || row.MachineScope != layout.machineScope {
				t.Fatalf("row target/scope = %s/%t, want %s/%t", row.Target, row.MachineScope, layout.target, layout.machineScope)
			}
			if row.File != canonicalLegacy {
				t.Fatalf("row file = %q, want the legacy path %q", row.File, canonicalLegacy)
			}
			if row.Event != layout.event || row.Matcher != wantMatcher {
				t.Fatalf("row event/matcher = %q/%q, want %q/%q", row.Event, row.Matcher, layout.event, wantMatcher)
			}
			if len(row.After) != 0 || !bytes.Contains(row.Before, []byte(oldCommand)) {
				t.Fatalf("row = %#v, want a removal carrying the old command %q", row, oldCommand)
			}
			if bytes.Contains(row.Before, []byte("zqx-native-current")) {
				t.Fatalf("row before payload = %s, must not contain the unchanged native command", row.Before)
			}
			requireHookReviewCodes(t, dry, nativeHookReviewCodesFor(layout.target, layout.machineScope, true))
			assertFileUnchanged(t, legacyPath, legacyBefore, legacyMode, legacyMtime)
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			assertNativeSupportSnapshot(t, fsBefore, nativeSupportSnapshot(t, repo, home))

			report = runHookReview(t, repo, layout.target, layout.machineScope, false, provider)
			if len(report.HookChanges) != 1 {
				t.Fatalf("mutating sync hook changes = %#v, want exactly the legacy removal row", report.HookChanges)
			}
			row = report.HookChanges[0]
			if row.File != canonicalLegacy || len(row.After) != 0 || !bytes.Contains(row.Before, []byte(oldCommand)) {
				t.Fatalf("mutating row = %#v, want removal of %q at %s", row, oldCommand, canonicalLegacy)
			}
			requireHookReviewCodes(t, report, nativeHookReviewCodesFor(layout.target, layout.machineScope, false))
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			legacyAfter, err := os.ReadFile(legacyPath)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(legacyAfter, []byte(oldCommand)) || bytes.Contains(legacyAfter, []byte("_managed_by")) || !bytes.Contains(legacyAfter, []byte("unrelated")) {
				t.Fatalf("legacy file = %s, want the marked entry removed and unrelated content kept", legacyAfter)
			}

			report = runHookReview(t, repo, layout.target, layout.machineScope, false, provider)
			if len(report.HookChanges) != 0 {
				t.Fatalf("repeat sync hook changes = %#v, want none", report.HookChanges)
			}
			requireHookReviewCodes(t, report)
		})
	}
}

// spec: hook-resources/changed-hook-commands-are-listed-with-exact-values
// spec: hook-resources/unchanged-hooks-produce-no-review
func TestNativeHookLegacyMixedGroupListsOnlyRemovedHandlers(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	layout := nativeHookJSONCaseByName(t, "factory_repo")
	provider := additiveSafetyProvider(t, layout.target, false, "echo zqx-native-current-mixed", "")
	if report := runHookReview(t, repo, layout.target, false, false, provider); len(report.HookChanges) != 1 {
		t.Fatalf("seed sync hook changes = %#v, want the added native row", report.HookChanges)
	}
	legacyPath := legacyHookFileFor(layout.target, false, repo, "")
	additiveSafetyWrite(t, legacyPath, `{"PreToolUse":[
  {"matcher":"Execute","hooks":[{"type":"command","command":"echo zqx-legacy-marked-9a11","_managed_by":"agent-marketplace"},{"type":"command","command":"echo zqx-handwritten-9a11"}]},
  {"matcher":"Other","hooks":[{"type":"command","command":"echo zqx-sibling-9a11"}]}
],"unrelated":{"keep":true}}`+"\n")
	if err := os.Chmod(legacyPath, 0o640); err != nil {
		t.Fatal(err)
	}

	report := runHookReview(t, repo, layout.target, false, false, provider)
	if len(report.HookChanges) != 1 {
		t.Fatalf("mixed sync hook changes = %#v, want exactly one removal row", report.HookChanges)
	}
	row := report.HookChanges[0]
	if row.Event != "PreToolUse" || row.Matcher != "Execute" || len(row.After) != 0 {
		t.Fatalf("row = %#v, want the marked handler removal under Execute", row)
	}
	var before struct {
		Hooks []map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(row.Before, &before); err != nil || len(before.Hooks) != 1 {
		t.Fatalf("row before = %s err %v, want a matcher group with exactly the removed handler", row.Before, err)
	}
	if !bytes.Contains(row.Before, []byte("zqx-legacy-marked-9a11")) || bytes.Contains(row.Before, []byte("zqx-handwritten-9a11")) || bytes.Contains(row.Before, []byte("zqx-sibling-9a11")) {
		t.Fatalf("row before = %s, want only the marked removed handler", row.Before)
	}
	if _, ok := before.Hooks[0]["_managed_by"]; !ok {
		t.Fatalf("removed handler = %s, the canonical legacy bytes must be carried verbatim", before.Hooks[0])
	}
	legacyAfter, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(legacyAfter, []byte("zqx-legacy-marked-9a11")) || bytes.Contains(legacyAfter, []byte("_managed_by")) {
		t.Fatalf("legacy file = %s, want the marked handler removed", legacyAfter)
	}
	for _, keep := range []string{"zqx-handwritten-9a11", "zqx-sibling-9a11", `"unrelated"`} {
		if !bytes.Contains(legacyAfter, []byte(keep)) {
			t.Fatalf("legacy file = %s, handwritten %s must be preserved", legacyAfter, keep)
		}
	}
	requireHookReviewCodes(t, report, diag.CodeMaterializeHookReview)
}

// spec: hook-resources/unchanged-hooks-produce-no-review
// spec: hook-resources/check-reports-pending-hook-changes-without-writing
func TestNativeHookLegacyRemovalSuppressedOnRefusalFaultIncomplete(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	key := outputKey{target: hcc.TargetFactory}
	destination, file, err := nativeHookJSONLayout(key, repo)
	if err != nil {
		t.Fatal(err)
	}
	nativePath := filepath.Join(destination.Root, file.rel)
	legacyPath := legacyHookFileFor(hcc.TargetFactory, false, repo, "")
	newWork := func() *ownedJSONAggregateWork {
		entries, err := ownedJSONHookEntries(file.rel, map[string][]json.RawMessage{
			"PreToolUse": {json.RawMessage(`{"matcher":"Execute","hooks":[{"type":"command","command":"echo zqx-fault-native"}]}`)},
		}, "selected")
		if err != nil {
			t.Fatal(err)
		}
		for i := range entries {
			entries[i].sourceDigest = "sha256:" + strings.Repeat("a", 64)
		}
		work := &ownedJSONAggregateWork{}
		for _, entry := range entries {
			work.add(entry)
		}
		return work
	}
	seedLegacy := func() {
		legacyDoc, _ := nativeHookLegacyDocument(hcc.TargetFactory, "", "PreToolUse", "Execute", "echo zqx-legacy-fault-old", false, true)
		additiveSafetyWrite(t, legacyPath, string(legacyDoc))
		if err := os.Chmod(legacyPath, 0o640); err != nil {
			t.Fatal(err)
		}
	}

	var finalizeDiags []diag.Diagnostic
	finalize := nativeHookActivationFinalizer(key, repo, nil, false, func() {}, &finalizeDiags)
	optionsFor := func() ownedJSONReconcileOptions {
		options := nativeHookReconcileOptionsFor(nil)
		options.nativeHookFinalize = finalize
		return options
	}
	report := reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, newWork(), optionsFor(), false)
	if report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("seed reconcile diagnostics = %v", report)
	}
	seededNative, nativeMode, nativeMtime := fileSnapshot(t, nativePath)

	incompleteWork := newWork()
	incompleteWork.markIncomplete(file.rel)
	seedLegacy()
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyPath)
	report = reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, incompleteWork, optionsFor(), false)
	if report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("incomplete reconcile diagnostics = %v", report)
	}
	if len(report.HookChanges) != 0 {
		t.Fatalf("incomplete run hook changes = %#v, want none — legacy entries were preserved", report.HookChanges)
	}
	requireHookReviewCodes(t, report)
	assertFileUnchanged(t, legacyPath, legacyBefore, legacyMode, legacyMtime)
	assertFileUnchanged(t, nativePath, seededNative, nativeMode, nativeMtime)

	additiveSafetyWrite(t, legacyPath, "{ not valid json\n")
	report = reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, newWork(), optionsFor(), false)
	if report == nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("malformed legacy diagnostics = %v, want a refusal", report)
	}
	if len(report.HookChanges) != 0 {
		t.Fatalf("malformed legacy hook changes = %#v, want none", report.HookChanges)
	}
	requireHookReviewCodes(t, report)

	seedLegacy()
	legacyBefore, legacyMode, legacyMtime = fileSnapshot(t, legacyPath)
	sentinel := errors.New("interrupted native plus legacy publication")
	options := optionsFor()
	options.nativeHookFinalize = func(ctx context.Context, store *ownership.Store, output outputKey, outDir string, aggregate ownedJSONFileSpec, abs string, raw []byte, readErr error, doc *jsonc.Document, plan ownedJSONPatch, proof ownership.Proof, proofErr error, incomplete, dryRun bool) (nativeHookTransactionExtras, error) {
		extras, err := finalize(ctx, store, output, outDir, aggregate, abs, raw, readErr, doc, plan, proof, proofErr, incomplete, dryRun)
		extras.commitOptions = append(extras.commitOptions, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
			if point.Stage == dependencytxn.FaultBeforePublish && point.Index == 0 {
				return sentinel
			}
			return nil
		}))
		return extras, err
	}
	faulted := reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, newWork(), options, false)
	if faulted == nil || !diag.HasCode(faulted.Diags, "materialize.write.error") {
		t.Fatalf("faulted reconcile diagnostics = %v, want materialize.write.error", faulted)
	}
	if len(faulted.HookChanges) != 0 {
		t.Fatalf("faulted reconcile hook changes = %#v, want none — nothing was published", faulted.HookChanges)
	}
	requireHookReviewCodes(t, faulted)
	assertFileUnchanged(t, legacyPath, legacyBefore, legacyMode, legacyMtime)
	assertFileUnchanged(t, nativePath, seededNative, nativeMode, nativeMtime)

	report = reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, newWork(), optionsFor(), false)
	if report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("recovery reconcile diagnostics = %v", report)
	}
	legacyAfter, err := os.ReadFile(legacyPath)
	if err != nil || bytes.Contains(legacyAfter, []byte("zqx-legacy-fault-old")) {
		t.Fatalf("recovered legacy file = %q, err %v — the persisted journal must let the cleanup converge", legacyAfter, err)
	}
}
