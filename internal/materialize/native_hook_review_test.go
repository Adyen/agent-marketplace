package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func hookReviewProvider(t *testing.T, registryRoot, name, command string) resolver.ResolvedArtifact {
	t.Helper()
	additiveSafetyWrite(t, filepath.Join(registryRoot, "hooks", "hooks.json"),
		fmt.Sprintf(`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":%q}]}]}`, command))
	cat, err := catalog.Load(registryRoot, "factory")
	if err != nil {
		t.Fatalf("catalog %s: %v", registryRoot, err)
	}
	if len(cat.Entries) != 1 {
		t.Fatalf("catalog %s entries = %v, want exactly one hook", registryRoot, cat.Entries)
	}
	entry := cat.Entries[0]
	entry.Name = name
	return resolver.ResolvedArtifact{Entry: entry, RegistryURL: registryRoot, CatalogKey: registryRoot}
}

func hookReviewDiagCodes(report *Report) []string {
	var codes []string
	for _, d := range report.Diags {
		switch d.Code {
		case diag.CodeMaterializeHookReview, diag.CodeMaterializeHookReviewPending,
			diag.CodeMaterializeCodexTrustReview, diag.CodeMaterializeCodexTrustReviewPending:
			codes = append(codes, d.Code)
		}
	}
	sort.Strings(codes)
	return codes
}

func requireHookReviewCodes(t *testing.T, report *Report, want ...string) {
	t.Helper()
	got := hookReviewDiagCodes(report)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("review diagnostics = %v, want %v (all diags: %v)", got, want, report.Diags)
	}
}

func nonReviewDiags(report *Report) []diag.Diagnostic {
	var kept []diag.Diagnostic
	for _, d := range report.Diags {
		switch d.Code {
		case diag.CodeMaterializeHookReview, diag.CodeMaterializeHookReviewPending,
			diag.CodeMaterializeCodexTrustReview, diag.CodeMaterializeCodexTrustReviewPending:
			continue
		}
		kept = append(kept, d)
	}
	return kept
}

func nativeHookReviewCodesFor(target hcc.Target, machine, dryRun bool) string {
	if target == hcc.TargetCodex && !machine {
		if dryRun {
			return diag.CodeMaterializeCodexTrustReviewPending
		}
		return diag.CodeMaterializeCodexTrustReview
	}
	if dryRun {
		return diag.CodeMaterializeHookReviewPending
	}
	return diag.CodeMaterializeHookReview
}

func hookReviewFile(loc nativeSupportLocation, repo, home string, machine bool) string {
	base := repo
	rel := loc.repoFile
	if machine {
		base = home
		rel = loc.userFile
	}
	resolved, err := filepath.EvalSymlinks(base)
	if err == nil {
		base = resolved
	}
	return filepath.Join(base, filepath.FromSlash(rel))
}

func findHookChange(t *testing.T, changes []HookChange, marker string) HookChange {
	t.Helper()
	var found []HookChange
	for _, change := range changes {
		if bytes.Contains(change.Before, []byte(marker)) || bytes.Contains(change.After, []byte(marker)) {
			found = append(found, change)
		}
	}
	if len(found) != 1 {
		t.Fatalf("hook change rows matching %q = %d, want 1 (rows: %#v)", marker, len(found), changes)
	}
	return found[0]
}

