package compile

import (
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// PreApprovalSkillTools returns the allowed-tools list to emit on a target
// that reads a skill's allowed-tools as a pre-approval. A list carrying
// another harness's tool provenance is never emitted there: it is omitted
// with skill.allowed-tools-omitted.
func PreApprovalSkillTools(s *ir.Skill, target Target) ([]string, []diag.Diagnostic) {
	if s.AllowedTools == nil || s.ToolSource == "" || Target(s.ToolSource) == target {
		return s.AllowedTools, nil
	}
	return nil, []diag.Diagnostic{{
		Code:     diag.CodeSkillAllowedToolsOmitted,
		Severity: diag.SeverityWarning,
		Target:   string(target),
		Resource: s.Name(),
		Message:  "allowed-tools from " + s.ToolSource + " omitted; it is never emitted as a pre-approval on this target",
		Hint:     "Declare pre-approved tools in the target harness's own layout if the skill needs them",
		Source:   DiagSource(s.Source),
	}}
}
