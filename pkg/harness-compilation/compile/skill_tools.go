package compile

import (
	"strings"

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
		Message:  "allowed-tools from another harness omitted; it is never emitted as a pre-approval on this target",
		Hint:     "Declare pre-approved tools in the target harness's own layout if the skill needs them",
		Source:   DiagSource(s.Source),
	}}
}

// PreApprovalCommandExtra returns the command's extension fields for a target
// that reads allowed-tools as a pre-approval. An allowed-tools list carrying
// another harness's tool provenance is removed with
// skill.allowed-tools-omitted.
func PreApprovalCommandExtra(c *ir.Command, target Target) (map[string]any, []diag.Diagnostic) {
	if c.ToolSource == "" || Target(c.ToolSource) == target {
		return c.Extra, nil
	}
	var extra map[string]any
	for key, value := range c.Extra {
		if strings.EqualFold(key, "allowed-tools") {
			continue
		}
		if extra == nil {
			extra = make(map[string]any, len(c.Extra))
		}
		extra[key] = value
	}
	if len(extra) == len(c.Extra) {
		return c.Extra, nil
	}
	return extra, []diag.Diagnostic{{
		Code:     diag.CodeSkillAllowedToolsOmitted,
		Severity: diag.SeverityWarning,
		Target:   string(target),
		Resource: c.Name(),
		Message:  "allowed-tools from another harness omitted; it is never emitted as a pre-approval on this target",
		Hint:     "Declare pre-approved tools in the target harness's own layout if the command needs them",
		Source:   DiagSource(c.Source),
	}}
}
