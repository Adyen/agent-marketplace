package harnesscompile

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func codexLiteralFixture(value string) fstest.MapFS {
	return fstest.MapFS{".codex/config.toml": {Data: []byte(fmt.Sprintf("[mcp_servers.fixture]\nurl = 'https://example.invalid/mcp'\n[mcp_servers.fixture.http_headers]\nX-Literal = %q\n", value))}}
}

func TestCodexMCPLiteralDoesNotBecomeEnvironmentReference(t *testing.T) {
	t.Setenv("SYNTHETIC_KEY", "must-not-read-host-value")
	for _, target := range []Harness{HarnessWindsurf, HarnessCodex, HarnessClaude, HarnessFactory, HarnessGemini} {
		result, err := Convert(codexLiteralFixture("$SYNTHETIC_KEY"), HarnessCodex, target)
		if err != nil {
			t.Fatal(err)
		}
		if target == HarnessGemini {
			if len(result.Artifacts) != 0 || !result.HasErrors() || !diag.HasCode(result.Diagnostics, diag.CodeMcpGeminiInterpolationUnrepresentable) {
				t.Fatalf("Codex literal would gain interpolation on Gemini: %+v", result)
			}
			continue
		}
		if result.HasErrors() || len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), "$SYNTHETIC_KEY") || strings.Contains(string(result.Artifacts[0].Bytes), "must-not-read-host-value") {
			t.Fatalf("compatible literal was lost or resolved for %s: %+v", target, result)
		}
	}
}

func TestCanonicalMCPBareDollarStaysLiteralOnWindsurf(t *testing.T) {
	resource := ir.NewMcpServerWithOptions("canonical", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.invalid/mcp"), ir.WithMcpHeaders(map[string]string{"X-Variable": "$SYNTHETIC_KEY"}))
	result, err := Compile(resource, TargetWindsurf)
	if err != nil || result.HasErrors() || len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), `"$SYNTHETIC_KEY"`) || strings.Contains(string(result.Artifacts[0].Bytes), "${env:") {
		t.Fatalf("canonical bare dollar literal changed: %v %+v", err, result)
	}
}

func TestCodexMCPSafeLiteralControls(t *testing.T) {
	for _, value := range []string{"ordinary", "$lowercase", "Bearer $SYNTHETIC_KEY", "$SYNTHETIC_KEY/path", "$"} {
		result, err := Convert(codexLiteralFixture(value), HarnessCodex, HarnessWindsurf)
		if err != nil || result.HasErrors() || len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), value) {
			t.Fatalf("safe literal was rejected or changed: %q, %v %+v", value, err, result)
		}
	}
	for _, fields := range []string{"command = '$SYNTHETIC_KEY'", "command = 'server'\nargs = ['$SYNTHETIC_KEY']", "command = 'server'\ncwd = '$SYNTHETIC_KEY'", "command = 'server'\n[mcp_servers.fixture.env]\nTOKEN = '$SYNTHETIC_KEY'"} {
		src := fstest.MapFS{".codex/config.toml": {Data: []byte("[mcp_servers.fixture]\n" + fields + "\n")}}
		result, err := Convert(src, HarnessCodex, HarnessWindsurf)
		if err != nil {
			t.Fatal(err)
		}
		if result.HasErrors() || len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), `"$SYNTHETIC_KEY"`) || strings.Contains(string(result.Artifacts[0].Bytes), "${env:") {
			t.Fatalf("native literal field was not kept literal: %+v", result)
		}
	}
}
