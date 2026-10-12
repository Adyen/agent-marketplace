package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func nativeHookLegacyEntry(target hcc.Target, event, matcher, command string, marked bool) string {
	marker := ""
	if marked {
		marker = `,"_managed_by":"agent-marketplace"`
	}
	switch target {
	case hcc.TargetFactory:
		return fmt.Sprintf(`{ "matcher" : %q , "hooks" : [ { "type" : "command" , "command" : %q } ] , "userExtra" : "keep"%s }`, matcher, command, marker)
	case hcc.TargetClaude:
		return fmt.Sprintf(`{ "type" : "command" , "command" : %q , "matcher" : %q , "userExtra" : "keep"%s }`, command, matcher, marker)
	case hcc.TargetCursor:
		return fmt.Sprintf(`{ "event" : %q , "command" : %q , "matcher" : %q , "type" : "command" , "userExtra" : "keep"%s }`, event, command, matcher, marker)
	case hcc.TargetWindsurf:
		return fmt.Sprintf(`{ "command" : %q , "userExtra" : "keep"%s }`, command, marker)
	default:
		panic("unsupported legacy hook fixture target")
	}
}

func nativeHookLegacyDocument(target hcc.Target, container, event, matcher, command string, includeHandwritten, includeUnrelated bool) ([]byte, string) {
	managed := nativeHookLegacyEntry(target, event, matcher, command, true)
	handwritten := nativeHookLegacyEntry(target, event, matcher, "echo handwritten", false)
	items := managed
	if includeHandwritten {
		items += ",\n      " + handwritten
	}
	eventArray := fmt.Sprintf("%q : [\n      %s\n    ]", event, items)
	member := eventArray
	if container != "" {
		member = fmt.Sprintf("%q : {\n    %s\n  }", container, eventArray)
	}
	fields := make([]string, 0, 2)
	if includeUnrelated {
		fields = append(fields, `"unrelated" : { "keep" : true }`)
	}
	fields = append(fields, member)
	return []byte("{\n  // keep legacy root formatting\n  " + strings.Join(fields, ",\n  ") + "\n}\n"), handwritten
}

func assertNativeHookMigrationBackup(t *testing.T, root, legacyPath string, original []byte) {
	t.Helper()
	canonicalDir, err := filepath.EvalSymlinks(filepath.Dir(legacyPath))
	if err != nil {
		t.Fatalf("canonical legacy directory: %v", err)
	}
	canonicalPath := filepath.Join(canonicalDir, filepath.Base(legacyPath))
	rel, err := filepath.Rel(root, canonicalPath)
	if err != nil {
		t.Fatalf("legacy backup relative path: %v", err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	backups, err := os.ReadDir(store.SafetyBackupDir(filepath.ToSlash(rel)))
	if err != nil || len(backups) == 0 {
		t.Fatalf("missing legacy safety backup: %v", err)
	}
	for _, backup := range backups {
		info, err := backup.Info()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(store.SafetyBackupDir(filepath.ToSlash(rel)), backup.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() == 0o600 && bytes.Equal(data, original) {
			return
		}
	}
	t.Fatal("no private safety backup preserved the original legacy bytes")
}

// spec: hook-resources/owned-factory-entries-move-to-the-native-file
// spec: hook-resources/owned-windsurf-entries-move-to-the-devin-desktop-workspace-file
func TestNativeHookLegacyMigrationMovesOwnedRules(t *testing.T) {
	cases := []struct {
		name, layout       string
		target             hcc.Target
		machine            bool
		preserveDevinHooks bool
	}{
		{name: "factory_repo", layout: "factory_repo", target: hcc.TargetFactory},
		{name: "factory_machine", layout: "factory_machine", target: hcc.TargetFactory, machine: true},
		{name: "claude_repo", layout: "claude_repo", target: hcc.TargetClaude},
		{name: "claude_machine", layout: "claude_machine", target: hcc.TargetClaude, machine: true},
		{name: "cursor_repo", layout: "cursor_repo", target: hcc.TargetCursor},
		{name: "cursor_machine", layout: "cursor_machine", target: hcc.TargetCursor, machine: true},
		{name: "windsurf_repo", layout: "windsurf_repo", target: hcc.TargetWindsurf, preserveDevinHooks: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, home, codexHome := nativeHookJSONTestEnvironment(t)
			layout := nativeHookJSONCaseByName(t, tc.layout)
			_, nativeRoot, _, nativePath := nativeHookMetadataTarget(t, layout, repo, home, codexHome)
			legacyPath := legacyHookFileFor(tc.target, tc.machine, repo, home)
			var devinHookPath string
			var devinHookBefore []byte
			var devinHookInfo os.FileInfo
			if tc.preserveDevinHooks {
				devinHookPath = filepath.Join(repo, ".devin", "hooks.v1.json")
				devinBytes := []byte(`{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"echo Devin hand-written hook"}]}]}`)
				additiveSafetyWrite(t, devinHookPath, string(devinBytes))
				if err := os.Chmod(devinHookPath, 0o640); err != nil {
					t.Fatal(err)
				}
				var readErr error
				devinHookBefore, readErr = os.ReadFile(devinHookPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				devinHookInfo, readErr = os.Stat(devinHookPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
			}
			legacy, handwritten := nativeHookLegacyDocument(tc.target, layout.container, layout.event, layout.matcher, "echo migrate", true, true)
			additiveSafetyWrite(t, legacyPath, string(legacy))
			if err := os.Chmod(legacyPath, 0o640); err != nil {
				t.Fatal(err)
			}
			legacyBefore, legacyMode, _ := fileSnapshot(t, legacyPath)
			provider := additiveSafetyProvider(t, tc.target, tc.machine, "echo migrate", "")
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("Run: %v / %v", err, report)
			}
			if got := additiveSafetyCommands(t, nativePath, layout.container, layout.event); !equalStrings(got, []string{"echo migrate"}) {
				t.Fatalf("native commands = %v", got)
			}
			nativeBytes, err := os.ReadFile(nativePath)
			if err != nil || bytes.Contains(nativeBytes, []byte("_managed_by")) {
				t.Fatalf("native aggregate contains a marker: %v / %s", err, nativeBytes)
			}
			legacyAfter, err := os.ReadFile(legacyPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(legacyAfter, []byte(handwritten)) || !bytes.Contains(legacyAfter, []byte("// keep legacy root formatting")) ||
				!bytes.Contains(legacyAfter, []byte(`"unrelated" : { "keep" : true }`)) || bytes.Contains(legacyAfter, []byte("_managed_by")) || bytes.Contains(legacyAfter, []byte("echo migrate")) {
				t.Fatalf("legacy migration did not preserve handwritten bytes/order and unrelated settings: %s", legacyAfter)
			}
			legacyInfo, err := os.Stat(legacyPath)
			if err != nil || legacyInfo.Mode().Perm() != legacyMode.Perm() {
				t.Fatalf("legacy mode changed: before=%o after=%v err=%v", legacyMode.Perm(), legacyInfo, err)
			}
			if tc.preserveDevinHooks {
				devinHookAfter, err := os.ReadFile(devinHookPath)
				devinHookInfoAfter, statErr := os.Stat(devinHookPath)
				if err != nil || statErr != nil || !bytes.Equal(devinHookBefore, devinHookAfter) || devinHookInfo.Mode() != devinHookInfoAfter.Mode() || !devinHookInfo.ModTime().Equal(devinHookInfoAfter.ModTime()) {
					t.Fatalf("Devin native hook changed during Windsurf migration: bytes=%q/%q info=%v/%v errors=%v/%v", devinHookBefore, devinHookAfter, devinHookInfo, devinHookInfoAfter, err, statErr)
				}
			}
			assertNativeHookMigrationBackup(t, nativeRoot, legacyPath, legacyBefore)
		})
	}
}

func canonicalTransactionTestPath(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return filepath.Clean(absolute)
	}
	return filepath.Join(parent, filepath.Base(absolute))
}

// spec: hook-resources/owned-windsurf-entries-move-to-the-devin-desktop-workspace-file
func TestDevinCleanupPreservesWindsurfNativeHookFile(t *testing.T) {
	repo, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	hook := ir.NewHookWithMatcherOptions("devin-managed", "PreToolUse", "Execute", "echo devin-managed", "command", nil, ir.SourceRef{}, ir.WithHookSource("factory"))
	resolved := ingestedHookFixture(t, config.RepoCommitted, hcc.TargetDevin, "devin-managed", hook, nil)
	if report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved}); err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Devin hook: %v / %v", err, report)
	}
	windsurfPath := filepath.Join(repo, ".devin", "hooks.json")
	windsurfBytes := []byte(`{"hooks":{"pre_run_command":[{"command":"echo preserved-windsurf"}]}}` + "\n")
	additiveSafetyWrite(t, windsurfPath, string(windsurfBytes))
	if err := os.Chmod(windsurfPath, 0o640); err != nil {
		t.Fatal(err)
	}
	before, mode, mtime := fileSnapshot(t, windsurfPath)
	if report, err := Run(context.Background(), repo, nil); err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Devin cleanup: %v / %v", err, report)
	}
	assertFileUnchanged(t, windsurfPath, before, mode, mtime)
	nativePath := filepath.Join(repo, ".devin", "hooks.v1.json")
	if data, err := os.ReadFile(nativePath); err == nil && bytes.Contains(data, []byte("echo devin-managed")) {
		t.Fatalf("Devin cleanup retained its deselected hook: %s", data)
	}
}

