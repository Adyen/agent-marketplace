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

func TestModelPolicyUnknownModelStillEmits(t *testing.T) {
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
			if len(result.Diagnostics) == 0 {
				t.Fatal("model omission was not diagnosed")
			}
		})
	}
}

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
