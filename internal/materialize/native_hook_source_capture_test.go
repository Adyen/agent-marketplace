package materialize

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type work19NativeHookCase struct {
	name           string
	harness        hcc.Harness
	target         hcc.Target
	sourceFile     string
	definition     []byte
	wantEvent      string
	wantMatcher    string
	wantCommand    string
	wantPrompt     string
	wantType       string
	wantHookSource string
	wantTimeout    int
	hasTimeout     bool
	extraKey       string
	extraValue     any
}

func work19NativeHookCases() []work19NativeHookCase {
	return []work19NativeHookCase{
		{
			name: "claude_settings", harness: hcc.HarnessClaude, target: hcc.TargetClaude,
			sourceFile: ".claude/settings.json",
			definition: []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo capture-claude","timeout":9,"capture_extra":"claude"}]}]}}`),
			wantEvent:  "PreToolUse", wantMatcher: "Bash", wantCommand: "echo capture-claude", wantType: "command", wantHookSource: "claude", wantTimeout: 9, hasTimeout: true,
			extraKey: "capture_extra", extraValue: "claude",
		},
		{
			name: "factory_standalone", harness: hcc.HarnessFactory, target: hcc.TargetFactory,
			sourceFile: ".factory/hooks.json",
			definition: []byte(`{"PreToolUse":[{"matcher":"Execute","commandRegex":"^echo-capture$","hooks":[{"type":"command","command":"echo capture-factory"}]}]}`),
			wantEvent:  "PreToolUse", wantMatcher: "Execute", wantCommand: "echo capture-factory", wantType: "command", wantHookSource: "factory",
			extraKey: "commandRegex", extraValue: "^echo-capture$",
		},
		{
			name: "cursor_versioned", harness: hcc.HarnessCursor, target: hcc.TargetCursor,
			sourceFile: ".cursor/hooks.json",
			definition: []byte(`{"version":1,"hooks":{"beforeSubmitPrompt":[{"type":"command","command":"echo capture-cursor"}]}}`),
			wantEvent:  "beforeSubmitPrompt", wantCommand: "echo capture-cursor", wantType: "command", wantHookSource: "cursor",
		},
		{
			name: "windsurf_devin_workspace", harness: hcc.HarnessWindsurf, target: hcc.TargetWindsurf,
			sourceFile: ".devin/hooks.json",
			definition: []byte(`{"hooks":{"pre_run_command":[{"type":"command","command":"echo capture-windsurf","show_output":true,"working_dir":"."}]}}`),
			wantEvent:  "pre_run_command", wantCommand: "echo capture-windsurf", wantType: "command", wantHookSource: "windsurf",
		},
		{
			name: "gemini_milliseconds", harness: hcc.HarnessGemini, target: hcc.TargetGemini,
			sourceFile: ".gemini/settings.json",
			definition: []byte(`{"hooks":{"BeforeTool":[{"matcher":"run_shell_command","hooks":[{"type":"command","command":"echo capture-gemini","timeout":2500}]}]}}`),
			wantEvent:  "BeforeTool", wantMatcher: "run_shell_command", wantCommand: "echo capture-gemini", wantType: "command", wantHookSource: "gemini", wantTimeout: 2500, hasTimeout: true,
		},
		{
			name: "devin_prompt", harness: hcc.HarnessDevin, target: hcc.TargetDevin,
			sourceFile: ".devin/hooks.v1.json",
			definition: []byte(`{"PostCompaction":[{"type":"prompt","prompt":"capture Devin prompt"}]}`),
			wantEvent:  "PostCompaction", wantPrompt: "capture Devin prompt", wantType: "prompt", wantHookSource: "devin",
		},
		{
			name: "codex_session_start", harness: hcc.HarnessCodex, target: hcc.TargetCodex,
			sourceFile: ".codex/hooks.json",
			definition: []byte(`{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"echo capture-codex"}]}]}}`),
			wantEvent:  "SessionStart", wantMatcher: "startup", wantCommand: "echo capture-codex", wantType: "command", wantHookSource: "codex",
		},
		{
			name: "claude_canonical_provenance", harness: hcc.HarnessClaude, target: hcc.TargetClaude,
			sourceFile: "hooks/hooks.json",
			definition: []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo canonical-claude"}]}]}}`),
			wantEvent:  "PreToolUse", wantMatcher: "Bash", wantCommand: "echo canonical-claude", wantType: "command", wantHookSource: "",
		},
	}
}

