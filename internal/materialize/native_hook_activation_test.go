package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

func nativeHookActivationSupportPath(t *testing.T, resolved resolver.ResolvedArtifact, target hcc.Target, machine bool, root string) (hookSupportPlan, string) {
	t.Helper()
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: target, machineScope: machine}})
	plan := plans[0]
	if plan.err != nil || len(conflicts) != 0 || len(plan.artifacts) == 0 {
		t.Fatalf("prepareHookSupport = %+v / %v", plan, conflicts)
	}
	return plan, filepath.Join(root, filepath.FromSlash(plan.artifacts[0].Path))
}

// spec: hook-native-location-tolerance/satisfaction-proof-matches-every-contributor-exactly
func TestNativeHookActivationIdenticalContributorsUseSelectedAliases(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	first, _ := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "alpha")
	second, _ := namedHookFixture(t, config.RepoCommitted, hcc.TargetFactory, "beta")
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: first, target: hcc.TargetFactory}, {skill: second, target: hcc.TargetFactory}})
	if len(conflicts) != 0 || plans[0].err != nil || plans[1].err != nil || plans[0].sourceDigest == plans[1].sourceDigest {
		t.Fatalf("distinct captured contributors = %+v / %v", plans, conflicts)
	}
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{first, second})
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}
	nativePath := filepath.Join(repo, ".factory", "hooks.json")
	if got := additiveSafetyCommands(t, nativePath, "", "PreToolUse"); !reflect.DeepEqual(got, []string{sharedSourceCommand}) {
		t.Fatalf("identical contributors emitted %v, want one native rule", got)
	}
	store := nativeHookMetadataStore(t, filepath.Join(repo, ".factory"))
	proof := nativeHookMetadataProof(t, store, "hooks.json")
	if proof.OwnedCount() != 1 {
		t.Fatalf("owned entries = %d, want one collapsed native rule", proof.OwnedCount())
	}
	id := proof.OwnedIDs()[0]
	entry, ok := proof.Entry(id)
	if !ok {
		t.Fatalf("missing owned proof entry %+v", id)
	}
	if len(proof.HookSatisfactions()) != 0 {
		t.Fatalf("owned hook proof unexpectedly stored as a handwritten satisfaction: %+v", proof.HookSatisfactions())
	}
	sources := append([]ownership.HookSourceEvidence{{Resource: entry.Resource, SourceDigest: entry.SourceDigest}}, entry.SourceEvidence...)
	want := map[string]string{"alpha": plans[0].sourceDigest, "beta": plans[1].sourceDigest}
	for _, source := range sources {
		if want[source.Resource] != source.SourceDigest {
			t.Fatalf("unexpected contributor source: %+v", source)
		}
		delete(want, source.Resource)
	}
	if len(want) != 0 {
		t.Fatalf("missing contributor proof pairs: %v (sources=%+v)", want, sources)
	}
	frozen, err := frozenLegacyToleranceFragmentForSelection(first, hcc.TargetFactory, first.Resource.(*ir.Hook))
	if err != nil {
		t.Fatal(err)
	}
	key := outputKey{target: hcc.TargetFactory}
	inspection := nativeHookInspectionFor(map[outputKey]*nativeHookInspection{}, key, repo)
	for _, contributor := range []resolver.ResolvedArtifact{first, second} {
		legacyRoot := filepath.Join(repo, ".factory")
		remaining, err := suppressNativeSatisfiedHookRules(contributor, key, legacyRoot, inspection, []artifact.Artifact{frozen})
		if err != nil || len(remaining) != 0 {
			t.Fatalf("reader rejected contributor %q: remaining=%d err=%v", contributor.Name, len(remaining), err)
		}
	}
}