func requireHookChangeRow(t *testing.T, change HookChange, loc nativeSupportLocation, machine bool, file, beforeMarker, afterMarker string) {
	t.Helper()
	if change.Target != loc.target || change.MachineScope != machine {
		t.Fatalf("row target/scope = %s/%t, want %s/%t", change.Target, change.MachineScope, loc.target, machine)
	}
	if change.File != file {
		t.Fatalf("row file = %q, want %q", change.File, file)
	}
	if change.Event != loc.event || change.Matcher != loc.matcher {
		t.Fatalf("row event/matcher = %q/%q, want %q/%q", change.Event, change.Matcher, loc.event, loc.matcher)
	}
	if beforeMarker == "" && len(change.Before) != 0 {
		t.Fatalf("row before payload = %s, want empty (added entry)", change.Before)
	}
	if beforeMarker != "" && !bytes.Contains(change.Before, []byte(beforeMarker)) {
		t.Fatalf("row before payload = %s, want it to contain %q", change.Before, beforeMarker)
	}
	if afterMarker == "" && len(change.After) != 0 {
		t.Fatalf("row after payload = %s, want empty (removed entry)", change.After)
	}
	if afterMarker != "" && !bytes.Contains(change.After, []byte(afterMarker)) {
		t.Fatalf("row after payload = %s, want it to contain %q", change.After, afterMarker)
	}
}

func runHookReview(t *testing.T, repo string, target hcc.Target, machine bool, dryRun bool, providers ...resolver.ResolvedArtifact) *Report {
	t.Helper()
	resolved := nativeSupportResolve(t, target, machine, providers)
	opts := []RunOption{}
	if dryRun {
		opts = append(opts, WithDryRun())
	}
	report, err := Run(context.Background(), repo, resolved, opts...)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run produced error diagnostics: %v", report.Diags)
	}
	return report
}

// spec: hook-resources/changed-hook-commands-are-listed-with-exact-values
// spec: hook-resources/check-reports-pending-hook-changes-without-writing
// spec: hook-resources/unchanged-hooks-produce-no-review
// spec: hook-resources/codex-keeps-its-trust-review-codes
func TestNativeHookReviewReportsEntryChanges(t *testing.T) {
	for _, loc := range nativeSupportLocations() {
		for _, machine := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/machine=%t", loc.target, machine), func(t *testing.T) {
				repo, home := additiveSafetyEnv(t)
				nativeFile := hookReviewFile(loc, repo, home, machine)
				registryA, registryB := t.TempDir(), t.TempDir()
				alphaV1 := hookReviewProvider(t, registryA, "review-alpha", "echo zqx-alpha-v1")
				betaV1 := hookReviewProvider(t, registryB, "review-beta", "echo zqx-beta-v1")
				reviewCode := nativeHookReviewCodesFor(loc.target, machine, false)
				pendingCode := nativeHookReviewCodesFor(loc.target, machine, true)

				report := runHookReview(t, repo, loc.target, machine, false, alphaV1, betaV1)
				if len(report.HookChanges) != 2 {
					t.Fatalf("initial sync hook changes = %#v, want two added rows", report.HookChanges)
				}
				requireHookChangeRow(t, findHookChange(t, report.HookChanges, "zqx-alpha-v1"), loc, machine, nativeFile, "", "zqx-alpha-v1")
				requireHookChangeRow(t, findHookChange(t, report.HookChanges, "zqx-beta-v1"), loc, machine, nativeFile, "", "zqx-beta-v1")
				requireHookReviewCodes(t, report, reviewCode)

				report = runHookReview(t, repo, loc.target, machine, false, alphaV1, betaV1)
				if len(report.HookChanges) != 0 {
					t.Fatalf("repeat sync hook changes = %#v, want none", report.HookChanges)
				}
				requireHookReviewCodes(t, report)

				before, mode, mtime := fileSnapshot(t, nativeFile)
				alphaV2 := hookReviewProvider(t, registryA, "review-alpha", "echo zqx-alpha-v2")
				dry := runHookReview(t, repo, loc.target, machine, true, alphaV2, betaV1)
				assertFileUnchanged(t, nativeFile, before, mode, mtime)
				if len(dry.HookChanges) != 1 {
					t.Fatalf("dry-run hook changes = %#v, want one changed row", dry.HookChanges)
				}
				requireHookChangeRow(t, findHookChange(t, dry.HookChanges, "zqx-alpha"), loc, machine, nativeFile, "zqx-alpha-v1", "zqx-alpha-v2")
				requireHookReviewCodes(t, dry, pendingCode)

				report = runHookReview(t, repo, loc.target, machine, false, alphaV2, betaV1)
				if len(report.HookChanges) != 1 {
					t.Fatalf("update sync hook changes = %#v, want one changed row", report.HookChanges)
				}
				requireHookChangeRow(t, findHookChange(t, report.HookChanges, "zqx-alpha"), loc, machine, nativeFile, "zqx-alpha-v1", "zqx-alpha-v2")
				requireHookReviewCodes(t, report, reviewCode)

				report = runHookReview(t, repo, loc.target, machine, false, alphaV2)
				if len(report.HookChanges) != 1 {
					t.Fatalf("deselect sync hook changes = %#v, want one removed row", report.HookChanges)
				}
				requireHookChangeRow(t, findHookChange(t, report.HookChanges, "zqx-beta-v1"), loc, machine, nativeFile, "zqx-beta-v1", "")
				requireHookReviewCodes(t, report, reviewCode)

				registryC := t.TempDir()
				alphaMirror := hookReviewProvider(t, registryC, "review-alpha", "echo zqx-alpha-v2")
				report = runHookReview(t, repo, loc.target, machine, false, alphaMirror)
				if len(report.HookChanges) != 0 {
					t.Fatalf("source-refresh sync hook changes = %#v, want none", report.HookChanges)
				}
				requireHookReviewCodes(t, report)

				raw, err := os.ReadFile(nativeFile)
				if err != nil {
					t.Fatalf("read native file: %v", err)
				}
				var indented bytes.Buffer
				if err := json.Indent(&indented, raw, "", "    "); err != nil {
					t.Fatalf("reindent native file: %v", err)
				}
				if err := os.WriteFile(nativeFile, indented.Bytes(), 0o644); err != nil {
					t.Fatalf("rewrite native file: %v", err)
				}
				report = runHookReview(t, repo, loc.target, machine, false, alphaMirror)
				if len(report.HookChanges) != 0 {
					t.Fatalf("reformat sync hook changes = %#v, want none", report.HookChanges)
				}
				requireHookReviewCodes(t, report)
			})
		}
	}
}