func work19IngestNativeHook(t *testing.T, tc work19NativeHookCase, definition []byte) (*ir.Hook, string) {
	t.Helper()
	root := t.TempDir()
	definitionPath := filepath.Join(root, filepath.FromSlash(tc.sourceFile))
	if err := os.MkdirAll(filepath.Dir(definitionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	supportPath := filepath.Join(filepath.Dir(definitionPath), "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supportPath, []byte("source support bytes\n"), 0o751); err != nil {
		t.Fatal(err)
	}
	bundle, err := hcc.IngestBundle(os.DirFS(root), tc.harness)
	if err != nil {
		t.Fatalf("IngestBundle(%s): %v", tc.harness, err)
	}
	if diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("IngestBundle(%s) diagnostics: %#v", tc.harness, bundle.Diagnostics)
	}
	var hooks []*ir.Hook
	for _, resource := range bundle.Resources {
		if hook, ok := resource.(*ir.Hook); ok {
			hooks = append(hooks, hook)
		}
	}
	if len(hooks) != 1 {
		t.Fatalf("IngestBundle(%s) hooks = %d, want one: %#v", tc.harness, len(hooks), bundle.Resources)
	}
	hook := hooks[0]
	if hook.Source.File != tc.sourceFile || hook.HookSource != tc.wantHookSource || hook.Event != tc.wantEvent || hook.Matcher != tc.wantMatcher || hook.Command != tc.wantCommand || hook.Prompt != tc.wantPrompt || hook.Type != tc.wantType {
		t.Fatalf("IngestBundle(%s) hook = %#v", tc.harness, hook)
	}
	if tc.hasTimeout {
		if hook.Timeout == nil || *hook.Timeout != tc.wantTimeout {
			t.Fatalf("IngestBundle(%s) timeout = %v, want %d", tc.harness, hook.Timeout, tc.wantTimeout)
		}
	} else if hook.Timeout != nil {
		t.Fatalf("IngestBundle(%s) timeout = %d, want absent", tc.harness, *hook.Timeout)
	}
	if tc.extraKey != "" && hook.Extra[tc.extraKey] != tc.extraValue {
		t.Fatalf("IngestBundle(%s) extra[%q] = %v, want %v", tc.harness, tc.extraKey, hook.Extra[tc.extraKey], tc.extraValue)
	}
	if hook.SourceDefinitionDigest() == "" {
		t.Fatalf("IngestBundle(%s) returned no observed source digest", tc.harness)
	}
	return hook, root
}

type work19IndexOnlyFixture struct {
	resolved resolver.ResolvedArtifact
	hook     *ir.Hook
	payload  []byte
	staged   string
}

func work19BuildIndexOnlyFixture(t *testing.T, tc work19NativeHookCase, sourceDefinition, stagedDefinition []byte) work19IndexOnlyFixture {
	t.Helper()
	original, _ := work19IngestNativeHook(t, tc, sourceDefinition)
	canonical, err := hccartifact.FromIR(original)
	if err != nil {
		t.Fatal(err)
	}
	archive, _, err := hccartifact.Encode(canonical)
	if err != nil {
		t.Fatal(err)
	}
	decodedArchive, _, err := hccartifact.Decode(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	resource, err := hccartifact.DecodeResource(decodedArchive.Manifest.Kind, decodedArchive.Payload)
	if err != nil {
		t.Fatal(err)
	}
	hook, ok := resource.(*ir.Hook)
	if !ok {
		t.Fatalf("decoded canonical resource is %T, want hook", resource)
	}
	if hook.SourceDefinitionDigest() != "" {
		t.Fatal("canonical decode unexpectedly retained the private source-definition binding")
	}
	canonicalHook, err := hccartifact.FromIR(hook)
	if err != nil {
		t.Fatal(err)
	}
	staged := t.TempDir()
	definitionPath := filepath.Join(staged, "flattened-definition.json")
	if err := os.WriteFile(definitionPath, stagedDefinition, 0o640); err != nil {
		t.Fatal(err)
	}
	supportPath := filepath.Join(staged, "scripts", "run.sh")
	if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supportPath, []byte{0x00, 0x41, 0xff, 0x0a}, 0o751); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.ResolvedArtifact{
		Entry:  catalog.Entry{Kind: ir.KindHook, Name: hook.Name(), Path: definitionPath, Resource: hook},
		Source: config.RepoCommitted, Harnesses: []hcc.Target{tc.target},
		IsMarketplace: true, MarketplaceAcquisition: config.MarketplaceAcquisitionIndexOnly,
		MarketplaceInstanceID: "work19-instance", MarketplaceRegistryID: "work19-registry",
		MarketplaceResourceName: hook.Name(), MarketplaceDigest: "sha256:" + strings.Repeat("a", 64),
		InstalledName: "installed-" + tc.name, StagedSourceDir: staged,
	}
	return work19IndexOnlyFixture{resolved: resolved, hook: hook, payload: append([]byte(nil), canonicalHook.Payload...), staged: staged}
}

func TestPrepareHookSupportSnapshotBindsDefinitionAndExcludedPeer(t *testing.T) {
	root := t.TempDir()
	definition := []byte(`{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}`)
	definitionPath := filepath.Join(root, ".devin", "hooks.v1.json")
	supportPath := filepath.Join(root, ".devin", "scripts", "run.sh")
	peerPath := filepath.Join(root, ".devin", "config.json")
	if err := os.MkdirAll(filepath.Dir(definitionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(supportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definitionPath, definition, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supportPath, []byte{0x00, 0x01, 0xff}, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(peerPath, []byte(`{"hooks":{"PreToolUse":[]}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load(root, "devin")
	if err != nil || len(cat.Entries) != 1 {
		t.Fatalf("catalog: %v entries=%#v", err, cat.Entries)
	}
	resolved := resolver.ResolvedArtifact{Entry: cat.Entries[0], RegistryURL: root, CatalogKey: root, Source: config.RepoCommitted, V4Kind: config.V4SourceDirectory, Harnesses: []hcc.Target{hcc.TargetDevin}}
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetDevin}})
	plan := plans[0]
	if plan.err != nil || len(conflicts) != 0 {
		t.Fatalf("initial capture: plan=%+v conflicts=%v", plan, conflicts)
	}
	if plan.sourceDigest == "" {
		t.Fatal("stable ingested hook has no captured source proof")
	}
	if len(plan.artifacts) != 1 || !strings.HasSuffix(plan.artifacts[0].Path, "/scripts/run.sh") || string(plan.artifacts[0].Bytes) != string([]byte{0x00, 0x01, 0xff}) || plan.artifacts[0].Mode.Perm() != 0o750 {
		t.Fatalf("initial support projection = %#v", plan.artifacts)
	}
	originalDigest := plan.sourceDigest
	originalArtifacts := []hccartifact.Artifact{{Path: plan.artifacts[0].Path, Bytes: append([]byte(nil), plan.artifacts[0].Bytes...), Mode: plan.artifacts[0].Mode}}
	if err := os.WriteFile(supportPath, []byte{0x11, 0x22, 0x33}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(supportPath, 0o600); err != nil {
		t.Fatal(err)
	}
	supportChanged, supportConflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetDevin}})
	supportPlan := supportChanged[0]
	if supportPlan.err != nil || len(supportConflicts) != 0 {
		t.Fatalf("support-only capture: plan=%+v conflicts=%v", supportPlan, supportConflicts)
	}
	if supportPlan.sourceDigest == originalDigest {
		t.Fatal("changing support bytes and mode did not change the captured proof")
	}
	if len(supportPlan.artifacts) != 1 || !strings.HasSuffix(supportPlan.artifacts[0].Path, "/scripts/run.sh") || string(supportPlan.artifacts[0].Bytes) != string([]byte{0x11, 0x22, 0x33}) || supportPlan.artifacts[0].Mode.Perm() != 0o600 {
		t.Fatalf("support-only projection = %#v", supportPlan.artifacts)
	}
	if err := os.WriteFile(peerPath, []byte(`{"hooks":{"PostToolUse":[]}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	peerChanged, peerConflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetDevin}})
	peerPlan := peerChanged[0]
	if peerPlan.err != nil || len(peerConflicts) != 0 {
		t.Fatalf("excluded-peer capture: plan=%+v conflicts=%v", peerPlan, peerConflicts)
	}
	if peerPlan.sourceDigest == supportPlan.sourceDigest {
		t.Fatal("changing the excluded native peer did not change the captured proof")
	}
	if len(peerPlan.artifacts) != len(supportPlan.artifacts) || peerPlan.artifacts[0].Path != supportPlan.artifacts[0].Path || string(peerPlan.artifacts[0].Bytes) != string(supportPlan.artifacts[0].Bytes) || peerPlan.artifacts[0].Mode.Perm() != supportPlan.artifacts[0].Mode.Perm() {
		t.Fatalf("excluded-peer projection changed installed support: %#v", peerPlan.artifacts)
	}
	if plan.sourceDigest != originalDigest || len(plan.artifacts) != len(originalArtifacts) || string(plan.artifacts[0].Bytes) != string(originalArtifacts[0].Bytes) || plan.artifacts[0].Mode.Perm() != originalArtifacts[0].Mode.Perm() {
		t.Fatal("captured plan changed after the source tree was modified")
	}
}

func TestHookSourceFingerprintCapturedSnapshotMatchesFrozenVectors(t *testing.T) {
	vectors := loadHookSourceFingerprintVectors(t)
	for _, vector := range vectors.Cases {
		t.Run(vector.Name, func(t *testing.T) {
			root, _ := writeFingerprintSource(t, vector)
			snapshot, err := captureHookSourceSnapshot(root, vector.Definition.Path, path.Dir(vector.Definition.Path))
			if err != nil {
				t.Fatalf("captureHookSourceSnapshot: %v", err)
			}
			got, err := hookSourceFingerprintFromSnapshot(fingerprintVectorInput(vector, root), snapshot)
			if err != nil {
				t.Fatalf("hookSourceFingerprintFromSnapshot: %v", err)
			}
			if got != vector.Digest {
				t.Fatalf("captured fingerprint = %s, want frozen vector %s", got, vector.Digest)
			}
		})
	}
}

func TestPrepareHookSupportIndexOnlyReingestsActualNativeLayouts(t *testing.T) {
	for _, tc := range work19NativeHookCases() {
		t.Run(tc.name, func(t *testing.T) {
			fixture := work19BuildIndexOnlyFixture(t, tc, tc.definition, tc.definition)
			before := nativeSupportSnapshot(t, fixture.staged)
			beforePayload := append([]byte(nil), fixture.payload...)
			plans, conflicts := prepareHookSupport([]skillTarget{{skill: fixture.resolved, target: tc.target}})
			plan := plans[0]
			if plan.err != nil || len(conflicts) != 0 {
				t.Fatalf("prepareHookSupport: plan=%+v conflicts=%v", plan, conflicts)
			}
			if plan.sourceDigest == "" {
				t.Fatal("semantically equal index-only hook did not acquire captured source proof")
			}
			if !strings.HasSuffix(plan.relBase, "/installed-"+tc.name) {
				t.Fatalf("support base = %q, want installed alias", plan.relBase)
			}
			if len(plan.artifacts) != 1 || !strings.HasSuffix(plan.artifacts[0].Path, "/scripts/run.sh") || string(plan.artifacts[0].Bytes) != string([]byte{0x00, 0x41, 0xff, 0x0a}) || plan.artifacts[0].Mode.Perm() != 0o751 {
				t.Fatalf("support projection = %#v", plan.artifacts)
			}
			afterCanonical, err := hccartifact.FromIR(fixture.hook)
			if err != nil || !bytes.Equal(afterCanonical.Payload, beforePayload) {
				t.Fatalf("prepareHookSupport mutated canonical resource: %v", err)
			}
			assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, fixture.staged))
		})
	}
}

func TestPrepareHookSupportIndexOnlyRejectsDefinitionAndTypedPayloadDrift(t *testing.T) {
	factory := work19NativeHookCases()[1]
	changedDefinition := []byte(`{"PreToolUse":[{"matcher":"Execute","commandRegex":"^echo-capture$","hooks":[{"type":"command","command":"echo changed-factory"}]}]}`)
	fixture := work19BuildIndexOnlyFixture(t, factory, factory.definition, changedDefinition)
	before := nativeSupportSnapshot(t, fixture.staged)
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: fixture.resolved, target: factory.target}})
	if plans[0].err == nil || plans[0].sourceDigest != "" || len(plans[0].artifacts) != 0 || len(conflicts) != 0 {
		t.Fatalf("changed captured definition produced proof/output: plan=%+v conflicts=%v", plans[0], conflicts)
	}
	assertNativeSupportSnapshot(t, before, nativeSupportSnapshot(t, fixture.staged))

	cases := []struct {
		name   string
		mutate func(*ir.Hook)
	}{
		{name: "matcher", mutate: func(hook *ir.Hook) { hook.Matcher = "Read" }},
		{name: "prompt", mutate: func(hook *ir.Hook) { hook.Prompt = "unbound prompt" }},
		{name: "type", mutate: func(hook *ir.Hook) { hook.Type = "prompt" }},
		{name: "timeout", mutate: func(hook *ir.Hook) { value := 17; hook.Timeout = &value }},
		{name: "extra", mutate: func(hook *ir.Hook) { hook.Extra = map[string]any{"unbound": "field"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := work19BuildIndexOnlyFixture(t, factory, factory.definition, factory.definition)
			tc.mutate(fixture.hook)
			beforePayload, err := hccartifact.FromIR(fixture.hook)
			if err != nil {
				t.Fatal(err)
			}
			beforeTree := nativeSupportSnapshot(t, fixture.staged)
			plans, conflicts := prepareHookSupport([]skillTarget{{skill: fixture.resolved, target: factory.target}})
			if plans[0].err == nil || plans[0].sourceDigest != "" || len(plans[0].artifacts) != 0 || len(conflicts) != 0 {
				t.Fatalf("mismatched canonical payload produced proof/output: plan=%+v conflicts=%v", plans[0], conflicts)
			}
			afterPayload, err := hccartifact.FromIR(fixture.hook)
			if err != nil || !bytes.Equal(beforePayload.Payload, afterPayload.Payload) {
				t.Fatalf("prepareHookSupport mutated canonical resource: %v", err)
			}
			assertNativeSupportSnapshot(t, beforeTree, nativeSupportSnapshot(t, fixture.staged))
		})
	}
}

