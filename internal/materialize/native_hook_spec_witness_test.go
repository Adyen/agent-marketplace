package materialize

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const nativeWitnessClaudeCommand = "echo native-claude-witness"

// spec: hook-resources/claude-code-hooks-merge-into-settings
// spec: hook-resources/handwritten-claude-hooks-satisfy-identical-entries
func TestNativeClaudeSettingsMergeAndSatisfaction(t *testing.T) {
	permissions := `"permissions" : { "allow" : [ "Bash" , "Read" ] }`
	seed := func(handwritten string) string {
		return "{\n  " + permissions + ",\n  \"hooks\" : { \"PreToolUse\" : [\n      " + handwritten + "\n  ] }\n}\n"
	}

	t.Run("merge", func(t *testing.T) {
		repo, _ := additiveSafetyEnv(t)
		settings := filepath.Join(repo, ".claude", "settings.json")
		handwritten := `{ "matcher" : "Bash" , "hooks" : [ { "type" : "command" , "command" : "echo handwritten-claude" } ] }`
		additiveSafetyWrite(t, settings, seed(handwritten))
		resolved := additiveSafetyProvider(t, hcc.TargetClaude, false, nativeWitnessClaudeCommand, "")
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
		if err != nil || hasErrorDiagnostics(report.Diags) {
			t.Fatalf("Run: %v / %v", err, report)
		}
		data, err := os.ReadFile(settings)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte(permissions)) || !bytes.Contains(data, []byte(handwritten)) {
			t.Fatalf("handwritten settings fragments changed: %s", data)
		}
		if bytes.Count(data, []byte(nativeWitnessClaudeCommand)) != 1 {
			t.Fatalf("selected command count in settings: %s", data)
		}
		if bytes.Contains(data, []byte("_managed_by")) {
			t.Fatalf("native Claude entries contain an in-file marker: %s", data)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("decode merged settings: %v", err)
		}
		groups, ok := document["hooks"].(map[string]any)["PreToolUse"].([]any)
		if !ok {
			t.Fatalf("missing hooks.PreToolUse groups: %s", data)
		}
		selected := 0
		for _, raw := range groups {
			group, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("invalid PreToolUse group: %#v", raw)
			}
			handlers, _ := group["hooks"].([]any)
			for _, rawHandler := range handlers {
				handler, ok := rawHandler.(map[string]any)
				if !ok {
					t.Fatalf("invalid PreToolUse handler: %#v", rawHandler)
				}
				if handler["command"] == nativeWitnessClaudeCommand {
					selected++
					if group["matcher"] != "Bash" || handler["type"] != "command" {
						t.Fatalf("selected handler shape = group %+v handler %+v", group, handler)
					}
				}
			}
		}
		if selected != 1 {
			t.Fatalf("selected PreToolUse/Bash grouped handler count = %d, want 1: %s", selected, data)
		}
		assertPathAbsent(t, filepath.Join(repo, ".claude", "hooks", "hooks.json"))
	})

	t.Run("satisfaction", func(t *testing.T) {
		repo, _ := additiveSafetyEnv(t)
		settings := filepath.Join(repo, ".claude", "settings.json")
		handwritten := `{ "matcher" : "Bash" , "hooks" : [ { "type" : "command" , "command" : "` + nativeWitnessClaudeCommand + `" } ] }`
		additiveSafetyWrite(t, settings, seed(handwritten))
		resolved := additiveSafetyProvider(t, hcc.TargetClaude, false, nativeWitnessClaudeCommand, "")
		plans, conflicts := prepareHookSupport([]skillTarget{{skill: resolved, target: hcc.TargetClaude}})
		if len(conflicts) != 0 || plans[0].err != nil || plans[0].sourceDigest == "" {
			t.Fatalf("prepareHookSupport = %+v / %v", plans, conflicts)
		}
		before, mode, mtime := fileSnapshot(t, settings)
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !diag.HasCode(report.Diags, diag.CodeMaterializeHookSatisfiedByUnmanaged) {
			t.Fatalf("missing satisfied-by-unmanaged diagnostic: %+v", report.Diags)
		}
		assertFileUnchanged(t, settings, before, mode, mtime)
		store := nativeHookMetadataStore(t, filepath.Join(repo, ".claude"))
		proof := nativeHookMetadataProof(t, store, "settings.json")
		if proof.OwnedCount() != 0 || len(proof.OwnedIDs()) != 0 {
			t.Fatalf("handwritten Claude hook became owned: %+v", proof.OwnedIDs())
		}
		satisfactions := proof.HookSatisfactions()
		wantSources := []ownership.HookSourceEvidence{{Resource: resolved.Name, SourceDigest: plans[0].sourceDigest}}
		if len(satisfactions) != 1 || !reflect.DeepEqual(satisfactions[0].Sources, wantSources) {
			t.Fatalf("source-bound satisfactions = %+v, want exactly one %+v", satisfactions, wantSources)
		}
		assertPathAbsent(t, filepath.Join(repo, ".claude", "hooks", "hooks.json"))
		report, err = Run(context.Background(), repo, nil)
		if err != nil || hasErrorDiagnostics(report.Diags) {
			t.Fatalf("deselect Run: %v / %v", err, report)
		}
		assertFileUnchanged(t, settings, before, mode, mtime)
	})
}

