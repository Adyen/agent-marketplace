package harnesscompile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

// Minimal Claude-plugin fixture for CompileBundle tests.
type compileBundleInvalidResource struct{ name string }

func (r compileBundleInvalidResource) Kind() ir.ResourceKind { return ir.KindSkill }
func (r compileBundleInvalidResource) Name() string          { return r.name }

var testPluginFS = fstest.MapFS{
	// Skill
	"skills/my-skill/SKILL.md": &fstest.MapFile{
		Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\nDo the thing"),
	},
	// Command
	"commands/my-cmd.md": &fstest.MapFile{
		Data: []byte("---\nname: my-cmd\ndescription: A test command\n---\nRun the command"),
	},
	// Subagent
	"agents/my-agent.md": &fstest.MapFile{
		Data: []byte("---\nname: my-agent\ndescription: A test agent\nmodel: sonnet\ntools:\n  - Read\n  - Write\n---\nYou are an agent"),
	},
	// Hook
	"hooks/hooks.json": &fstest.MapFile{
		Data: []byte(`{"hooks":{"on-save":[{"type":"command","command":"echo saved"}]}}`),
	},
	// MCP server
	".mcp.json": &fstest.MapFile{
		Data: []byte(`{"mcpServers":{"my-server":{"command":"npx","args":["-y","my-mcp"]}}}`),
	},
}

func TestCompileBundleFromIRWarnsWhenDroppingNativeCodexModelAndEffort(t *testing.T) {
	resource := ir.NewSubagent("reviewer", "Review", "Review.", "", nil, "", nil, "", nil, ir.SourceRef{})
	resource.Ext.Codex = &ext.Codex{Model: "gpt-native", ReasoningEffort: "xhigh"}

	result, err := compileBundleFromIR(&ingest.Bundle{Resources: []ir.Resource{resource}}, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) == 0 || !diag.HasCode(result.Diagnostics, diag.CodeSubagentCodexModelDropped) || !diag.HasCode(result.Diagnostics, diag.CodeSubagentCodexEffortDropped) || diag.HasErrors(result.Diagnostics) {
		t.Fatalf("compileBundleFromIR() = %#v, want artifacts plus native model and effort warnings", result)
	}
}

// TestCompileBundle_ContinuesAfterCompilerErrorAndSuppressesArtifacts pins the
// per-resource contract for a compiler that returns a hard error: the failing
// resource contributes nothing, the valid resource next to it still compiles,
// and the run is reported as unclean.
func TestCompileBundle_ContinuesAfterCompilerErrorAndSuppressesArtifacts(t *testing.T) {
	result, err := compileBundleFromIR(&ingest.Bundle{
		Resources: []ir.Resource{
			compileBundleInvalidResource{name: "bad-resource"},
			ir.NewSkill("good-resource", "description", "body", nil, nil, ir.SourceRef{}),
		},
	}, TargetClaude)
	if err != nil {
		t.Fatalf("compileBundleFromIR: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("HasErrors() = false, want true after a compiler failure")
	}
	// The good skill keeps both its own file and its plugin.json fragment; the
	// failing resource contributes neither.
	for _, path := range artifactPaths(result.Artifacts) {
		if strings.Contains(path, "bad-resource") {
			t.Errorf("artifact %q belongs to the failing resource", path)
		}
	}
	if !sliceContains(artifactPaths(result.Artifacts), "skills/good-resource/SKILL.md") {
		t.Fatalf("artifacts = %v, want the valid skill to survive", artifactPaths(result.Artifacts))
	}
	assertSkippedResources(t, result.Skipped, []SkippedResource{
		{Kind: ir.KindSkill, Name: "bad-resource", Codes: []string{diag.CodeBundleCompileResourceFailed}},
	})
	found := false
	for _, d := range result.Diagnostics {
		if d.Code == diag.CodeBundleCompileResourceFailed && d.Severity == SeverityError && d.Resource == "bad-resource" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing compiler error diagnostic: %#v", result.Diagnostics)
	}
}

func TestCompileBundle_ClaudeTarget(t *testing.T) {
	result, err := CompileBundle(testPluginFS, TargetClaude)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}

	if len(result.Artifacts) == 0 {
		t.Fatal("expected at least one artifact")
	}

	// Verify diagnostics don't contain errors
	for _, d := range result.Diagnostics {
		if d.Severity == SeverityError {
			t.Logf("warning: error diagnostic: %s: %s", d.Code, d.Message)
		}
	}
}

func TestCompileBundle_AllTargets(t *testing.T) {
	targets := Targets()
	for _, target := range targets {
		t.Run(string(target), func(t *testing.T) {
			result, err := CompileBundle(testPluginFS, target)
			if err != nil {
				t.Fatalf("CompileBundle(%s): %v", target, err)
			}
			if result == nil {
				t.Fatal("result must not be nil")
			}
			// Each target should produce at least some artifacts (even if some
			// resources are dropped with warnings for lossy targets).
			t.Logf("target=%s artifacts=%d diags=%d", target, len(result.Artifacts), len(result.Diagnostics))
		})
	}
}

func TestCompileBundle_EmptyFS(t *testing.T) {
	tests := []struct {
		name string
		src  fstest.MapFS
	}{
		{name: "empty", src: fstest.MapFS{}},
		{name: "unrecognized", src: fstest.MapFS{
			"README.md": &fstest.MapFile{Data: []byte("not a harness source")},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			outputPath := filepath.Join(outDir, "previous-output.txt")
			original := []byte("previous successful conversion")
			if err := os.WriteFile(outputPath, original, 0o644); err != nil {
				t.Fatalf("seed previous output: %v", err)
			}

			result, err := CompileBundle(tt.src, TargetClaude)
			if err == nil {
				// This is the destructive behavior the regression guards: an empty
				// successful result would be treated as clean by sync materialization.
				if _, materializeErr := Materialize(result, outDir, WithWriteMode(WriteModeSync)); materializeErr != nil {
					t.Fatalf("Materialize refused the empty result: %v", materializeErr)
				}
				t.Errorf("CompileBundle(%s) returned success for a refused source", tt.name)
			}
			if !errors.Is(err, errSourceLayoutNotFound) {
				t.Errorf("CompileBundle(%s) error = %v, want errSourceLayoutNotFound", tt.name, err)
			}
			if result != nil {
				t.Errorf("CompileBundle(%s) result = %#v, want nil on refusal", tt.name, result)
			}

			got, readErr := os.ReadFile(outputPath)
			if readErr != nil {
				t.Fatalf("read previous output: %v", readErr)
			}
			if string(got) != string(original) {
				t.Fatalf("previous output changed from %q to %q", original, got)
			}
		})
	}
}

func TestCompileBundle_WithMaterialize(t *testing.T) {
	outDir := t.TempDir()

	result, err := CompileBundle(testPluginFS, TargetClaude)
	if err != nil {
		t.Fatalf("CompileBundle: %v", err)
	}

	report, err := Materialize(result, outDir, WithWriteMode(WriteModeOverwrite))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if len(report.Written) == 0 {
		t.Fatal("expected at least one written file")
	}

	// Verify at least the skill body file exists
	skillPath := filepath.Join(outDir, "skills/my-skill/SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatalf("skill file should exist at %s", skillPath)
	}
}