// spec: hook-resources/changed-hook-commands-are-listed-with-exact-values
func TestNativeHookReviewPromptPayloadListed(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	registry := t.TempDir()
	additiveSafetyWrite(t, filepath.Join(registry, "hooks", "hooks.json"),
		`{"UserPromptSubmit":[{"hooks":[{"type":"prompt","prompt":"zqx-prompt-review-42a1 do it"}]}]}`)
	cat, err := catalog.Load(registry, "claude")
	if err != nil || len(cat.Entries) != 1 {
		t.Fatalf("prompt catalog: %v %v", cat, err)
	}
	provider := resolver.ResolvedArtifact{Entry: cat.Entries[0], RegistryURL: registry, CatalogKey: registry}
	report := runHookReview(t, repo, hcc.TargetClaude, false, false, provider)
	row := findHookChange(t, report.HookChanges, "zqx-prompt-review-42a1")
	if len(row.Before) != 0 || !bytes.Contains(row.After, []byte("zqx-prompt-review-42a1 do it")) {
		t.Fatalf("prompt row = %#v, want an added row carrying the exact prompt", row)
	}
	requireHookReviewCodes(t, report, diag.CodeMaterializeHookReview)
}

// spec: hook-resources/unchanged-hooks-produce-no-review
func TestNativeHookReviewSiblingMcpChangeStaysSilent(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	report := runHookReview(t, repo, hcc.TargetGemini, false, false, cleanupMCPSelection(hcc.TargetGemini, "zqx-mcp-only")...)
	if len(report.HookChanges) != 0 {
		t.Fatalf("MCP-only sync hook changes = %#v, want none", report.HookChanges)
	}
	requireHookReviewCodes(t, report)
	settings, err := os.ReadFile(filepath.Join(repo, ".gemini", "settings.json"))
	if err != nil || !bytes.Contains(settings, []byte("zqx-mcp-only")) {
		t.Fatalf("gemini settings.json = %q, err %v — the MCP write must still happen", settings, err)
	}
}

