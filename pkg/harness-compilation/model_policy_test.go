package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func modelPolicyFields(t *testing.T, artifacts []artifact.Artifact) map[string]any {
	t.Helper()
	for _, a := range artifacts {
		if !strings.HasSuffix(a.Path, "/review.md") && !strings.HasSuffix(a.Path, "/review.toml") {
			continue
		}
		fields := map[string]any{}
		if strings.HasSuffix(a.Path, ".toml") {
			if err := toml.Unmarshal(a.Bytes, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["developer_instructions"] != "Keep this body.\n" {
				t.Fatalf("body lost: %s", a.Bytes)
			}
		} else {
			parts := strings.SplitN(string(a.Bytes), "---\n", 3)
			if len(parts) != 3 || parts[2] != "Keep this body.\n" {
				t.Fatalf("body lost: %s", a.Bytes)
			}
			if err := yaml.Unmarshal([]byte(parts[1]), &fields); err != nil {
				t.Fatal(err)
			}
		}
		return fields
	}
	t.Fatal("subagent artifact missing")
	return nil
}

// spec: harness-model-catalogs/unresolved-model-warns-and-keeps-the-resource
func TestModelPolicyUnknownModelStillEmits(t *testing.T) {
	wantCode := map[Target]string{
		TargetClaude:  diag.CodeModelUnresolved,
		TargetFactory: diag.CodeModelUnresolved,
		TargetCursor:  diag.CodeModelUnresolved,
		TargetGemini:  diag.CodeModelUnresolved,
		TargetDevin:   diag.CodeSubagentDevinModelUnmappable,
		TargetCodex:   diag.CodeModelUnresolved,
	}
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetGemini, TargetDevin, TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			r := ir.NewSubagent("review", "Review", "Keep this body.\n", "unrecognized-provider-model", nil, "", nil, "", nil, ir.SourceRef{})
			result, err := Compile(r, target)
			if err != nil || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("model loss suppressed resource: %v %+v", err, result)
			}
			if fields := modelPolicyFields(t, result.Artifacts); fields["model"] != nil {
				t.Fatalf("unknown model emitted: %v", fields)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != wantCode[target] || result.Diagnostics[0].Severity != diag.SeverityWarning {
				t.Fatalf("model omission diagnostics = %+v, want one %s warning", result.Diagnostics, wantCode[target])
			}
		})
	}
}

// spec: harness-model-catalogs/no-cross-provider-approximation
// spec: harness-model-catalogs/floating-family-selector-pinned-with-notice
// spec: harness-model-catalogs/unsupported-reasoning-warns-without-suppressing
// spec: harness-model-catalogs/conflicting-reasoning-preference-warns-and-resolves-once
// spec: harness-model-catalogs/crossing-inheritance-and-default-boundaries-warns
// spec: harness-model-catalogs/reasoning-on-inherited-selection-is-omitted-when-unrepresentable
func TestModelPolicyDiagnosticScenarios(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		source                                          Target
		target                                          Target
		model, effort, wantModel, effortKey, wantEffort string
		wantCode                                        string
		wantSeverity                                    diag.Severity
	}{
		{"no cross-provider approximation", "", TargetGemini, "sonnet", "", "", "effort", "", diag.CodeModelUnresolved, diag.SeverityWarning},
		{"unsupported reasoning", "", TargetGemini, "gemini-3-preview", "high", "gemini-3-preview", "effort", "", diag.CodeModelReasoningOmitted, diag.SeverityWarning},
		{"conflicting reasoning", TargetCursor, TargetClaude, "claude-opus-5[effort=high]", "low", "claude-opus-5", "effort", "low", diag.CodeModelReasoningConflict, diag.SeverityWarning},
		{"floating family pin", "", TargetFactory, "sonnet", "", "claude-sonnet-5", "reasoningEffort", "", diag.CodeModelSelectorPinned, diag.SeverityInfo},
		{"inheritance/default mismatch", TargetDevin, TargetClaude, "", "", "", "effort", "", diag.CodeModelInheritanceMismatch, diag.SeverityWarning},
		{"reasoning on inherited selection", "", TargetFactory, "inherit", "high", "inherit", "reasoningEffort", "", diag.CodeModelReasoningOmitted, diag.SeverityWarning},
		{"positive control", TargetClaude, TargetCodex, "gpt-5.6-luna", "medium", "gpt-5.6-luna", "model_reasoning_effort", "medium", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ir.NewSubagent("review", "Review", "Keep this body.\n", tc.model, nil, tc.effort, nil, "", nil, ir.SourceRef{})
			r.ModelSource = string(tc.source)
			result, err := Compile(r, tc.target)
			if err != nil || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("compile: %v %+v", err, result)
			}
			fields := modelPolicyFields(t, result.Artifacts)
			model, _ := fields["model"].(string)
			effort, _ := fields[tc.effortKey].(string)
			if model != tc.wantModel || effort != tc.wantEffort {
				t.Fatalf("fields = %v, want model=%q %s=%q", fields, tc.wantModel, tc.effortKey, tc.wantEffort)
			}
			if tc.wantCode == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("positive control emitted diagnostics: %+v", result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != tc.wantCode || result.Diagnostics[0].Severity != tc.wantSeverity {
				t.Fatalf("diagnostics = %+v, want one %s %s", result.Diagnostics, tc.wantSeverity, tc.wantCode)
			}
		})
	}
}

