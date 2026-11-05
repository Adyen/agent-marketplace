package analyze

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	ext "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: harness-opencode/sensitive-native-fields-are-redacted-in-consumer-findings
func TestOpenCodeNativeFindingsAndInspection(t *testing.T) {
	const secret = "SYNTHETIC-NOT-A-CREDENTIAL-abcdef"
	mcp := ir.NewMcpServerWithOptions("docs", "", nil, nil, nil, ir.SourceRef{}, ir.WithMcpURL("https://example.invalid/mcp?token="+secret), ir.WithMcpOAuth("", secret, ""))
	mcp.Ext.OpenCode = &ext.OpenCode{MCP: &ext.OpenCodeMCP{NativeValues: true, OAuthScope: secret}}
	agent := ir.NewSubagent("reviewer", "Review", "Permitted body", "", nil, "", nil, "", nil, ir.SourceRef{})
	agent.Ext.OpenCode = &ext.OpenCode{Model: "provider/" + secret}
	hook := ir.NewHookWithMatcher("check", "PreToolUse", "Bash", "printf '<!--"+secret+"-->'", "command", nil, ir.SourceRef{})
	for _, resource := range []ir.Resource{mcp, agent, hook} {
		report, err := NewEngine().Analyze(t.Context(), resource, nil, DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(report.Findings)
		if strings.Contains(string(encoded), secret) {
			t.Fatal("native payload disclosed in findings")
		}
		for _, finding := range report.Findings {
			if finding.SourceRange == nil || finding.SourceRange.File == "" {
				t.Fatal("finding lost source attribution")
			}
		}
	}
	body := []byte("Permitted command body\n")
	projection, err := inspection.Build(store.ResourceCoordinate{Marketplace: "test", Registry: "resources", Kind: "command", Name: "check"}, "sha256:"+strings.Repeat("a", 64), body, nil, 10, 1024, 4096)
	if err != nil || string(projection.Bodies[inspection.Digest(body)]) != string(body) {
		t.Fatal("permitted inspection body missing")
	}
}