// spec: hook-resources/owned-factory-entries-move-to-the-native-file
// spec: hook-resources/deselected-hooks-remove-their-support-directory
func TestNativeHookLegacySupportManifestAndOwnershipRecoverInOneTransaction(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	layout := nativeHookJSONCaseByName(t, "factory_repo")
	destination, file, err := nativeHookJSONLayout(outputKey{target: hcc.TargetFactory}, repo)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := ownership.CanonicalRoot(destination.Root)
	if err != nil {
		t.Fatal(err)
	}
	nativePath := filepath.Join(destination.Root, file.rel)
	legacyPath := legacyHookFileFor(hcc.TargetFactory, false, repo, "")
	legacyBefore, handwritten := nativeHookLegacyDocument(hcc.TargetFactory, layout.container, layout.event, layout.matcher, "echo legacy", true, true)
	additiveSafetyWrite(t, legacyPath, string(legacyBefore))
	if err := os.Chmod(legacyPath, 0o640); err != nil {
		t.Fatal(err)
	}
	legacyBefore, err = os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	supportPath := filepath.Join(destination.Root, "hooks", "shared.sh")
	if err := os.WriteFile(supportPath, []byte("legacy support bytes\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadPortableState(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := portablePathDigest(supportPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.add(hcc.TargetFactory, "hooks/shared.sh", digest)
	manifestBytes, hasEntries, err := manifest.serialized()
	if err != nil || !hasEntries {
		t.Fatalf("serialize portable manifest: entries=%v err=%v", hasEntries, err)
	}
	if err := os.MkdirAll(filepath.Dir(manifest.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest.path, manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(manifest.path)
	if err != nil {
		t.Fatal(err)
	}
	hookGroups := map[string][]json.RawMessage{"PreToolUse": {json.RawMessage(`{"matcher":"Execute","hooks":[{"type":"command","command":"echo recovered"}]}`)}}
	hookEntries, err := ownedJSONHookEntries(file.rel, hookGroups, "transaction-hook")
	if err != nil {
		t.Fatal(err)
	}
	for i := range hookEntries {
		hookEntries[i].sourceDigest = "sha256:" + strings.Repeat("a", 64)
	}
	work := &ownedJSONAggregateWork{}
	for _, entry := range hookEntries {
		work.add(entry)
	}
	key := outputKey{target: hcc.TargetFactory}
	var mcpDiagnostics []diag.Diagnostic
	finalize := nativeHookActivationFinalizer(key, repo, nil, false, func() {}, &mcpDiagnostics)
	injected := errors.New("interrupted native plus legacy publication")
	options := nativeHookReconcileOptionsFor(nil)
	options.nativeHookFinalize = func(ctx context.Context, store *ownership.Store, output outputKey, outDir string, aggregate ownedJSONFileSpec, abs string, raw []byte, readErr error, doc *jsonc.Document, plan ownedJSONPatch, proof ownership.Proof, proofErr error, incomplete, dryRun bool) (nativeHookTransactionExtras, error) {
		extras, err := finalize(ctx, store, output, outDir, aggregate, abs, raw, readErr, doc, plan, proof, proofErr, incomplete, dryRun)
		extras.commitOptions = append(extras.commitOptions, dependencytxn.WithFaultHook(func(point dependencytxn.FaultPoint) error {
			if point.Stage == dependencytxn.FaultAfterPublish && point.Index == 0 {
				return injected
			}
			return nil
		}))
		return extras, err
	}
	report := reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, work, options, false)
	if !diag.HasCode(report.Diags, "materialize.write.error") {
		t.Fatalf("faulted migration diagnostics = %v, want transaction write error", report.Diags)
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	var journals []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dependencytxn-") && strings.HasSuffix(entry.Name(), ".json") {
			journals = append(journals, filepath.Join(stateDir, entry.Name()))
		}
	}
	if len(journals) != 1 {
		t.Fatalf("pending transaction journals = %v, want one", journals)
	}
	journalBytes, err := os.ReadFile(journals[0])
	if err != nil {
		t.Fatal(err)
	}
	var journal struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(journalBytes, &journal); err != nil {
		t.Fatal(err)
	}
	journalPaths := make(map[string]bool, len(journal.Files))
	for _, participant := range journal.Files {
		journalPaths[participant.Path] = true
	}
	store, err := ownership.Open(destination.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, participant := range []string{nativePath, store.StateFile(), legacyPath, supportPath, manifest.path} {
		canonical := canonicalTransactionTestPath(t, participant)
		if !journalPaths[canonical] {
			t.Fatalf("transaction journal omitted participant %q: %v", canonical, journalPaths)
		}
	}
	if len(journalPaths) != 5 {
		t.Fatalf("transaction journal has %d changed participants, want 5: %v", len(journalPaths), journalPaths)
	}
	if err := dependencytxn.Recover(context.Background(), stateDir); err != nil {
		t.Fatalf("recover migration transaction: %v", err)
	}
	nativeBytes, err := os.ReadFile(nativePath)
	if err != nil || !bytes.Contains(nativeBytes, []byte("echo recovered")) || bytes.Contains(nativeBytes, []byte("_managed_by")) {
		t.Fatalf("recovered native hooks = %s / %v", nativeBytes, err)
	}
	legacyAfter, err := os.ReadFile(legacyPath)
	if err != nil || !bytes.Contains(legacyAfter, []byte(handwritten)) || bytes.Contains(legacyAfter, []byte("_managed_by")) {
		t.Fatalf("recovered legacy hooks = %s / %v", legacyAfter, err)
	}
	if _, err := os.Lstat(supportPath); !os.IsNotExist(err) {
		t.Fatalf("recovered support candidate still exists: %v", err)
	}
	if _, err := os.Lstat(manifest.path); !os.IsNotExist(err) {
		t.Fatalf("recovered portable manifest still exists: %v", err)
	}
	proof := nativeHookMetadataProof(t, store, file.rel)
	if proof.OwnedCount() != 1 {
		t.Fatalf("recovered native ownership entries = %+v", proof.OwnedIDs())
	}
	assertNativeHookMigrationBackup(t, canonicalRoot, legacyPath, legacyBefore)
	if !bytes.Contains(manifestBefore, []byte("shared.sh")) {
		t.Fatalf("portable manifest seed omitted the support ownership entry: %s", manifestBefore)
	}
}

// spec: hook-resources/deselected-hooks-remove-their-support-directory
func TestNativeHookLegacyCleanupRefusesChangedNativeAggregate(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	hook := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo selected", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook})
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("initial Run: %v / %v", err, report)
	}
	destination, file, err := nativeHookJSONLayout(outputKey{target: hcc.TargetFactory}, repo)
	if err != nil {
		t.Fatal(err)
	}
	nativePath := filepath.Join(destination.Root, file.rel)
	nativeBefore, err := os.ReadFile(nativePath)
	if err != nil {
		t.Fatal(err)
	}
	var nativeGroups map[string][]json.RawMessage
	if err := json.Unmarshal(nativeBefore, &nativeGroups); err != nil {
		t.Fatal(err)
	}
	entries, err := ownedJSONHookEntries(file.rel, nativeGroups, hook.Name)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownership.Open(destination.Root)
	if err != nil {
		t.Fatal(err)
	}
	proof := nativeHookMetadataProof(t, store, file.rel)
	var sourceEntry ownership.Entry
	for _, id := range proof.OwnedIDs() {
		if id.Kind != ownership.KindHook {
			continue
		}
		candidate, ok := proof.Entry(id)
		if ok && candidate.Resource == hook.Name {
			sourceEntry = candidate
			break
		}
	}
	if sourceEntry.SourceDigest == "" {
		t.Fatalf("seed Native hook proof for %q has no source digest: %+v", hook.Name, proof.OwnedIDs())
	}
	work := &ownedJSONAggregateWork{}
	for _, entry := range entries {
		entry.sourceDigest = sourceEntry.SourceDigest
		entry.sourceEvidence = append([]ownership.HookSourceEvidence(nil), sourceEntry.SourceEvidence...)
		work.add(entry)
	}
	legacyPath := legacyHookFileFor(hcc.TargetFactory, false, repo, "")
	legacy, _ := nativeHookLegacyDocument(hcc.TargetFactory, "", "PreToolUse", "Execute", "echo selected", true, true)
	additiveSafetyWrite(t, legacyPath, string(legacy))
	if err := os.Chmod(legacyPath, 0o640); err != nil {
		t.Fatal(err)
	}
	supportPath := filepath.Join(destination.Root, "hooks", "shared.sh")
	if err := os.WriteFile(supportPath, []byte("owned support bytes\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadPortableState(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	supportDigest, err := portablePathDigest(supportPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.add(hcc.TargetFactory, "hooks/shared.sh", supportDigest)
	if err := manifest.save(false); err != nil {
		t.Fatal(err)
	}
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyPath)
	supportBefore, supportMode, supportMtime := fileSnapshot(t, supportPath)
	manifestBefore, manifestMode, manifestMtime := fileSnapshot(t, manifest.path)
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
	key := outputKey{target: hcc.TargetFactory}
	options := nativeHookReconcileOptionsFor(nil)
	var mcpDiagnostics []diag.Diagnostic
	finalize := nativeHookActivationFinalizer(key, repo, nil, false, func() {}, &mcpDiagnostics)
	changedNative := []byte(`{"PostToolUse":[]}` + "\n")
	options.nativeHookFinalize = func(ctx context.Context, store *ownership.Store, output outputKey, outDir string, aggregate ownedJSONFileSpec, abs string, raw []byte, readErr error, doc *jsonc.Document, plan ownedJSONPatch, proof ownership.Proof, proofErr error, incomplete, dryRun bool) (nativeHookTransactionExtras, error) {
		extras, err := finalize(ctx, store, output, outDir, aggregate, abs, raw, readErr, doc, plan, proof, proofErr, incomplete, dryRun)
		if err != nil {
			return extras, err
		}
		if !extras.pending {
			t.Fatal("legacy fixture did not prepare a pending cleanup")
		}
		if doc.Changed() {
			t.Fatal("Native plan unexpectedly changed the aggregate before the injected write")
		}
		priorGeneration, priorDigest := ownedAggregatePriorForApply(raw, readErr, proof)
		stateFile, err := store.Prepare(ownership.Mutation{
			Rel:                   aggregate.rel,
			AbsFile:               abs,
			NextBytes:             doc.Bytes(),
			Entries:               append(append([]ownership.Entry(nil), plan.owned...), nativeRetainedNonHookEntries(plan.owned, proof, plan.reconciledKinds)...),
			Reason:                ownership.ReasonSync,
			ExpectPriorGeneration: priorGeneration,
			ExpectPriorDigest:     priorDigest,
		}, &plan.satisfactions)
		if err != nil || stateFile.Path != "" {
			t.Fatalf("Native plan expected no state-file participant: path=%q err=%v", stateFile.Path, err)
		}
		extras.beforeNativeAssertion = func() error {
			return os.WriteFile(nativePath, changedNative, 0o640)
		}
		return extras, nil
	}
	refused := reconcileNativeHookJSON(context.Background(), &Report{}, key, repo, work, options, false)
	if !diag.HasCode(refused.Diags, diag.CodeOwnershipGenerationConflict) {
		t.Fatalf("changed Native aggregate diagnostics = %v, want generation conflict", refused.Diags)
	}
	if got := readFileString(t, nativePath); got != string(changedNative) {
		t.Fatalf("concurrent Native aggregate change was overwritten: %q", got)
	}
	assertFileUnchanged(t, legacyPath, legacyBefore, legacyMode, legacyMtime)
	assertFileUnchanged(t, supportPath, supportBefore, supportMode, supportMtime)
	assertFileUnchanged(t, manifest.path, manifestBefore, manifestMode, manifestMtime)
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
}

type nativeHookLegacyCacheFixture struct {
	repoRoot, nativeRoot, nativePath, legacyPath string
	supportLink, cacheTarget, manifestPath       string
	hookV2                                       resolver.ResolvedArtifact
	handwritten                                  string
	manifestBytes                                []byte
}

type nativeHookSymlinkSnapshot struct {
	bytes      []byte
	target     string
	linkInfo   os.FileInfo
	targetInfo os.FileInfo
}

func seedNativeHookLegacyCacheFixture(t *testing.T) nativeHookLegacyCacheFixture {
	t.Helper()
	repoRoot, _, _ := nativeHookJSONTestEnvironment(t)
	hookV1 := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo native-v1", "")
	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{hookV1})
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed Native Hook Run: %v / %v", err, report)
	}
	destination, file, err := nativeHookJSONLayout(outputKey{target: hcc.TargetFactory}, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := legacyHookFileFor(hcc.TargetFactory, false, repoRoot, "")
	legacy, handwritten := nativeHookLegacyDocument(hcc.TargetFactory, "", "PreToolUse", "Execute", "echo native-v1", true, true)
	additiveSafetyWrite(t, legacyPath, string(legacy))
	if err := os.Chmod(legacyPath, 0o640); err != nil {
		t.Fatal(err)
	}
	cacheTarget := filepath.Join(cache.CacheDir(), "native-hook-manifest-fixture", "shared.sh")
	if err := os.MkdirAll(filepath.Dir(cacheTarget), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheTarget, []byte("cached support bytes\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	supportLink := filepath.Join(destination.Root, "hooks", "shared.sh")
	if err := os.Symlink(cacheTarget, supportLink); err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Lstat(supportLink)
	if err != nil {
		t.Fatal(err)
	}
	linkTarget, err := os.Readlink(supportLink)
	if err != nil {
		t.Fatal(err)
	}
	if !nativeHookCacheLinkContained(supportLink, linkTarget) {
		t.Fatal("legacy support fixture is not a contained cache link")
	}
	manifest, err := loadPortableState(repoRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	manifest.add(hcc.TargetFactory, "hooks/shared.sh", nativeHookSymlinkSnapshotDigest(linkInfo.Mode(), linkTarget))
	if err := manifest.save(false); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(manifest.path)
	if err != nil {
		t.Fatal(err)
	}
	return nativeHookLegacyCacheFixture{
		repoRoot: repoRoot, nativeRoot: destination.Root, nativePath: filepath.Join(destination.Root, file.rel),
		legacyPath: legacyPath, supportLink: supportLink, cacheTarget: cacheTarget,
		manifestPath: manifest.path, hookV2: additiveSafetyProvider(t, hcc.TargetFactory, false, "echo native-v2", ""),
		handwritten: handwritten, manifestBytes: manifestBytes,
	}
}

func snapshotNativeHookLegacyCacheLink(t *testing.T, path string) nativeHookSymlinkSnapshot {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	linkInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return nativeHookSymlinkSnapshot{bytes: data, target: target, linkInfo: linkInfo, targetInfo: targetInfo}
}

func assertNativeHookLegacyCacheLinkUnchanged(t *testing.T, path string, before nativeHookSymlinkSnapshot) {
	t.Helper()
	after := snapshotNativeHookLegacyCacheLink(t, path)
	if !bytes.Equal(before.bytes, after.bytes) || before.target != after.target || before.linkInfo.Mode() != after.linkInfo.Mode() || !before.linkInfo.ModTime().Equal(after.linkInfo.ModTime()) || before.targetInfo.Mode() != after.targetInfo.Mode() || !before.targetInfo.ModTime().Equal(after.targetInfo.ModTime()) {
		t.Fatalf("legacy cache link changed: before=%q %q %v %v after=%q %q %v %v", before.bytes, before.target, before.linkInfo, before.targetInfo, after.bytes, after.target, after.linkInfo, after.targetInfo)
	}
}

func assertNativeHookManifestRefusalPreservesFixture(t *testing.T, fixture nativeHookLegacyCacheFixture, manifestUnchanged func()) {
	t.Helper()
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, fixture.nativePath)
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, fixture.legacyPath)
	supportBefore := snapshotNativeHookLegacyCacheLink(t, fixture.supportLink)
	cacheBefore, cacheMode, cacheMtime := fileSnapshot(t, fixture.cacheTarget)
	store, err := ownership.Open(fixture.nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
	report, err := Run(context.Background(), fixture.repoRoot, []resolver.ResolvedArtifact{fixture.hookV2})
	if err != nil || report == nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("manifest refusal Run: %v / %v", err, report)
	}
	if !diag.HasCode(report.Diags, "materialize.manifest.error") && !diag.HasCode(report.Diags, diag.CodeOwnershipGenerationConflict) {
		t.Fatalf("manifest refusal diagnostics = %v", report.Diags)
	}
	assertFileUnchanged(t, fixture.nativePath, nativeBefore, nativeMode, nativeMtime)
	assertFileUnchanged(t, fixture.legacyPath, legacyBefore, legacyMode, legacyMtime)
	assertNativeHookLegacyCacheLinkUnchanged(t, fixture.supportLink, supportBefore)
	assertFileUnchanged(t, fixture.cacheTarget, cacheBefore, cacheMode, cacheMtime)
	assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
	manifestUnchanged()
}

// spec: hook-resources/owned-factory-entries-move-to-the-native-file
// spec: hook-resources/deselected-hooks-remove-their-support-directory
func TestNativeHookSupportManifestFailuresRefuseLegacyMigration(t *testing.T) {
	t.Run("corrupt manifest", func(t *testing.T) {
		fixture := seedNativeHookLegacyCacheFixture(t)
		corrupt := []byte("version: [unfinished\n")
		if err := os.WriteFile(fixture.manifestPath, corrupt, 0o600); err != nil {
			t.Fatal(err)
		}
		manifestBefore, manifestMode, manifestMtime := fileSnapshot(t, fixture.manifestPath)
		assertNativeHookManifestRefusalPreservesFixture(t, fixture, func() {
			assertFileUnchanged(t, fixture.manifestPath, manifestBefore, manifestMode, manifestMtime)
		})
	})

	t.Run("symlink manifest", func(t *testing.T) {
		fixture := seedNativeHookLegacyCacheFixture(t)
		outsidePath := filepath.Join(t.TempDir(), "manifest.yaml")
		if err := os.WriteFile(outsidePath, fixture.manifestBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(fixture.manifestPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsidePath, fixture.manifestPath); err != nil {
			t.Fatal(err)
		}
		linkText, err := os.Readlink(fixture.manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		outsideBefore, outsideMode, outsideMtime := fileSnapshot(t, outsidePath)
		assertNativeHookManifestRefusalPreservesFixture(t, fixture, func() {
			got, err := os.Readlink(fixture.manifestPath)
			if err != nil || got != linkText {
				t.Fatalf("unsafe manifest link changed: %q / %v", got, err)
			}
			assertFileUnchanged(t, outsidePath, outsideBefore, outsideMode, outsideMtime)
		})
	})

	t.Run("valid manifest migrates cache link", func(t *testing.T) {
		fixture := seedNativeHookLegacyCacheFixture(t)
		report, err := Run(context.Background(), fixture.repoRoot, []resolver.ResolvedArtifact{fixture.hookV2})
		if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
			t.Fatalf("healthy migration Run: %v / %v", err, report)
		}
		nativeAfter, err := os.ReadFile(fixture.nativePath)
		if err != nil || !bytes.Contains(nativeAfter, []byte("echo native-v2")) || bytes.Contains(nativeAfter, []byte("_managed_by")) {
			t.Fatalf("healthy Native Hook output = %s / %v", nativeAfter, err)
		}
		legacyAfter, err := os.ReadFile(fixture.legacyPath)
		if err != nil || !bytes.Contains(legacyAfter, []byte(fixture.handwritten)) || bytes.Contains(legacyAfter, []byte("_managed_by")) || bytes.Contains(legacyAfter, []byte("echo native-v1")) {
			t.Fatalf("healthy legacy migration = %s / %v", legacyAfter, err)
		}
		if _, err := os.Lstat(fixture.supportLink); !os.IsNotExist(err) {
			t.Fatalf("owned legacy cache link remains: %v", err)
		}
		if _, err := os.Lstat(fixture.manifestPath); !os.IsNotExist(err) {
			t.Fatalf("empty portable manifest remains: %v", err)
		}
		if _, err := os.Stat(fixture.cacheTarget); err != nil {
			t.Fatalf("cache source was removed with its installed link: %v", err)
		}
	})
}

func TestValidatePortableManifestSnapshotDetectsConflicts(t *testing.T) {
	newState := func(t *testing.T) (*portableState, []byte, dependencytxn.Generation, os.FileMode) {
		t.Helper()
		root := t.TempDir()
		path := filepath.Join(root, ".agents", "materialization-manifest.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte("version: 1\nentries: []\n")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		state, err := loadPortableState(root, false)
		if err != nil {
			t.Fatal(err)
		}
		read, generation, mode, err := dependencytxn.ReadSnapshot(path)
		if err != nil || !bytes.Equal(read, data) {
			t.Fatalf("manifest snapshot = %q / %v", read, err)
		}
		return state, data, generation, mode
	}
	expectConflict := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, dependencytxn.ErrGenerationConflict) {
			t.Fatalf("snapshot conflict error = %v, want ErrGenerationConflict", err)
		}
	}
	t.Run("matching", func(t *testing.T) {
		state, data, generation, mode := newState(t)
		if err := validatePortableManifestSnapshot(state, data, generation, mode); err != nil {
			t.Fatalf("matching manifest snapshot: %v", err)
		}
	})
	for _, tc := range []struct {
		name string
		read func(*testing.T, *portableState, []byte, os.FileMode) ([]byte, dependencytxn.Generation, os.FileMode)
	}{
		{name: "bytes", read: func(_ *testing.T, _ *portableState, data []byte, mode os.FileMode) ([]byte, dependencytxn.Generation, os.FileMode) {
			return []byte("version: 2\n"), dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(data)}, mode
		}},
		{name: "missing", read: func(_ *testing.T, _ *portableState, _ []byte, _ os.FileMode) ([]byte, dependencytxn.Generation, os.FileMode) {
			return nil, dependencytxn.Generation{}, 0
		}},
		{name: "mode", read: func(_ *testing.T, _ *portableState, data []byte, _ os.FileMode) ([]byte, dependencytxn.Generation, os.FileMode) {
			return data, dependencytxn.Generation{Exists: true, Digest: dependencytxn.Digest(data)}, 0o640
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, data, _, mode := newState(t)
			observed, generation, observedMode := tc.read(t, state, data, mode)
			expectConflict(t, validatePortableManifestSnapshot(state, observed, generation, observedMode))
		})
	}
	t.Run("appeared", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, ".agents", "materialization-manifest.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		state, err := loadPortableState(root, false)
		if err != nil {
			t.Fatal(err)
		}
		data := []byte("version: 1\nentries: []\n")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		read, generation, mode, err := dependencytxn.ReadSnapshot(path)
		if err != nil {
			t.Fatal(err)
		}
		expectConflict(t, validatePortableManifestSnapshot(state, read, generation, mode))
	})
	for _, tc := range []struct {
		name  string
		repin func(*testing.T, *portableState, []byte, os.FileMode)
	}{
		{name: "file-replaced-with-identical-bytes", repin: func(t *testing.T, state *portableState, data []byte, mode os.FileMode) {
			retained := state.path + ".retained"
			if err := os.Rename(state.path, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(state.path, data, mode.Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(state.path, mode.Perm()); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "parent-replaced-with-identical-manifest", repin: func(t *testing.T, state *portableState, data []byte, mode os.FileMode) {
			parent := filepath.Dir(state.path)
			parentInfo, err := os.Stat(parent)
			if err != nil {
				t.Fatal(err)
			}
			retained := parent + ".retained"
			if err := os.Rename(parent, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(parent, parentInfo.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, parentInfo.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(state.path, data, mode.Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(state.path, mode.Perm()); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "scope-replaced-with-identical-manifest", repin: func(t *testing.T, state *portableState, data []byte, mode os.FileMode) {
			root := state.repoRoot
			rootInfo, err := os.Stat(root)
			if err != nil {
				t.Fatal(err)
			}
			retained := root + ".retained"
			if err := os.Rename(root, retained); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, rootInfo.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, rootInfo.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Dir(state.path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(state.path, data, mode.Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(state.path, mode.Perm()); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, data, _, mode := newState(t)
			tc.repin(t, state, data, mode)
			read, generation, observedMode, err := dependencytxn.ReadSnapshot(state.path)
			if err != nil || !bytes.Equal(read, data) || generation.Digest != dependencytxn.Digest(data) || observedMode.Perm() != mode.Perm() {
				t.Fatalf("replacement fixture changed manifest bytes/generation/mode: %q / %+v / %v / %v", read, generation, observedMode, err)
			}
			expectConflict(t, validatePortableManifestSnapshot(state, read, generation, observedMode))
		})
	}
}

// spec: hook-resources/a-legacy-file-with-only-owned-entries-is-deleted
func TestNativeHookLegacyMigrationDeletesOnlyOwnedFile(t *testing.T) {
	repo, home, codexHome := nativeHookJSONTestEnvironment(t)
	layout := nativeHookJSONCaseByName(t, "claude_repo")
	_, nativeRoot, _, nativePath := nativeHookMetadataTarget(t, layout, repo, home, codexHome)
	legacyPath := legacyHookFileFor(hcc.TargetClaude, false, repo, home)
	legacy, _ := nativeHookLegacyDocument(hcc.TargetClaude, layout.container, layout.event, layout.matcher, "echo migrate", false, false)
	additiveSafetyWrite(t, legacyPath, string(legacy))
	legacyBefore, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	provider := additiveSafetyProvider(t, hcc.TargetClaude, false, "echo migrate", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}
	if _, err := os.Lstat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("only-owned legacy aggregate survived: %v", err)
	}
	if got := additiveSafetyCommands(t, nativePath, layout.container, layout.event); !equalStrings(got, []string{"echo migrate"}) {
		t.Fatalf("native commands = %v", got)
	}
	assertNativeHookMigrationBackup(t, nativeRoot, legacyPath, legacyBefore)
}

// spec: hook-resources/malformed-legacy-files-block-migration-without-changes
func TestNativeHookLegacyMalformedBlocksTargetAndPreservesSibling(t *testing.T) {
	repo, home, codexHome := nativeHookJSONTestEnvironment(t)
	layout := nativeHookJSONCaseByName(t, "cursor_repo")
	_, cursorRoot, _, cursorNative := nativeHookMetadataTarget(t, layout, repo, home, codexHome)
	cursorLegacy := legacyHookFileFor(hcc.TargetCursor, false, repo, home)
	additiveSafetyWrite(t, cursorLegacy, "{\n")
	cursorBytes := []byte(`{"version":1,"hooks":{"preToolUse":[{"command":"echo native prior","matcher":"^Shell$","type":"command"}]}}`)
	additiveSafetyWrite(t, cursorNative, string(cursorBytes))
	if err := os.Chmod(cursorNative, 0o640); err != nil {
		t.Fatal(err)
	}
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, cursorLegacy)
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, cursorNative)
	cursorStore, err := ownership.Open(cursorRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertPathAbsent(t, cursorStore.StateFile())
	cursor := additiveSafetyProvider(t, hcc.TargetCursor, false, "echo cursor", "")
	factory := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo sibling", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{cursor, factory})
	if err != nil {
		t.Fatal(err)
	}
	if !diag.HasCode(report.Diags, diag.CodeMaterializeHookLegacyMalformed) {
		t.Fatalf("missing legacy-malformed diagnostic: %+v", report.Diags)
	}
	blocked := false
	for _, resource := range report.BlockedResources {
		if resource.Target == hcc.TargetCursor && resource.Name == cursor.Name {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("malformed Cursor hook was not blocked: %+v", report.BlockedResources)
	}
	assertFileUnchanged(t, cursorLegacy, legacyBefore, legacyMode, legacyMtime)
	assertFileUnchanged(t, cursorNative, nativeBefore, nativeMode, nativeMtime)
	assertPathAbsent(t, cursorStore.StateFile())
	factoryNative := filepath.Join(repo, ".factory", "hooks.json")
	if got := additiveSafetyCommands(t, factoryNative, "", "PreToolUse"); !equalStrings(got, []string{"echo sibling"}) {
		t.Fatalf("independent Factory native commands = %v", got)
	}
	assertPathAbsent(t, legacyHookFileFor(hcc.TargetFactory, false, repo, home))
}

// spec: hook-resources/check-reports-a-pending-migration
func TestNativeHookLegacyMigrationDryRunReportsPendingWithoutWrites(t *testing.T) {
	repo, home, codexHome := nativeHookJSONTestEnvironment(t)
	layout := nativeHookJSONCaseByName(t, "factory_repo")
	_, _, _, nativePath := nativeHookMetadataTarget(t, layout, repo, home, codexHome)
	legacyPath := legacyHookFileFor(hcc.TargetFactory, false, repo, home)
	legacy, _ := nativeHookLegacyDocument(hcc.TargetFactory, layout.container, layout.event, layout.matcher, "echo pending", true, true)
	additiveSafetyWrite(t, legacyPath, string(legacy))
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyPath)
	repoBefore := satisfactionPathTree(t, repo)
	stateBefore := satisfactionPathTree(t, filepath.Join(home, "state"))
	cacheBefore := satisfactionPathTree(t, filepath.Join(home, "cache"))
	provider := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo pending", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider}, WithDryRun())
	if err != nil {
		t.Fatalf("dry Run: %v", err)
	}
	if report.InSync() || report.TotalWritten()+report.TotalRemoved() == 0 {
		t.Fatalf("dry run did not report pending migration: %+v", report)
	}
	assertFileUnchanged(t, legacyPath, legacyBefore, legacyMode, legacyMtime)
	assertPathAbsent(t, nativePath)
	if !equalStrings(repoBefore, satisfactionPathTree(t, repo)) || !equalStrings(stateBefore, satisfactionPathTree(t, filepath.Join(home, "state"))) || !equalStrings(cacheBefore, satisfactionPathTree(t, filepath.Join(home, "cache"))) {
		t.Fatal("dry migration created filesystem, state, backup, cache, or temporary residue")
	}
}

// spec: hook-resources/deselected-hooks-remove-their-support-directory
func TestNativeHookLegacySupportCleanupUsesOwnershipEvidence(t *testing.T) {
	cases := []struct {
		name        string
		kind        string
		manifest    string
		wantRemoved bool
	}{
		{name: "cache link", kind: "cache-link", wantRemoved: true},
		{name: "foreign link", kind: "foreign-link"},
		{name: "dangling cache link", kind: "dangling-link"},
		{name: "matching portable digest", kind: "regular", manifest: "matching", wantRemoved: true},
		{name: "drifted portable digest", kind: "regular", manifest: "drifted"},
		{name: "digestless portable entry", kind: "regular", manifest: "digestless"},
		{name: "unmarked regular file", kind: "regular"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := additiveSafetyEnv(t)
			nativeRoot := filepath.Join(repo, ".factory")
			legacyPath := filepath.Join(nativeRoot, "hooks", "hooks.json")
			legacy, _ := nativeHookLegacyDocument(hcc.TargetFactory, "", "PreToolUse", "Execute", "./shared.sh", false, false)
			additiveSafetyWrite(t, legacyPath, string(legacy))
			supportPath := filepath.Join(filepath.Dir(legacyPath), "shared.sh")
			var cacheTarget string
			var beforeBytes []byte
			var beforeMode os.FileMode
			var beforeLink string
			switch tc.kind {
			case "cache-link", "foreign-link", "dangling-link":
				if tc.kind == "cache-link" || tc.kind == "dangling-link" {
					cacheTarget = filepath.Join(cache.CacheDir(), "legacy-hooks", tc.name, "shared.sh")
				} else {
					cacheTarget = filepath.Join(t.TempDir(), "shared.sh")
				}
				if tc.kind != "dangling-link" {
					if err := os.MkdirAll(filepath.Dir(cacheTarget), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(cacheTarget, []byte("cached legacy script"), 0o755); err != nil {
						t.Fatal(err)
					}
					beforeBytes = []byte("cached legacy script")
				}
				if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(cacheTarget, supportPath); err != nil {
					t.Fatal(err)
				}
				info, err := os.Lstat(supportPath)
				if err != nil {
					t.Fatal(err)
				}
				beforeMode = info.Mode()
				beforeLink, err = os.Readlink(supportPath)
				if err != nil {
					t.Fatal(err)
				}
			case "regular":
				beforeBytes = []byte("legacy support bytes")
				additiveSafetyWrite(t, supportPath, string(beforeBytes))
				if err := os.Chmod(supportPath, 0o640); err != nil {
					t.Fatal(err)
				}
				info, err := os.Lstat(supportPath)
				if err != nil {
					t.Fatal(err)
				}
				beforeMode = info.Mode()
				if tc.manifest != "" {
					state, err := loadPortableState(repo, false)
					if err != nil {
						t.Fatal(err)
					}
					digest := ""
					if tc.manifest == "matching" || tc.manifest == "drifted" {
						digest, err = portablePathDigest(supportPath)
						if err != nil {
							t.Fatal(err)
						}
					}
					state.add(hcc.TargetFactory, "hooks/shared.sh", digest)
					if err := state.save(false); err != nil {
						t.Fatal(err)
					}
					if tc.manifest == "drifted" {
						beforeBytes = []byte("drifted legacy support bytes")
						if err := os.WriteFile(supportPath, beforeBytes, 0o640); err != nil {
							t.Fatal(err)
						}
					}
				}
			default:
				t.Fatalf("unknown fixture kind %q", tc.kind)
			}
			provider := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo migrate", "")
			opts := []RunOption(nil)
			if tc.kind == "regular" {
				opts = []RunOption{WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile})}
			}
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider}, opts...)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("Run: %v / %v", err, report)
			}
			if got := additiveSafetyCommands(t, filepath.Join(nativeRoot, "hooks.json"), "", "PreToolUse"); !equalStrings(got, []string{"echo migrate"}) {
				t.Fatalf("native commands = %v", got)
			}
			if tc.wantRemoved {
				if _, err := os.Lstat(supportPath); !os.IsNotExist(err) {
					t.Fatalf("owned support candidate survived: %v", err)
				}
				if tc.kind == "cache-link" {
					if data, err := os.ReadFile(cacheTarget); err != nil || !bytes.Equal(data, beforeBytes) {
						t.Fatalf("cache referent changed: %q / %v", data, err)
					}
				}
			} else {
				info, err := os.Lstat(supportPath)
				if err != nil || info.Mode() != beforeMode {
					t.Fatalf("unproven support candidate changed: info=%v err=%v", info, err)
				}
				if beforeMode&os.ModeSymlink != 0 {
					target, err := os.Readlink(supportPath)
					if err != nil || target != beforeLink {
						t.Fatalf("unproven support link changed: %q / %v", target, err)
					}
					if len(beforeBytes) == 0 {
						if _, err := os.Stat(target); !os.IsNotExist(err) {
							t.Fatalf("dangling support target unexpectedly exists: %v", err)
						}
					} else if data, err := os.ReadFile(supportPath); err != nil || !bytes.Equal(data, beforeBytes) {
						t.Fatalf("foreign support referent changed: %q / %v", data, err)
					}
				} else if data, err := os.ReadFile(supportPath); err != nil || !bytes.Equal(data, beforeBytes) {
					t.Fatalf("unproven regular support changed: %q / %v", data, err)
				}
			}
			if tc.manifest != "" {
				state, err := loadPortableState(repo, false)
				if err != nil {
					t.Fatal(err)
				}
				_, owned := state.entry(hcc.TargetFactory, "hooks/shared.sh")
				if owned == tc.wantRemoved {
					t.Fatalf("portable manifest ownership = %v, want removed=%v", owned, tc.wantRemoved)
				}
			}
		})
	}
}

// spec: hook-resources/malformed-legacy-files-block-migration-without-changes
func TestNativeHookLegacyMigrationRejectsSymlinkedAncestor(t *testing.T) {
	repo, home := additiveSafetyEnv(t)
	nativeRoot := filepath.Join(repo, ".factory")
	outside := t.TempDir()
	legacyDoc, _ := nativeHookLegacyDocument(hcc.TargetFactory, "", "PreToolUse", "Execute", "echo legacy", false, false)
	legacyFile := filepath.Join(outside, "hooks.json")
	if err := os.WriteFile(legacyFile, legacyDoc, 0o644); err != nil {
		t.Fatal(err)
	}
	supportFile := filepath.Join(outside, "shared.sh")
	if err := os.WriteFile(supportFile, []byte("outside support"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(nativeRoot, "hooks")
	if err := os.Symlink(outside, legacyDir); err != nil {
		t.Fatal(err)
	}
	nativeFile := filepath.Join(nativeRoot, "hooks.json")
	nativePrior := []byte(`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo prior"}]}]}`)
	if err := os.WriteFile(nativeFile, nativePrior, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyBefore, legacyMode, legacyMtime := fileSnapshot(t, legacyFile)
	supportBefore, supportMode, supportMtime := fileSnapshot(t, supportFile)
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativeFile)
	linkBefore, err := os.Readlink(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	provider := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo selected", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{provider})
	if err != nil {
		t.Fatal(err)
	}
	if !diag.HasCode(report.Diags, "materialize.destination.error") || !diag.HasCode(report.Diags, diag.CodeMaterializeHookLegacyMalformed) {
		t.Fatalf("unsafe native legacy refusal lacks its early boundary and scoped hook diagnostics: %+v", report.Diags)
	}
	assertFileUnchanged(t, legacyFile, legacyBefore, legacyMode, legacyMtime)
	assertFileUnchanged(t, supportFile, supportBefore, supportMode, supportMtime)
	assertFileUnchanged(t, nativeFile, nativeBefore, nativeMode, nativeMtime)
	if linkAfter, err := os.Readlink(legacyDir); err != nil || linkAfter != linkBefore {
		t.Fatalf("symlinked ancestor changed: %q / %v", linkAfter, err)
	}
	assertPathAbsent(t, filepath.Join(home, "state", "agent-marketplace", "ownership"))
	t.Run("non-hook-boundary-error-does-not-report-legacy-refusal", func(t *testing.T) {
		nonHookHome, nonHookRepo, outside := t.TempDir(), t.TempDir(), t.TempDir()
		t.Setenv("HOME", nonHookHome)
		t.Setenv("CODEX_HOME", filepath.Join(nonHookHome, ".codex"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(nonHookHome, "state"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(nonHookHome, "cache"))
		factoryRoot := filepath.Join(nonHookRepo, ".factory")
		if err := os.MkdirAll(factoryRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(outside, "foreign.txt")
		if err := os.WriteFile(foreign, []byte("foreign skill bytes"), 0o640); err != nil {
			t.Fatal(err)
		}
		skillsLink := filepath.Join(factoryRoot, "skills")
		if err := os.Symlink(outside, skillsLink); err != nil {
			t.Fatal(err)
		}
		linkBefore, err := os.Lstat(skillsLink)
		if err != nil {
			t.Fatal(err)
		}
		linkTarget, err := os.Readlink(skillsLink)
		if err != nil {
			t.Fatal(err)
		}
		foreignBefore, foreignMode, foreignMtime := fileSnapshot(t, foreign)
		foreignInfo, err := os.Stat(foreign)
		if err != nil {
			t.Fatal(err)
		}
		skill := ir.NewSkill("boundary-probe", "Boundary probe", "inert skill body", nil, nil, ir.SourceRef{})
		resolved := []resolver.ResolvedArtifact{{
			Entry:  catalog.Entry{Name: "boundary-probe", Kind: ir.KindSkill, Resource: skill, Path: "skills/boundary-probe/SKILL.md"},
			Source: config.RepoCommitted, Harnesses: []hcc.Target{hcc.TargetFactory},
		}}
		report, err := Run(t.Context(), nonHookRepo, resolved)
		if err != nil || report == nil || !diag.HasCode(report.Diags, "materialize.destination.error") {
			t.Fatalf("unsafe non-hook destination was not refused: %+v %v", report, err)
		}
		if diag.HasCode(report.Diags, diag.CodeMaterializeHookLegacyMalformed) {
			t.Fatalf("non-hook destination received Native legacy diagnostic: %+v", report.Diags)
		}
		linkAfter, err := os.Lstat(skillsLink)
		if err != nil || !os.SameFile(linkBefore, linkAfter) || linkBefore.Mode() != linkAfter.Mode() || !linkBefore.ModTime().Equal(linkAfter.ModTime()) {
			t.Fatalf("non-hook destination alias changed: %v", err)
		}
		if target, err := os.Readlink(skillsLink); err != nil || target != linkTarget {
			t.Fatalf("non-hook destination alias target changed: %q %v", target, err)
		}
		assertFileUnchanged(t, foreign, foreignBefore, foreignMode, foreignMtime)
		foreignAfter, err := os.Stat(foreign)
		if err != nil || !os.SameFile(foreignInfo, foreignAfter) {
			t.Fatalf("foreign non-hook payload identity changed: %v", err)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
