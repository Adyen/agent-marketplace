package compile

import (
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// SubagentTools resolves a subagent tool allowlist for target.
//
// native is the target's own list retained at ingestion; it is nil when the
// source layout belongs to another harness. A native list is emitted exactly
// and in order, applying only explicit invocation overrides: the curated table
// translates canonical identifiers and is not an inventory of each runtime's
// built-ins or dynamically registered MCP tools.
//
// Without a native list every canonical tool must map. Each unmapped tool
// yields one copy of unknown and no tools are returned, so the caller
// suppresses the subagent instead of emitting a partial or default-tool
// policy. Both paths keep an absent list nil and an explicit empty list empty.
func SubagentTools(canonical, native []string, target Target, opts Options, unknown diag.Diagnostic) ([]string, []diag.Diagnostic) {
	if native != nil {
		tools := make([]string, len(native))
		for i, tool := range native {
			if override, ok := opts.ToolOverride()[tool]; ok {
				tool = override
			}
			tools[i] = tool
		}
		return tools, nil
	}
	if canonical == nil {
		return nil, nil
	}
	tools := make([]string, 0, len(canonical))
	var diagnostics []diag.Diagnostic
	for _, tool := range canonical {
		mapped, ok := mapping.ToolWithOverride(tool, target, opts.ToolOverride())
		if !ok {
			diagnostics = append(diagnostics, unknown)
			continue
		}
		tools = append(tools, mapped...)
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	return tools, nil
}