// spec: hook-native-location-tolerance/satisfaction-proof-matches-every-contributor-exactly
func TestNativeHookActivationInstalledAliasIsReaderProofIdentity(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	resolved, _ := marketplaceHookFixture(t, hcc.TargetFactory, "sha256:abababababababababababababababababababababababababababababababab")
	resolved.Name = "selected-entry"
	resolved.InstalledName = "installed-alias"
	canonicalName := resolved.Resource.Name()
	if resolved.Name == resolved.InstalledName || resolved.InstalledName == canonicalName || resolved.Name == canonicalName {
		t.Fatalf("fixture identities are not distinct: entry=%q installed=%q canonical=%q", resolved.Name, resolved.InstalledName, canonicalName)
	}
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetFactory}})
	if len(conflicts) != 0 || plans[0].err != nil || plans[0].sourceDigest == "" {
		t.Fatalf("prepareHookSupport = %+v / %v", plans, conflicts)
	}
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}
	if resolved.Resource.Name() != canonicalName {
		t.Fatalf("Run mutated canonical resource name: %q, want %q", resolved.Resource.Name(), canonicalName)
	}
	nativePath := filepath.Join(repo, ".factory", "hooks.json")
	if got := additiveSafetyCommands(t, nativePath, "", "PreToolUse"); !reflect.DeepEqual(got, []string{"./run.sh"}) {
		t.Fatalf("native commands = %v", got)
	}
	store := nativeHookMetadataStore(t, filepath.Join(repo, ".factory"))
	proof := nativeHookMetadataProof(t, store, "hooks.json")
	if proof.OwnedCount() != 1 || len(proof.OwnedIDs()) != 1 || len(proof.HookSatisfactions()) != 0 {
		t.Fatalf("native alias proof = owned %v, satisfactions %+v", proof.OwnedIDs(), proof.HookSatisfactions())
	}
	entry, ok := proof.Entry(proof.OwnedIDs()[0])
	if !ok || entry.Resource != resolved.InstalledName || entry.SourceDigest != plans[0].sourceDigest {
		t.Fatalf("proof entry = %+v, want resource %q and captured digest %q", entry, resolved.InstalledName, plans[0].sourceDigest)
	}
	key := outputKey{target: hcc.TargetFactory}
	inspection := nativeHookInspectionFor(map[outputKey]*nativeHookInspection{}, key, repo)
	fragment, err := frozenLegacyToleranceFragment(hcc.TargetFactory, resolved.Resource.(*ir.Hook))
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := suppressNativeSatisfiedHookRules(resolved, key, filepath.Join(repo, ".factory"), inspection, []artifact.Artifact{fragment})
	if err != nil || len(remaining) != 0 {
		t.Fatalf("reader did not consume installed-alias proof: remaining=%d err=%v", len(remaining), err)
	}
	assertPathAbsent(t, legacyHookFileFor(hcc.TargetFactory, false, repo, ""))
}

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
// spec: hook-resources/factory-hooks-use-the-standalone-native-file
func TestFactoryNativeHookPreservesStandaloneRootFormatting(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	nativeFile := filepath.Join(repo, ".factory", "hooks.json")
	seed := []byte(`{
  // keep Factory root formatting
  "PostToolUse" : [ { "matcher" : "Manual", "hooks" : [ { "type" : "command", "command" : "echo manual" } ] } ]
}
`)
	if err := os.MkdirAll(filepath.Dir(nativeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativeFile, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nativeFile, 0o640); err != nil {
		t.Fatal(err)
	}
	hook := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo factory-native", "")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}))
	if err != nil || diag.HasErrors(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}
	data, err := os.ReadFile(nativeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("// keep Factory root formatting")) || !bytes.Contains(data, []byte(`"PostToolUse" : [ { "matcher" : "Manual"`)) ||
		!bytes.Contains(data, []byte("echo manual")) || !bytes.Contains(data, []byte("echo factory-native")) || bytes.Contains(data, []byte("_managed_by")) {
		t.Fatalf("Factory native output did not preserve its standalone root data: %s", data)
	}
	doc, err := jsonc.Parse(nativeFile, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, wrapped := doc.Root().Member("hooks"); wrapped {
		t.Fatalf("Factory native output was wrapped in a hooks member: %s", data)
	}
	if got := doc.Root().Keys(); !reflect.DeepEqual(got, []string{"PostToolUse", "PreToolUse"}) {
		t.Fatalf("Factory root event order = %v", got)
	}
	info, err := os.Stat(nativeFile)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("Factory native mode changed: info=%v err=%v", info, err)
	}
}

// spec: hook-resources/repeat-sync-is-byte-identical
func TestFactoryOwnedNativeHookReformatIsRepairedWithoutDuplicate(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	nativeRoot := filepath.Join(repo, ".factory")
	nativePath := filepath.Join(nativeRoot, "hooks.json")
	manualRule := []byte(`{ "matcher" : "Manual", "hooks" : [ { "type" : "command", "command" : "echo handwritten" } ] }`)
	seed := []byte("{\n  // keep Factory root formatting\n  \"PreToolUse\" : [\n    " + string(manualRule) + "\n  ]\n}\n")
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativePath, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nativePath, 0o640); err != nil {
		t.Fatal(err)
	}
	resolved := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo factory-owned", "")
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetFactory}})
	if len(conflicts) != 0 || plans[0].err != nil || plans[0].sourceDigest == "" {
		t.Fatalf("Factory hook source plan = %+v / %v", plans, conflicts)
	}
	mode := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	if report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved}, WithMaterializationModes(mode)); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("initial owned Factory hook Run: %v / %v", err, report)
	}
	initial, err := os.ReadFile(nativePath)
	if err != nil || !bytes.Contains(initial, manualRule) || !bytes.Contains(initial, []byte("// keep Factory root formatting")) {
		t.Fatalf("initial native root did not preserve manual bytes: %s / %v", initial, err)
	}
	if bytes.Count(initial, []byte("echo factory-owned")) != 1 {
		t.Fatalf("initial owned hook count = %d: %s", bytes.Count(initial, []byte("echo factory-owned")), initial)
	}
	store, err := ownership.Open(nativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof("hooks.json")
	if err != nil || proof.OwnedCount() != 1 || len(proof.OwnedIDs()) != 1 {
		t.Fatalf("initial Factory ownership proof = %v / %+v", err, proof.OwnedIDs())
	}
	id := proof.OwnedIDs()[0]
	entryBefore, ok := proof.Entry(id)
	if !ok || entryBefore.Resource != resolved.Name || entryBefore.SourceDigest != plans[0].sourceDigest {
		t.Fatalf("initial Factory owned entry = %+v", entryBefore)
	}
	formatted := bytes.Replace(initial, []byte(`"matcher":"Execute"`), []byte(`"matcher" : "Execute"`), 1)
	if bytes.Equal(formatted, initial) {
		t.Fatalf("Native Factory rule did not have the expected Execute matcher: %s", initial)
	}
	if err := os.WriteFile(nativePath, formatted, 0o640); err != nil {
		t.Fatal(err)
	}
	inspection, err := inspectNativeHookFile(store, nativeRoot, hcc.TargetFactory, false)
	if err != nil {
		t.Fatalf("inspect reformatted owned hook: %v", err)
	}
	var cleanAfterReformat bool
	for _, native := range inspection.entries {
		cleanAfterReformat = cleanAfterReformat || native.ID == id && native.Status == ownership.StatusOwnedClean
	}
	if !cleanAfterReformat || !inspection.hookSatisfied(plans[0].sourceDigest, resolved.Name) {
		t.Fatalf("format-only change lost clean selected proof: %+v", inspection.entries)
	}
	if report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved}, WithMaterializationModes(mode)); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("reformatted owned Factory hook Run: %v / %v", err, report)
	}
	afterFormat, err := os.ReadFile(nativePath)
	if err != nil || bytes.Contains(afterFormat, []byte(`"matcher" : "Execute"`)) || bytes.Count(afterFormat, []byte(`"matcher":"Execute"`)) != 1 ||
		bytes.Count(afterFormat, []byte("echo factory-owned")) != 1 || !bytes.Contains(afterFormat, manualRule) || !bytes.Contains(afterFormat, []byte("// keep Factory root formatting")) {
		t.Fatalf("reformatted Factory hook was not repaired without changing handwritten bytes: %s / %v", afterFormat, err)
	}
	info, err := os.Stat(nativePath)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("reformatted Factory mode changed: %v / %v", info, err)
	}
	proof, err = store.Proof("hooks.json")
	if err != nil || proof.OwnedCount() != 1 {
		t.Fatalf("proof after formatting = %v / %+v", err, proof.OwnedIDs())
	}
	entryAfterFormat, ok := proof.Entry(id)
	if !ok || entryAfterFormat.ValueDigest != entryBefore.ValueDigest || entryAfterFormat.SourceDigest != entryBefore.SourceDigest {
		t.Fatalf("proof changed after equivalent reformat: before=%+v after=%+v", entryBefore, entryAfterFormat)
	}

	if report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved}, WithMaterializationModes(mode)); err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("repeat repaired Factory hook Run: %v / %v", err, report)
	}
	afterRepeat, err := os.ReadFile(nativePath)
	if err != nil || !bytes.Equal(afterFormat, afterRepeat) || bytes.Count(afterRepeat, []byte("echo factory-owned")) != 1 || !bytes.Contains(afterRepeat, manualRule) {
		t.Fatalf("repeated Factory hook sync changed the repaired aggregate: %s / %v", afterRepeat, err)
	}
	info, err = os.Stat(nativePath)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("repeated Factory hook sync changed mode: %v / %v", info, err)
	}
	proof, err = store.Proof("hooks.json")
	if err != nil || proof.OwnedCount() != 1 {
		t.Fatalf("proof after repeated sync = %v / %+v", err, proof.OwnedIDs())
	}
	entryAfterRepeat, ok := proof.Entry(id)
	if !ok || entryAfterRepeat.Resource != resolved.Name || entryAfterRepeat.ValueDigest != entryBefore.ValueDigest || entryAfterRepeat.SourceDigest != entryBefore.SourceDigest {
		t.Fatalf("proof after repeated sync = %+v, want original selected identity and semantic value", entryAfterRepeat)
	}
}