// spec: hook-resources/unchanged-hooks-produce-no-review
func TestNativeHookReviewPublishFaultLeavesNoReview(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	key := outputKey{target: hcc.TargetGemini}
	_, file, err := nativeHookJSONLayout(key, repo)
	if err != nil {
		t.Fatal(err)
	}
	nativeFile := filepath.Join(repo, ".gemini", filepath.FromSlash(file.rel))
	newWork := func(command string) *ownedJSONAggregateWork {
		groups := map[string][]json.RawMessage{"BeforeTool": {json.RawMessage(
			fmt.Sprintf(`{"matcher":"run_shell_command","hooks":[{"type":"command","command":%q}]}`, command))}}
		entries, err := ownedJSONHookEntries(file.rel, groups, "selected")
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

	report := reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, newWork("echo zqx-fault-v1"), nativeHookReconcileOptionsFor(nil), false)
	if report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("initial reconcile diagnostics = %v", report)
	}
	written, err := os.ReadFile(nativeFile)
	if err != nil || !bytes.Contains(written, []byte("zqx-fault-v1")) {
		t.Fatalf("initial native file = %q, err %v", written, err)
	}
	before, mode, mtime := fileSnapshot(t, nativeFile)

	sentinel := errors.New("interrupted native hook publication")
	options := nativeHookReconcileOptionsFor(nil)
	var finalizeDiags []diag.Diagnostic
	finalize := nativeHookActivationFinalizer(key, repo, nil, false, func() {}, &finalizeDiags)
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
	faulted := reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, newWork("echo zqx-fault-v2"), options, false)
	if faulted == nil || !diag.HasCode(faulted.Diags, "materialize.write.error") {
		t.Fatalf("faulted reconcile diagnostics = %v, want materialize.write.error", faulted)
	}
	if len(faulted.HookChanges) != 0 {
		t.Fatalf("faulted reconcile hook changes = %#v, want none — nothing was published", faulted.HookChanges)
	}
	requireHookReviewCodes(t, faulted)
	assertFileUnchanged(t, nativeFile, before, mode, mtime)

	report = reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, newWork("echo zqx-fault-v2"), nativeHookReconcileOptionsFor(nil), false)
	if report == nil || diag.HasErrors(report.Diags) {
		t.Fatalf("recovery reconcile diagnostics = %v", report)
	}
	written, err = os.ReadFile(nativeFile)
	if err != nil || !bytes.Contains(written, []byte("zqx-fault-v2")) {
		t.Fatalf("recovered native file = %q, err %v — the persisted journal must let the mutation converge", written, err)
	}
}

func hookReviewSeedNativeFile(t *testing.T, loc nativeSupportLocation, path string, elements ...json.RawMessage) {
	t.Helper()
	eventBody, err := json.Marshal(elements)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]json.RawMessage{}
	if loc.target == hcc.TargetCursor {
		version, _ := json.Marshal(1)
		root["version"] = version
	}
	eventMap, err := json.Marshal(map[string]json.RawMessage{loc.event: eventBody})
	if err != nil {
		t.Fatal(err)
	}
	if loc.container != "" {
		root[loc.container] = eventMap
	} else {
		root[loc.event] = eventBody
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	additiveSafetyWrite(t, path, string(append(data, '\n')))
}

func hookReviewFirstElement(t *testing.T, loc nativeSupportLocation, path string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated native file: %v", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("decode generated native file: %v", err)
	}
	container := root
	if loc.container != "" {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(root[loc.container], &inner); err != nil {
			t.Fatalf("decode container %s: %v", loc.container, err)
		}
		container = inner
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(container[loc.event], &elements); err != nil || len(elements) == 0 {
		t.Fatalf("event %s elements = %v, err %v", loc.event, elements, err)
	}
	return elements[len(elements)-1]
}