// spec: harness-model-catalogs/supported-reasoning-emits-native-encoding
func TestModelPolicyNativeReasoningEncoding(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		target                                          Target
		model, effort, wantModel, effortKey, wantEffort string
	}{
		{"cursor parameter", TargetCursor, "claude-opus-5", "high", "claude-opus-5[effort=high]", "effort", ""},
		{"gemini has no effort field", TargetGemini, "gemini-3-preview", "high", "gemini-3-preview", "effort", ""},
		{"codex inheritance is omission", TargetCodex, "inherit", "", "", "model_reasoning_effort", ""},
		{"factory inheritance ignores effort", TargetFactory, "inherit", "high", "inherit", "reasoningEffort", ""},
		{"codex concrete model and effort", TargetCodex, "gpt-5.6-terra", "high", "gpt-5.6-terra", "model_reasoning_effort", "high"},
		{"codex Terra 6.1 reasoning", TargetCodex, "openai/gpt-6.1-terra", "high", "gpt-6.1-terra", "model_reasoning_effort", "high"},
		{"factory DeepSeek 1017", TargetFactory, "deepseek/deepseek-v4.1-flash-1017", "low", "deepseek-v4.1-flash-1017", "reasoningEffort", "low"},
		{"factory MiniMax Flash", TargetFactory, "minimax/minimax-m3.1-flash", "medium", "minimax-m3.1-flash", "reasoningEffort", "medium"},
		{"factory Nemotron Super", TargetFactory, "nvidia/nemotron-3.5-super", "high", "nemotron-3.5-super", "reasoningEffort", "high"},
		{"factory Luna fast", TargetFactory, "openai/gpt-6-luna-fast", "medium", "gpt-6-luna-fast", "reasoningEffort", "medium"},
		{"factory Sol fast", TargetFactory, "openai/gpt-6-sol-fast", "low", "gpt-6-sol-fast", "reasoningEffort", "low"},
		{"factory Sol ultrafast", TargetFactory, "openai/gpt-6.1-sol-ultrafast", "high", "gpt-6.1-sol-ultrafast", "reasoningEffort", "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ir.NewSubagent("review", "Review", "Keep this body.\n", tc.model, nil, tc.effort, nil, "", nil, ir.SourceRef{})
			result, err := Compile(r, tc.target)
			if err != nil || diag.HasErrors(result.Diagnostics) {
				t.Fatalf("compile: %v %+v", err, result)
			}
			fields := modelPolicyFields(t, result.Artifacts)
			model, _ := fields["model"].(string)
			effort, _ := fields[tc.effortKey].(string)
			if model != tc.wantModel || effort != tc.wantEffort {
				t.Fatalf("fields = %v, want model=%q %s=%q", fields, tc.wantModel, tc.effortKey, tc.wantEffort)
			}
		})
	}
}

// spec: harness-model-catalogs/same-harness-native-model-strings-round-trip-unchanged
func TestModelPolicyDevinNativeRoundTrip(t *testing.T) {
	for _, model := range []string{"swe", "gpt", "gpt-5-6-luna-medium", ""} {
		t.Run(model, func(t *testing.T) {
			field := ""
			if model != "" {
				field = "model: " + model + "\n"
			}
			src := fstest.MapFS{".devin/agents/review.md": {Data: []byte("---\nname: review\ndescription: Review\n" + field + "---\nKeep this body.\n")}}
			result, err := Convert(src, HarnessDevin, TargetDevin)
			if err != nil {
				t.Fatal(err)
			}
			fields := modelPolicyFields(t, result.Artifacts)
			got, _ := fields["model"].(string)
			if got != model {
				t.Fatalf("native selection changed: %v diagnostics=%+v", fields, result.Diagnostics)
			}
			for _, d := range result.Diagnostics {
				if d.Severity != diag.SeverityInfo {
					t.Fatalf("native selection produced a loss: %+v", d)
				}
			}
		})
	}
}