func geminiMCPProvider(t testing.TB, name, command string) resolver.ResolvedArtifact {
	t.Helper()
	sourceRoot := t.TempDir()
	definitionPath := filepath.Join(sourceRoot, ".mcp.json")
	definition, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		name: map[string]any{"command": command, "args": []string{"--stdio"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.LoadWithOptions(sourceRoot, "claude", catalog.IngestOptions{LegacyBareMCP: true})
	if err != nil {
		t.Fatalf("Claude MCP catalog: %v", err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].Kind != ir.KindMcpServer || loaded.Entries[0].Name != name {
		t.Fatalf("Claude MCP catalog entries = %+v, want %q", loaded.Entries, name)
	}
	entry := loaded.Entries[0]
	entry.Path = definitionPath
	return resolver.ResolvedArtifact{
		Entry:       entry,
		Source:      config.RepoCommitted,
		RegistryURL: sourceRoot,
		CatalogKey:  sourceRoot,
		V4Kind:      config.V4SourceDirectory,
		Harnesses:   []hcc.Target{hcc.TargetGemini},
	}
}

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
// spec: hook-resources/deselected-hook-is-removed-from-the-merged-file
func TestGeminiSharedNativeHooksAndMCPPreserveSiblingOwnership(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	nativeRoot := filepath.Join(repo, ".gemini")
	nativeFile := filepath.Join(nativeRoot, "settings.json")
	seed := []byte(`{
  // handwritten settings
  "unrelated" : { "keep" : true },
  "mcpServers" : { "handwritten" : { "command" : "manual-bin" } },
  "hooks" : { "AfterTool" : [ { "matcher" : "^manual$", "hooks" : [ { "type" : "command", "command" : "echo manual" } ] } ] }
}
`)
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativeFile, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nativeFile, 0o640); err != nil {
		t.Fatal(err)
	}
	hookV1 := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo hook-v1", "")
	mcpV1 := geminiMCPProvider(t, "managed-server", "server-v1")
	run := func(resolved ...resolver.ResolvedArtifact) {
		t.Helper()
		report, err := Run(context.Background(), repo, resolved, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}))
		if err != nil || diag.HasErrors(report.Diags) {
			t.Fatalf("Run: %v / %v", err, report)
		}
	}
	assertPreservedSettings := func() []byte {
		t.Helper()
		data, err := os.ReadFile(nativeFile)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range []string{"// handwritten settings", `"unrelated" : { "keep" : true }`, `"command" : "manual-bin"`, `"command" : "echo manual"`} {
			if !bytes.Contains(data, []byte(fragment)) {
				t.Fatalf("handwritten settings fragment %q was not preserved: %s", fragment, data)
			}
		}
		if bytes.Contains(data, []byte("_managed_by")) {
			t.Fatalf("native Gemini entries contain an in-file ownership marker: %s", data)
		}
		return data
	}
	ownedEntry := func(kind ownership.EntryKind, container, key string) (ownership.EntryID, ownership.Entry) {
		t.Helper()
		store := nativeHookMetadataStore(t, nativeRoot)
		proof := nativeHookMetadataProof(t, store, "settings.json")
		for _, id := range proof.OwnedIDs() {
			if id.Kind == kind && id.Container == container && id.Key == key {
				entry, ok := proof.Entry(id)
				if ok {
					return id, entry
				}
			}
		}
		t.Fatalf("missing owned Gemini entry kind=%s container=%s key=%s; proof=%+v", kind, container, key, proof.OwnedIDs())
		return ownership.EntryID{}, ownership.Entry{}
	}
	run(hookV1, mcpV1)
	first := assertPreservedSettings()
	if !bytes.Contains(first, []byte("echo hook-v1")) {
		t.Fatalf("native Gemini hook was not published: %s", first)
	}
	mcpV1ID, mcpV1Entry := ownedEntry(ownership.KindMcpServer, "mcpServers", "managed-server")
	hookV1ID, hookV1Entry := ownedEntry(ownership.KindHook, "BeforeTool", "")
	if mcpV1Entry.ValueDigest == "" || hookV1Entry.SourceDigest == "" {
		t.Fatalf("missing shared ownership evidence: MCP=%+v Hook=%+v", mcpV1Entry, hookV1Entry)
	}

	hookV2 := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo hook-v2", "")
	run(hookV2, mcpV1)
	afterHookUpdate := assertPreservedSettings()
	if bytes.Equal(first, afterHookUpdate) || !bytes.Contains(afterHookUpdate, []byte("echo hook-v2")) {
		t.Fatalf("hook-only update did not change native hooks: %s", afterHookUpdate)
	}
	_, mcpAfterHook := ownedEntry(ownership.KindMcpServer, "mcpServers", "managed-server")
	if mcpAfterHook.ValueDigest != mcpV1Entry.ValueDigest {
		t.Fatalf("hook-only update changed MCP proof: before=%+v after=%+v", mcpV1Entry, mcpAfterHook)
	}
	_, hookAfterHook := ownedEntry(ownership.KindHook, "BeforeTool", "")

	mcpV2 := geminiMCPProvider(t, "managed-server", "server-v2")
	run(hookV2, mcpV2)
	afterMCPUpdate := assertPreservedSettings()
	if !bytes.Contains(afterMCPUpdate, []byte("server-v2")) || bytes.Contains(afterMCPUpdate, []byte("server-v1")) {
		t.Fatalf("MCP-only update did not replace the selected server: %s", afterMCPUpdate)
	}
	_, hookAfterMCP := ownedEntry(ownership.KindHook, "BeforeTool", "")
	if hookAfterMCP.SourceDigest != hookAfterHook.SourceDigest {
		t.Fatalf("MCP-only update changed hook proof: before=%+v after=%+v", hookAfterHook, hookAfterMCP)
	}

	run(mcpV2)
	afterHookDeselection := assertPreservedSettings()
	if bytes.Contains(afterHookDeselection, []byte("echo hook-v2")) {
		t.Fatalf("deselected Gemini hook survived: %s", afterHookDeselection)
	}
	_, mcpAfterHookDeselection := ownedEntry(ownership.KindMcpServer, "mcpServers", "managed-server")
	if mcpAfterHookDeselection.ValueDigest == mcpV1Entry.ValueDigest {
		t.Fatalf("MCP update was lost during hook deselection: %+v", mcpAfterHookDeselection)
	}

	run(hookV2)
	afterMCPDeselection := assertPreservedSettings()
	if !bytes.Contains(afterMCPDeselection, []byte("echo hook-v2")) || bytes.Contains(afterMCPDeselection, []byte("server-v2")) {
		t.Fatalf("MCP deselection changed hooks or retained the server: %s", afterMCPDeselection)
	}
	store := nativeHookMetadataStore(t, nativeRoot)
	proof := nativeHookMetadataProof(t, store, "settings.json")
	if _, ok := proof.Entry(mcpV1ID); ok {
		t.Fatalf("deselected Gemini MCP proof survived: %+v", mcpV1ID)
	}
	if _, ok := proof.Entry(hookV1ID); ok {
		t.Fatalf("stale hook proof survived after value update: %+v", hookV1ID)
	}
	if proof.OwnedCount() != 1 {
		t.Fatalf("final Gemini ownership count = %d, want one hook entry", proof.OwnedCount())
	}
}