func TestPrepareHookSupportIndexOnlyRejectsUnsafeBinding(t *testing.T) {
	claude := work19NativeHookCases()[0]
	cases := []struct {
		name   string
		mutate func(*ir.Hook, *resolver.ResolvedArtifact)
	}{
		{name: "missing source file", mutate: func(hook *ir.Hook, _ *resolver.ResolvedArtifact) { hook.Source.File = "" }},
		{name: "unsafe source file", mutate: func(hook *ir.Hook, _ *resolver.ResolvedArtifact) { hook.Source.File = "../escape.json" }},
		{name: "source harness contradiction", mutate: func(_ *ir.Hook, res *resolver.ResolvedArtifact) { res.SourceHarness = "factory" }},
		{name: "malformed captured definition", mutate: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definition := claude.definition
			if tc.name == "malformed captured definition" {
				definition = []byte(`{"hooks":`)
			}
			fixture := work19BuildIndexOnlyFixture(t, claude, claude.definition, definition)
			if tc.mutate != nil {
				tc.mutate(fixture.hook, &fixture.resolved)
			}
			plans, conflicts := prepareHookSupport([]skillTarget{{skill: fixture.resolved, target: claude.target}})
			if plans[0].err == nil || plans[0].sourceDigest != "" || len(plans[0].artifacts) != 0 || len(conflicts) != 0 {
				t.Fatalf("unsafe index-only binding produced proof/output: plan=%+v conflicts=%v", plans[0], conflicts)
			}
		})
	}
}