// spec: hook-resources/unchanged-hooks-produce-no-review
func TestNativeHookReviewHandwrittenSatisfactionStaysSilent(t *testing.T) {
	for _, loc := range nativeSupportLocations() {
		t.Run(string(loc.target), func(t *testing.T) {
			scratchRepo, _ := additiveSafetyEnv(t)
			scratchRegistry := t.TempDir()
			alpha := hookReviewProvider(t, scratchRegistry, "review-alpha", "echo zqx-satisfied")
			runHookReview(t, scratchRepo, loc.target, false, false, alpha)
			scratchFile := hookReviewFile(loc, scratchRepo, "", false)
			element := hookReviewFirstElement(t, loc, scratchFile)

			repo, _ := additiveSafetyEnv(t)
			nativeFile := hookReviewFile(loc, repo, "", false)
			hookReviewSeedNativeFile(t, loc, nativeFile, element)
			seededBefore, seededMode, seededMtime := fileSnapshot(t, nativeFile)

			report := runHookReview(t, repo, loc.target, false, false, alpha)
			if len(report.HookChanges) != 0 {
				t.Fatalf("satisfied sync hook changes = %#v, want none", report.HookChanges)
			}
			requireHookReviewCodes(t, report)
			var satisfied int
			for _, d := range report.Diags {
				if strings.Contains(d.Code, "satisfied") {
					satisfied++
				}
			}
			if satisfied == 0 {
				t.Fatalf("satisfied sync produced no satisfied diagnostic; the entry may not have been recognized: %v", report.Diags)
			}
			assertFileUnchanged(t, nativeFile, seededBefore, seededMode, seededMtime)

			registryB := t.TempDir()
			beta := hookReviewProvider(t, registryB, "review-beta", "echo zqx-added")
			report = runHookReview(t, repo, loc.target, false, false, alpha, beta)
			if len(report.HookChanges) != 1 {
				t.Fatalf("mixed sync hook changes = %#v, want exactly the added row", report.HookChanges)
			}
			requireHookChangeRow(t, report.HookChanges[0], loc, false, nativeFile, "", "zqx-added")
			requireHookReviewCodes(t, report, nativeHookReviewCodesFor(loc.target, false, false))
		})
	}
}