// spec: hook-resources/every-harness-target-receives-hooks-in-its-native-file
// spec: hook-resources/new-entries-carry-no-in-file-marker
func TestGeminiUnprovenMCPMarkerDoesNotBlockHookPublication(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	nativeRoot := filepath.Join(repo, ".gemini")
	nativeFile := filepath.Join(nativeRoot, "settings.json")
	seed := []byte(`{
  "unrelated" : { "keep" : true },
  "mcpServers" : { "blocked" : { "_managed_by" : "handwritten-marker", "command" : "manual-bin" } }
}
`)
	if err := os.MkdirAll(nativeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativeFile, seed, 0o640); err != nil {
		t.Fatal(err)
	}
	hook := additiveSafetyProvider(t, hcc.TargetGemini, false, "echo independent-hook", "")
	mcp := geminiMCPProvider(t, "blocked", "replacement-bin")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{hook, mcp}, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}))
	if err != nil || report == nil || !diag.HasCode(report.Diags, "materialize.collision.user-owned") {
		t.Fatalf("Run diagnostics = %v, error = %v; want an unproven MCP collision", report, err)
	}
	data, err := os.ReadFile(nativeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("echo independent-hook")) || !bytes.Contains(data, []byte("handwritten-marker")) || !bytes.Contains(data, []byte("manual-bin")) || bytes.Contains(data, []byte("replacement-bin")) {
		t.Fatalf("MCP refusal did not preserve its marker while publishing the independent hook: %s", data)
	}
	store := nativeHookMetadataStore(t, nativeRoot)
	proof := nativeHookMetadataProof(t, store, "settings.json")
	if proof.OwnedCount() != 1 {
		t.Fatalf("unproven MCP marker was adopted or hook publication was lost: %+v", proof.OwnedIDs())
	}
	for _, id := range proof.OwnedIDs() {
		if id.Kind != ownership.KindHook {
			t.Fatalf("unexpected owned Gemini entry: %+v", id)
		}
	}
}

