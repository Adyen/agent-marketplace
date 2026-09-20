package harnesscompile

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
)

func TestOpaqueMetadataCannotActivateNativeControls(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, forbidden string
		target                      Target
	}{
		{"gemini-inline-mcp", "droids/guard.md", "mcpServers:\n  injected:\n    command: must-not-activate\n", "mcpServers:", TargetGemini},
		{"windsurf-auto-execution", "commands/guard.md", "auto_execution_mode: always\n", "auto_execution_mode:", TargetWindsurf},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := fstest.MapFS{tc.path: {Data: []byte("---\nname: guard\ndescription: Guard\nx-note: preserved\n" + tc.body + "---\nBody\n")}}
			result, err := Convert(src, HarnessFactory, Harness(tc.target))
			if err != nil || result.HasErrors() || len(result.Artifacts) != 1 {
				t.Fatalf("conversion failed: %v %+v", err, result)
			}
			body := string(result.Artifacts[0].Bytes)
			if strings.Contains(body, tc.forbidden) || strings.Contains(body, "must-not-activate") {
				t.Errorf("opaque metadata gained native authority: %s", body)
			}
			if !strings.Contains(body, "x-note: preserved") || !diag.HasCode(result.Diagnostics, diag.CodeCanonicalFieldOverridesExtra) {
				t.Fatalf("missing preservation or omission diagnostic: %+v", result)
			}
		})
	}
}

func TestNativeWorkflowModeRejectsUnsupportedShapes(t *testing.T) {
	for _, value := range []string{"null", "1", "true", "[always]", "{mode: always}"} {
		src := fstest.MapFS{".windsurf/workflows/native.md": {Data: []byte("---\ndescription: Native\nauto_execution_mode: " + value + "\n---\nBody\n")}}
		result, err := Convert(src, HarnessWindsurf, TargetWindsurf)
		if err == nil && !result.HasErrors() {
			t.Fatalf("unsupported native mode shape was accepted: %s", value)
		}
		if result != nil && len(result.Artifacts) != 0 {
			t.Fatal("unsupported native mode emitted a workflow")
		}
	}
}

func TestNativeMetadataControlsStillRoundTrip(t *testing.T) {
	for _, mode := range []string{"always", "never", ""} {
		src := fstest.MapFS{".windsurf/workflows/native.md": {Data: []byte(fmt.Sprintf("---\ndescription: Native\nauto_execution_mode: %q\nx-note: preserved\n---\nBody\n", mode))}}
		result, err := Convert(src, HarnessWindsurf, TargetWindsurf)
		if err != nil || result.HasErrors() || len(result.Artifacts) != 1 {
			t.Fatalf("native workflow failed: %v %+v", err, result)
		}
		body := string(result.Artifacts[0].Bytes)
		if !strings.Contains(body, "auto_execution_mode:") || !strings.Contains(body, "x-note: preserved") || diag.HasCode(result.Diagnostics, diag.CodeCanonicalFieldOverridesExtra) {
			t.Fatalf("native workflow control lost: %s %+v", body, result.Diagnostics)
		}
		frontmatter, _, err := yamlfm.ParseYAMLFrontmatter(bytes.NewReader(result.Artifacts[0].Bytes))
		if err != nil || frontmatter["auto_execution_mode"] != mode {
			t.Fatalf("native mode changed: %s (%v)", body, err)
		}
	}
	src := fstest.MapFS{".gemini/agents/native.md": {Data: []byte("---\nname: native\ndescription: Native\nkind: local\nmcpServers:\n  private:\n    command: native-inline-server\n    env:\n      API_TOKEN: synthetic-literal\n---\nBody\n")}}
	result, err := Convert(src, HarnessGemini, TargetGemini)
	if err != nil || result.HasErrors() || len(result.Artifacts) != 1 {
		t.Fatalf("native Gemini agent failed: %v %+v", err, result)
	}
	if !strings.Contains(string(result.Artifacts[0].Bytes), "native-inline-server") || !diag.HasCode(result.Diagnostics, diag.CodeMcpLiteralCredential) {
		t.Fatalf("typed inline MCP lost normalization/credential handling: %+v", result)
	}
}