// spec: hook-resources/diagnostics-never-quote-payloads-on-any-target
func TestNativeHookDiagnosticsAreValueFree(t *testing.T) {
	const (
		commandPayload = "echo zqx-cmd-5f27-distinctive"
		promptPayload  = "zqx-prompt-7d31 do the thing"
		matcherPayload = "zqxMatcher88"
		supportPayload = "zqx-support-9c4e distinctive body"
		timeoutPayload = "-170091"
		refusedCommand = "echo zqx-refused-7e92"
	)
	payloads := []string{commandPayload, promptPayload, matcherPayload, supportPayload, timeoutPayload, "zqx-refused-7e92"}

	var buf safeHookLogBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var allDiags []diag.Diagnostic
	runs := 0
	for _, loc := range nativeSupportLocations() {
		for _, machine := range []bool{false, true} {
			repo, home := additiveSafetyEnv(t)
			nativeFile := hookReviewFile(loc, repo, home, machine)
			okRegistry := t.TempDir()
			ok := hookReviewProvider(t, okRegistry, "zqx-ok-hook", commandPayload)
			additiveSafetyWrite(t, filepath.Join(okRegistry, "hooks", "shared.sh"), "#!/bin/sh\n"+supportPayload+"\n")

			badRegistry := t.TempDir()
			additiveSafetyWrite(t, filepath.Join(badRegistry, "hooks", "hooks.json"),
				fmt.Sprintf(`{"PreToolUse":[{"matcher":%q,"hooks":[{"type":"command","command":%q,"timeout":%s}]}]}`,
					matcherPayload, refusedCommand, timeoutPayload))
			badCat, err := catalog.Load(badRegistry, "factory")
			if err != nil || len(badCat.Entries) != 1 {
				t.Fatalf("refused-hook catalog: %v %v", badCat, err)
			}
			bad := resolver.ResolvedArtifact{Entry: badCat.Entries[0], RegistryURL: badRegistry, CatalogKey: badRegistry}
			bad.Name = "zqx-refused-hook"

			promptRegistry := t.TempDir()
			additiveSafetyWrite(t, filepath.Join(promptRegistry, "hooks", "hooks.json"),
				fmt.Sprintf(`{"UserPromptSubmit":[{"hooks":[{"type":"prompt","prompt":%q}]}]}`, promptPayload))
			promptCat, err := catalog.Load(promptRegistry, "claude")
			if err != nil || len(promptCat.Entries) == 0 {
				t.Fatalf("prompt catalog: %v %v", promptCat, err)
			}
			promptProvider := resolver.ResolvedArtifact{Entry: promptCat.Entries[0], RegistryURL: promptRegistry, CatalogKey: promptRegistry}
			promptProvider.Name = "zqx-prompt-hook"

			resolved := nativeSupportResolve(t, loc.target, machine, []resolver.ResolvedArtifact{ok, bad, promptProvider})
			report, err := Run(context.Background(), repo, resolved)
			if err != nil {
				t.Fatalf("Run %s machine=%t: %v", loc.target, machine, err)
			}
			if !hasErrorDiagnostics(report.Diags) {
				t.Fatalf("%s machine=%t: refused hook produced no error diagnostic; the refusal oracle would be vacuous: %v", loc.target, machine, report.Diags)
			}
			nativeData, err := os.ReadFile(nativeFile)
			if err != nil {
				t.Fatalf("%s machine=%t: read native file: %v", loc.target, machine, err)
			}
			if !bytes.Contains(nativeData, []byte(commandPayload)) {
				t.Fatalf("%s machine=%t: native file %q lacks the ok hook's command", loc.target, machine, nativeData)
			}
			if bytes.Contains(nativeData, []byte("zqx-refused-7e92")) {
				t.Fatalf("%s machine=%t: native file contains the refused command: %s", loc.target, machine, nativeData)
			}
			var sawOKRow bool
			for _, c := range report.HookChanges {
				if bytes.Contains(c.Before, []byte("zqx-refused-7e92")) || bytes.Contains(c.After, []byte("zqx-refused-7e92")) {
					t.Fatalf("refused hook produced a review row: %#v", c)
				}
				if bytes.Contains(c.After, []byte(commandPayload)) {
					sawOKRow = true
				}
			}
			if !sawOKRow {
				t.Fatalf("%s machine=%t: ok hook produced no added review row; the run did not reconcile: %#v", loc.target, machine, report.HookChanges)
			}
			allDiags = append(allDiags, report.Diags...)
			runs++
		}
	}
	if runs == 0 || len(allDiags) == 0 {
		t.Fatalf("payload runs produced no diagnostics at all; the assertion would be vacuous")
	}
	if buf.String() == "" {
		t.Fatalf("captured log is empty across %d runs; the log sweep would be vacuous", runs)
	}
	for _, d := range allDiags {
		serialized, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("diagnostic %s failed to serialize: %v", d.Code, err)
		}
		for _, payload := range payloads {
			if bytes.Contains(serialized, []byte(payload)) {
				t.Fatalf("diagnostic %s serialized form contains payload %q: %s", d.Code, payload, serialized)
			}
		}
	}
	for _, payload := range payloads {
		if strings.Contains(buf.String(), payload) {
			t.Fatalf("log records contain payload %q", payload)
		}
	}
}

type safeHookLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeHookLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeHookLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