func TestGeminiSharedHookAndMCPPlanRecoversTogetherAfterPublishFault(t *testing.T) {
	repo, _, _ := nativeHookJSONTestEnvironment(t)
	key := outputKey{target: hcc.TargetGemini}
	destination, file, err := nativeHookJSONLayout(key, repo)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string][]json.RawMessage{"BeforeTool": {json.RawMessage(`{"matcher":"run_shell_command","hooks":[{"type":"command","command":"echo shared-fault"}]}`)}}
	hookEntries, err := ownedJSONHookEntries(file.rel, groups, "selected")
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
	mcpEntry := ownedJSONEntry{
		rel: file.rel, container: "mcpServers", key: "docs", kind: ownership.KindMcpServer,
		identity: ownership.McpIdentityDigest("docs"), resource: "docs", raw: json.RawMessage(`{"command":"docs-server"}`),
	}
	var mcpDiagnostics []diag.Diagnostic
	finalize := nativeHookActivationFinalizer(key, repo, []ownedJSONEntry{mcpEntry}, false, func() {}, &mcpDiagnostics)
	injected := errors.New("interrupted shared Gemini publication")
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
		t.Fatalf("faulted reconciliation diagnostics = %v, want transaction write error", report.Diags)
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	var journalCount int
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dependencytxn-") && strings.HasSuffix(entry.Name(), ".json") {
			journalCount++
		}
	}
	if journalCount != 1 {
		t.Fatalf("pending dependency transaction journals = %d, want one", journalCount)
	}
	if err := dependencytxn.Recover(context.Background(), stateDir); err != nil {
		t.Fatalf("recover shared Gemini transaction: %v", err)
	}
	nativePath := filepath.Join(destination.Root, file.rel)
	nativeBytes, err := os.ReadFile(nativePath)
	if err != nil || !bytes.Contains(nativeBytes, []byte("shared-fault")) || !bytes.Contains(nativeBytes, []byte("docs-server")) {
		t.Fatalf("recovered shared native file = %s / %v", nativeBytes, err)
	}
	store, err := ownership.Open(destination.Root)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.Proof(file.rel)
	if err != nil {
		t.Fatal(err)
	}
	var hookOwned, mcpOwned bool
	for _, id := range proof.OwnedIDs() {
		hookOwned = hookOwned || id.Kind == ownership.KindHook
		mcpOwned = mcpOwned || id.Kind == ownership.KindMcpServer
	}
	if !hookOwned || !mcpOwned {
		t.Fatalf("recovered ownership proof omitted one shared file kind: %+v", proof.OwnedIDs())
	}
}