func TestSelectIndexOnlyHookRequiresOneCleanNameMatch(t *testing.T) {
	hook := ir.NewHook("one", "PreToolUse", "echo one", "command", nil, ir.SourceRef{File: "hooks/hooks.json"})
	if got, err := selectIndexOnlyHook(&ingest.Bundle{Resources: []ir.Resource{hook}}, "one"); err != nil || got != hook {
		t.Fatalf("unique hook selection = %p err=%v, want %p", got, err, hook)
	}
	if _, err := selectIndexOnlyHook(&ingest.Bundle{Resources: []ir.Resource{hook}}, "missing"); err == nil {
		t.Fatal("missing index-only hook was accepted")
	}
	if _, err := selectIndexOnlyHook(&ingest.Bundle{Resources: []ir.Resource{hook, hook}}, "one"); err == nil {
		t.Fatal("ambiguous index-only hook was accepted")
	}
	if _, err := selectIndexOnlyHook(&ingest.Bundle{Resources: []ir.Resource{hook}, Diagnostics: []diag.Diagnostic{{Severity: diag.SeverityError}}}, "one"); err == nil {
		t.Fatal("error-diagnosed index-only hook ingestion was accepted")
	}
}

func TestPrepareHookSupportUsesPinnedGitDefinition(t *testing.T) {
	tc := work19NativeHookCases()[1]
	hook, snapshotRoot := work19IngestNativeHook(t, tc, tc.definition)
	hook.Source.File = filepath.Join(snapshotRoot, filepath.FromSlash(tc.sourceFile))
	readableRoot := t.TempDir()
	readableDefinition := filepath.Join(readableRoot, filepath.FromSlash(tc.sourceFile))
	changedReadable := []byte(strings.Replace(string(tc.definition), "echo capture-factory", "echo readable-only", 1))
	if err := os.MkdirAll(filepath.Dir(readableDefinition), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readableDefinition, changedReadable, 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Kind: ir.KindHook, Name: hook.Name(), Path: readableDefinition, Resource: hook, SourceHarness: string(tc.harness)},
		RegistryURL: "https://example.invalid/registry.git", RegistryPath: "plugins/deploy", Ref: "main",
		V4Kind: config.V4SourceGit, Source: config.RepoCommitted, Harnesses: []hcc.Target{tc.target},
	}
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: tc.target}})
	plan := plans[0]
	if plan.err != nil || len(conflicts) != 0 || plan.sourceDigest == "" {
		t.Fatalf("snapshot-pinned capture: plan=%+v conflicts=%v", plan, conflicts)
	}
	if len(plan.artifacts) != 1 || !strings.HasSuffix(plan.artifacts[0].Path, "/scripts/run.sh") || string(plan.artifacts[0].Bytes) != "source support bytes\n" {
		t.Fatalf("snapshot-pinned support = %#v", plan.artifacts)
	}
}

