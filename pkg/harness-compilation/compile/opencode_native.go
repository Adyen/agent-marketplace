package compile

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func openCodeNativeLossDiagnostics(resource ir.Resource, target Target) []diag.Diagnostic {
	if target == TargetOpenCode {
		return nil
	}
	unsupported, lostMetadata := false, false
	switch r := resource.(type) {
	case *ir.Subagent:
		if n := r.Ext.OpenCode; n != nil {
			lostMetadata = n.Hidden != nil || n.Temperature != nil
			unsupported = n.Permission != nil || (n.Tools != nil || n.ToolsSet) || (n.Mode != "" && n.Mode != "subagent") || n.Steps != nil
		}
	case *ir.Command:
		if n := r.Ext.OpenCode; n != nil {
			lostMetadata = n.Model != ""
			unsupported = n.Agent != "" || n.Subtask != nil
		}
	case *ir.McpServer:
		if n := r.Ext.OpenCode; n != nil && n.MCP != nil {
			unsupported = n.MCP.OAuthDisabled != nil || n.MCP.OAuthScope != "" || n.MCP.Timeout != nil
			if n.MCP.NativeValues {
				values := []string{r.Command, r.URL, r.OAuthClientID, r.OAuthClientSecret}
				values = append(values, r.Args...)
				for _, v := range r.Env {
					values = append(values, v)
				}
				for _, v := range r.Headers {
					values = append(values, v)
				}
				for _, v := range values {
					if strings.Contains(v, "{env:") || strings.Contains(v, "{file:") || strings.Contains(v, "${") {
						unsupported = true
					}
				}
			}
		}
	}
	if !unsupported {
		if lostMetadata {
			return []diag.Diagnostic{{Code: diag.CodeOpenCodeFieldDropped, Severity: diag.SeverityWarning, Target: string(target), Resource: resource.Name(), Source: SourceOf(resource), Message: "Native OpenCode metadata has no equivalent in this target and was omitted"}}
		}
		return nil
	}
	return []diag.Diagnostic{{Code: diag.CodeOpenCodeNativeSemanticsUnsupported, Severity: diag.SeverityError, Target: string(target), Resource: resource.Name(), Source: SourceOf(resource), Message: "Native OpenCode execution or reference semantics have no verified equivalent in this target; resource omitted"}}
}