// spec: hook-native-location-tolerance/handwritten-native-satisfaction-prevents-legacy-recreation
func TestNativeHookActivationHandwrittenSatisfactionIsNotAdopted(t *testing.T) {
	repo, home, codexHome := nativeHookJSONTestEnvironment(t)
	resolved, sourceRoot := namedHookFixture(t, config.RepoCommitted, hcc.TargetClaude, "alpha")
	caseLayout := nativeHookJSONCaseByName(t, "claude_repo")
	_, root, spec, nativePath := nativeHookMetadataTarget(t, caseLayout, repo, home, codexHome)
	seed := sharedNativeSeed(t, hcc.TargetClaude, false)
	nativeBytes := []byte(seed.doc)
	if err := os.MkdirAll(filepath.Dir(nativePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nativePath, nativeBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if diag.HasErrors(report.Diags) || !diag.HasCode(report.Diags, diag.CodeMaterializeHookSatisfiedByUnmanaged) {
		t.Fatalf("handwritten native satisfaction diagnostics = %+v", report.Diags)
	}
	assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
	assertPathAbsent(t, legacyHookFileFor(hcc.TargetClaude, false, repo, home))
	store := nativeHookMetadataStore(t, root)
	proof := nativeHookMetadataProof(t, store, spec.rel)
	if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 {
		t.Fatalf("handwritten native hook was adopted: %+v", proof.OwnedIDs())
	}
	fingerprint := namedFingerprint(t, sourceRoot, resolved.Name, string(hcc.TargetClaude), HookScopeRepository)
	entry := nativeHookProofEntry(t, seed.event, seed.rule, resolved.Name, fingerprint)
	assertNativeHookSatisfactionRecord(t, proof, entry.ID(), entry.ValueDigest, resolved.Name, fingerprint)
	inspection, err := inspectNativeHookFile(store, root, hcc.TargetClaude, false)
	if err != nil {
		t.Fatalf("inspect handwritten native Hook: %v", err)
	}
	if !inspection.hookSatisfied(fingerprint, resolved.Name) {
		t.Fatal("native reader did not recognize the exact handwritten satisfaction")
	}
}

// spec: hook-native-location-tolerance/source-and-support-changes-invalidate-the-fingerprint
func TestNativeHookActivationSupportOnlyRefreshKeepsAggregateSnapshot(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			repo, _ := additiveSafetyEnv(t)
			resolved := additiveSafetyProvider(t, hcc.TargetFactory, false, "echo support", "#!/bin/sh\necho old support bytes\n")
			sourceSupport := filepath.Join(resolved.RegistryURL, "hooks", "shared.sh")
			if err := os.Chmod(sourceSupport, 0o755); err != nil {
				t.Fatal(err)
			}
			planBefore, supportPath := nativeHookActivationSupportPath(t, resolved, hcc.TargetFactory, false, filepath.Join(repo, ".factory"))
			opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved}, opts...)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("initial Run: %v / %v", err, report)
			}
			nativePath := filepath.Join(repo, ".factory", "hooks.json")
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativePath)
			installedBefore, err := os.ReadFile(supportPath)
			if err != nil {
				t.Fatal(err)
			}
			installedInfo, err := os.Lstat(supportPath)
			if err != nil {
				t.Fatal(err)
			}
			installedTargetInfo, err := os.Stat(supportPath)
			if err != nil {
				t.Fatal(err)
			}
			var supportTargetBefore string
			if mode == config.MaterializationModeSymlink {
				supportTargetBefore, err = os.Readlink(supportPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(sourceSupport, []byte("#!/bin/sh\necho new support bytes\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			planAfter, _ := nativeHookActivationSupportPath(t, resolved, hcc.TargetFactory, false, filepath.Join(repo, ".factory"))
			if planBefore.sourceDigest == planAfter.sourceDigest {
				t.Fatal("support-only source change did not refresh source proof input")
			}
			report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved}, opts...)
			if err != nil || diag.HasErrors(report.Diags) {
				t.Fatalf("support-only refresh: %v / %v", err, report)
			}
			assertFileUnchanged(t, nativePath, nativeBefore, nativeMode, nativeMtime)
			installedAfter, err := os.ReadFile(supportPath)
			if err != nil || !bytes.Equal(installedAfter, []byte("#!/bin/sh\necho new support bytes\n")) || bytes.Equal(installedBefore, installedAfter) {
				t.Fatalf("refreshed support = %q / %v", installedAfter, err)
			}
			installedInfoAfter, err := os.Lstat(supportPath)
			if err != nil || installedInfoAfter.Mode() != installedInfo.Mode() {
				t.Fatalf("support mode changed: before=%v after=%v err=%v", installedInfo.Mode(), installedInfoAfter, err)
			}
			installedTargetInfoAfter, err := os.Stat(supportPath)
			if err != nil || installedTargetInfoAfter.Mode().Perm() != installedTargetInfo.Mode().Perm() {
				t.Fatalf("support target permissions changed: before=%v after=%v err=%v", installedTargetInfo.Mode().Perm(), installedTargetInfoAfter, err)
			}
			store := nativeHookMetadataStore(t, filepath.Join(repo, ".factory"))
			proof := nativeHookMetadataProof(t, store, "hooks.json")
			if proof.OwnedCount() != 1 || len(proof.HookSatisfactions()) != 0 {
				t.Fatalf("owned hook proof after source refresh = %v / %+v", proof.OwnedIDs(), proof.HookSatisfactions())
			}
			id := proof.OwnedIDs()[0]
			entry, ok := proof.Entry(id)
			if !ok || entry.Resource != resolved.Name || entry.SourceDigest != planAfter.sourceDigest ||
				!proof.HasHookSource(id, resolved.Name, planAfter.sourceDigest) || proof.HasHookSource(id, resolved.Name, planBefore.sourceDigest) {
				t.Fatalf("refreshed hook proof = %+v, want the new captured digest", entry)
			}
			if mode == config.MaterializationModeSymlink {
				afterTarget, err := os.Readlink(supportPath)
				if err != nil || supportTargetBefore != afterTarget {
					t.Fatalf("support link changed: %q -> %q / %v", supportTargetBefore, afterTarget, err)
				}
			}
		})
	}
}