// spec: hook-resources/cursor-hooks-use-the-versioned-native-envelope
func TestNativeCursorPromptEnvelope(t *testing.T) {
	repo, _ := additiveSafetyEnv(t)
	hook := ir.NewHookWithOptions("submit-prompt", "UserPromptSubmit", "echo cursor-prompt-witness", "command", nil, ir.SourceRef{}, ir.WithHookSource("claude"))
	resolved := ingestedHookFixture(t, config.RepoCommitted, hcc.TargetCursor, "submit-prompt", hook, nil)
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
	if err != nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("Run: %v / %v", err, report)
	}
	native := filepath.Join(repo, ".cursor", "hooks.json")
	data, err := os.ReadFile(native)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Version int                         `json:"version"`
		Hooks   map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode cursor hooks.json: %v", err)
	}
	if document.Version != 1 {
		t.Fatalf("cursor envelope version = %v, want 1: %s", document.Version, data)
	}
	handlers := document.Hooks["beforeSubmitPrompt"]
	if len(handlers) != 1 || handlers[0]["command"] != "echo cursor-prompt-witness" || handlers[0]["type"] != "command" {
		t.Fatalf("beforeSubmitPrompt handlers = %+v, want one flat command handler", handlers)
	}
	if _, grouped := handlers[0]["hooks"]; grouped {
		t.Fatalf("flat Cursor handler carries a grouped hooks array: %+v", handlers[0])
	}
	if bytes.Contains(data, []byte("_managed_by")) {
		t.Fatalf("native Cursor entry contains an in-file marker: %s", data)
	}
	assertPathAbsent(t, filepath.Join(repo, ".cursor", "hooks", "hooks.json"))
}

// spec: hook-resources/windsurf-repository-hooks-use-the-devin-desktop-workspace-file
func TestNativeWindsurfDoesNotReadDevinCLIFile(t *testing.T) {
	const sentinel = "{invalid devin cli sentinel"
	const command = "echo windsurf-witness"
	nativeRel := filepath.Join(".devin", "hooks.json")

	assertWindsurfOutput := func(t *testing.T, repo string) {
		t.Helper()
		native := filepath.Join(repo, nativeRel)
		if got := additiveSafetyCommands(t, native, "hooks", "pre_run_command"); !reflect.DeepEqual(got, []string{command}) {
			t.Fatalf("windsurf native commands = %v", got)
		}
		data, err := os.ReadFile(native)
		if err != nil || bytes.Contains(data, []byte("_managed_by")) {
			t.Fatalf("native hook marker/output error: %v / %s", err, data)
		}
		assertPathAbsent(t, filepath.Join(repo, ".windsurf", "hooks.json"))
	}

	t.Run("public run reports the separate devin reconciliation only", func(t *testing.T) {
		repo, _ := additiveSafetyEnv(t)
		devinCLI := filepath.Join(repo, ".devin", "hooks.v1.json")
		additiveSafetyWrite(t, devinCLI, sentinel)
		devinBefore, devinMode, devinMtime := fileSnapshot(t, devinCLI)
		assertOnlyDevinSentinelError := func(report *Report) {
			t.Helper()
			devinMalformed := 0
			for _, diagnostic := range report.Diags {
				if diagnostic.Severity != diag.SeverityError {
					continue
				}
				if diagnostic.Target == string(hcc.TargetDevin) && diagnostic.Code == diag.CodeJSONMalformed {
					devinMalformed++
					continue
				}
				t.Fatalf("unexpected error diagnostic: %+v", diagnostic)
			}
			if devinMalformed != 1 {
				t.Fatalf("expected exactly one devin %s error for the invalid sentinel, got %d in %+v", diag.CodeJSONMalformed, devinMalformed, report.Diags)
			}
		}
		resolved := additiveSafetyProvider(t, hcc.TargetWindsurf, false, command, "")
		report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		assertOnlyDevinSentinelError(report)
		assertWindsurfOutput(t, repo)
		assertFileUnchanged(t, devinCLI, devinBefore, devinMode, devinMtime)
		nativeBefore, nativeMode, nativeMtime := fileSnapshot(t, filepath.Join(repo, nativeRel))
		report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{resolved})
		if err != nil {
			t.Fatalf("repeat Run: %v", err)
		}
		assertOnlyDevinSentinelError(report)
		assertFileUnchanged(t, filepath.Join(repo, nativeRel), nativeBefore, nativeMode, nativeMtime)
		assertFileUnchanged(t, devinCLI, devinBefore, devinMode, devinMtime)
		report, err = Run(context.Background(), repo, nil)
		if err != nil {
			t.Fatalf("deselect Run: %v", err)
		}
		assertOnlyDevinSentinelError(report)
		assertFileUnchanged(t, devinCLI, devinBefore, devinMode, devinMtime)
	})

	t.Run("windsurf-only execution never touches the devin cli file", func(t *testing.T) {
		repo, _ := additiveSafetyEnv(t)
		devinCLI := filepath.Join(repo, ".devin", "hooks.v1.json")
		additiveSafetyWrite(t, devinCLI, sentinel)
		devinBefore, devinMode, devinMtime := fileSnapshot(t, devinCLI)
		resolved := additiveSafetyProvider(t, hcc.TargetWindsurf, false, command, "")
		work, allOutputs, blocked, diags := buildWorkItems([]resolver.ResolvedArtifact{resolved}, repo, nil)
		for key := range allOutputs {
			if key != (outputKey{target: hcc.TargetWindsurf, machineScope: false}) {
				delete(allOutputs, key)
			}
		}
		compileResult := compileWorkItems(work, diags, allOutputs, blocked, repo)
		report := executeMaterializePlans(context.Background(), compileResult, false, repo, config.DefaultMaterializationModes())
		if hasErrorDiagnostics(report.Diags) {
			t.Fatalf("windsurf-scoped execution diagnostics: %+v", report.Diags)
		}
		assertWindsurfOutput(t, repo)
		assertFileUnchanged(t, devinCLI, devinBefore, devinMode, devinMtime)
	})
}