func TestPrepareHookSupportUnboundRawHookGetsNoSourceProof(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".factory", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".factory", "hooks.json"), []byte(`{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo manual"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".factory", "scripts", "run.sh"), []byte("manual support\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := ir.NewHookWithMatcher("manual", "PreToolUse", "Execute", "echo manual", "command", nil, ir.SourceRef{File: ".factory/hooks.json"})
	resolved := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Kind: ir.KindHook, Name: hook.Name(), Path: filepath.Join(root, ".factory", "hooks.json"), Resource: hook, SourceHarness: "factory"},
		RegistryURL: root, CatalogKey: root, V4Kind: config.V4SourceDirectory, Source: config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetFactory}})
	plan := plans[0]
	if plan.err != nil || len(conflicts) != 0 {
		t.Fatalf("legacy raw support preparation: plan=%+v conflicts=%v", plan, conflicts)
	}
	if plan.sourceDigest != "" {
		t.Fatalf("unbound raw hook received source proof %q", plan.sourceDigest)
	}
	if len(plan.artifacts) != 1 || !strings.HasSuffix(plan.artifacts[0].Path, "/scripts/run.sh") || string(plan.artifacts[0].Bytes) != "manual support\n" {
		t.Fatalf("unbound raw support artifacts = %#v", plan.artifacts)
	}
}

func TestPrepareHookSupportRejectsMissingRawDefinition(t *testing.T) {
	root := t.TempDir()
	hook := ir.NewHookWithMatcher("missing", "PreToolUse", "Execute", "echo missing", "command", nil, ir.SourceRef{File: ".factory/hooks.json"})
	resolved := resolver.ResolvedArtifact{
		Entry:       catalog.Entry{Kind: ir.KindHook, Name: hook.Name(), Path: filepath.Join(root, ".factory", "hooks.json"), Resource: hook, SourceHarness: "factory"},
		RegistryURL: root, CatalogKey: root, V4Kind: config.V4SourceDirectory, Source: config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}
	plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetFactory}})
	if plans[0].err == nil || plans[0].sourceDigest != "" || len(plans[0].artifacts) != 0 || len(conflicts) != 0 {
		t.Fatalf("missing definition was not refused before support preparation: plan=%+v conflicts=%v", plans[0], conflicts)
	}
}

func TestPrepareHookSupportRejectsUnsafeExcludedPeerSnapshot(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string, string) string
	}{
		{
			name: "symlink peer",
			mutate: func(t *testing.T, peer, outside string) string {
				if err := os.WriteFile(outside, []byte(`{"hooks":{"sentinel":"peer"}}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(peer); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, peer); err != nil {
					t.Fatal(err)
				}
				return outside
			},
		},
		{
			name: "oversized peer",
			mutate: func(t *testing.T, peer, _ string) string {
				file, err := os.Create(peer)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(hookSourceFingerprintMaxBytes + 1); err != nil {
					_ = file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				return ""
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			definitionPath := filepath.Join(root, ".devin", "hooks.v1.json")
			peerPath := filepath.Join(root, ".devin", "config.json")
			supportPath := filepath.Join(root, ".devin", "run.sh")
			if err := os.MkdirAll(filepath.Dir(definitionPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(definitionPath, []byte(`{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"./run.sh"}]}]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(peerPath, []byte(`{"hooks":{"PreToolUse":[]}}`), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(supportPath, []byte("safe support\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cat, err := catalog.Load(root, "devin")
			if err != nil || len(cat.Entries) != 1 {
				t.Fatalf("catalog: %v entries=%#v", err, cat.Entries)
			}
			resolved := resolver.ResolvedArtifact{Entry: cat.Entries[0], RegistryURL: root, CatalogKey: root, Source: config.RepoCommitted, V4Kind: config.V4SourceDirectory, Harnesses: []hcc.Target{hcc.TargetDevin}}
			outside := filepath.Join(t.TempDir(), "peer.json")
			beforeExternal := []byte(`{"hooks":{"sentinel":"peer"}}`)
			if err := os.WriteFile(outside, beforeExternal, 0o600); err != nil {
				t.Fatal(err)
			}
			external := tc.mutate(t, peerPath, outside)
			plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetDevin}})
			if plans[0].err == nil || plans[0].sourceDigest != "" || len(plans[0].artifacts) != 0 || len(conflicts) != 0 {
				t.Fatalf("unsafe excluded peer was not refused: plan=%+v conflicts=%v", plans[0], conflicts)
			}
			if external != "" {
				data, err := os.ReadFile(external)
				if err != nil || !bytes.Equal(data, beforeExternal) {
					t.Fatalf("external peer changed: %q err=%v", data, err)
				}
			}
		})
	}
}

func TestHookDefinitionSnapshotFSConformsToFS(t *testing.T) {
	data := []byte(`{"hooks":{"PreToolUse":[]}}`)
	for _, tc := range []struct {
		name       string
		definition string
	}{
		{name: "nested", definition: "nested/hooks.json"},
		{name: "root", definition: "hooks.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := hookDefinitionSnapshotFS{definition: tc.definition, data: data}
			if err := fstest.TestFS(source, tc.definition); err != nil {
				t.Fatalf("fstest.TestFS: %v", err)
			}
			directory := path.Dir(tc.definition)
			entries, err := fs.ReadDir(source, directory)
			if err != nil || len(entries) != 1 {
				t.Fatalf("ReadDir(%q) entries=%v err=%v", directory, entries, err)
			}
			entryInfo, err := entries[0].Info()
			if err != nil {
				t.Fatalf("DirEntry.Info: %v", err)
			}
			statInfo, err := fs.Stat(source, tc.definition)
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			file, err := source.Open(tc.definition)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			openInfo, statErr := file.Stat()
			closeErr := file.Close()
			if statErr != nil {
				t.Fatalf("Open.Stat: %v", statErr)
			}
			if closeErr != nil {
				t.Fatalf("Close: %v", closeErr)
			}
			wantSize := int64(len(data))
			if entryInfo.Size() != wantSize || entryInfo.Size() != statInfo.Size() || entryInfo.Size() != openInfo.Size() {
				t.Fatalf("file info sizes: ReadDir=%d Stat=%d Open.Stat=%d, want %d", entryInfo.Size(), statInfo.Size(), openInfo.Size(), wantSize)
			}
		})
	}
}