type nativeHookActivationSupportSnapshot struct {
	bytes      []byte
	mode       os.FileMode
	targetMode os.FileMode
	linkTarget string
	isLink     bool
}

func nativeHookActivationSupportSnapshotAt(t *testing.T, path string) nativeHookActivationSupportSnapshot {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := nativeHookActivationSupportSnapshot{bytes: data, mode: info.Mode(), targetMode: targetInfo.Mode()}
	if info.Mode()&os.ModeSymlink != 0 {
		snapshot.isLink = true
		snapshot.linkTarget, err = os.Readlink(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	return snapshot
}

func assertNativeHookActivationSupportSnapshot(t *testing.T, path string, before nativeHookActivationSupportSnapshot) {
	t.Helper()
	after := nativeHookActivationSupportSnapshotAt(t, path)
	if !bytes.Equal(before.bytes, after.bytes) || before.mode != after.mode || before.targetMode != after.targetMode ||
		before.isLink != after.isLink || (before.isLink && before.linkTarget != after.linkTarget) {
		t.Fatalf("installed support changed: before=%+v after=%+v", before, after)
	}
}

func optionalNativeHookFileSnapshot(t *testing.T, path string) ([]byte, os.FileMode, time.Time, bool) {
	t.Helper()
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, 0, time.Time{}, false
	} else if err != nil {
		t.Fatal(err)
	}
	data, mode, mtime := fileSnapshot(t, path)
	return data, mode, mtime, true
}

// spec: hook-native-location-tolerance/malformed-native-file-leaves-both-locations-unchanged
func TestNativeHookActivationMalformedNativePreservesInstalledSupport(t *testing.T) {
	for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
		t.Run(string(mode), func(t *testing.T) {
			repo, home := additiveSafetyEnv(t)
			resolved, staged := marketplaceHookFixture(t, hcc.TargetFactory, "sha256:abababababababababababababababababababababababababababababababab")
			resolved.InstalledName = "deploy-copy"
			_, supportPath := nativeHookActivationSupportPath(t, resolved, hcc.TargetFactory, false, filepath.Join(repo, ".factory"))
			opts := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved}, opts...)
			if err != nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("seed native hook and support: %v / %v", err, report)
			}
			supportBefore := nativeHookActivationSupportSnapshotAt(t, supportPath)
			if err := os.WriteFile(filepath.Join(staged, "run.sh"), []byte("#!/bin/sh\necho changed support\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			nativeFile := filepath.Join(repo, ".factory", "hooks.json")
			additiveSafetyWrite(t, nativeFile, `{"PreToolUse":"not-an-array"}`)
			if err := os.Chmod(nativeFile, 0o640); err != nil {
				t.Fatal(err)
			}
			nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, nativeFile)
			store := nativeHookMetadataStore(t, filepath.Join(repo, ".factory"))
			stateBefore, stateMode, stateMtime := fileSnapshot(t, store.StateFile())
			manifestPath, err := portableManifestPath(repo, false)
			if err != nil {
				t.Fatal(err)
			}
			manifestBefore, manifestMode, manifestMtime, manifestExists := optionalNativeHookFileSnapshot(t, manifestPath)
			legacyPath := legacyHookFileFor(hcc.TargetFactory, false, repo, home)
			assertPathAbsent(t, legacyPath)
			repoBefore := satisfactionPathTree(t, repo)
			stateTreeBefore := satisfactionPathTree(t, filepath.Join(home, "state"))
			cacheTreeBefore := satisfactionPathTree(t, filepath.Join(home, "cache"))
			report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved}, opts...)
			if err != nil {
				t.Fatalf("Run malformed native aggregate: %v", err)
			}
			if !diag.HasCode(report.Diags, diag.CodeHookNativeToleranceRefused) {
				t.Fatalf("missing %s native aggregate refusal: %+v", diag.CodeHookNativeToleranceRefused, report.Diags)
			}
			blocked := false
			for _, resource := range report.BlockedResources {
				if resource.Target == hcc.TargetFactory && resource.Name == "deploy-copy" {
					blocked = true
				}
			}
			if !blocked {
				t.Fatalf("malformed native hook was not blocked: %+v", report.BlockedResources)
			}
			assertFileUnchanged(t, nativeFile, nativeBefore, nativeMode, nativeMtime)
			assertFileUnchanged(t, store.StateFile(), stateBefore, stateMode, stateMtime)
			assertNativeHookActivationSupportSnapshot(t, supportPath, supportBefore)
			assertPathAbsent(t, legacyPath)
			if manifestExists {
				assertFileUnchanged(t, manifestPath, manifestBefore, manifestMode, manifestMtime)
			} else {
				assertPathAbsent(t, manifestPath)
			}
			if !reflect.DeepEqual(repoBefore, satisfactionPathTree(t, repo)) || !reflect.DeepEqual(stateTreeBefore, satisfactionPathTree(t, filepath.Join(home, "state"))) || !reflect.DeepEqual(cacheTreeBefore, satisfactionPathTree(t, filepath.Join(home, "cache"))) {
				t.Fatal("malformed native aggregate changed repository, state, cache, or staging paths")
			}
		})
	}
}
