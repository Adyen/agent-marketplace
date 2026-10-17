package cursor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mdc"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"

	// Register in compile registry on import.
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
)

func init() {
	compile.Register(&cursorCompiler{})
}

// cursorCompiler implements compile.Compiler for the Cursor target.
// Cursor is isomorphic to Factory with different directory names:
// skills/<name>/SKILL.md, commands/<name>.md, agents/<name>.md,
// hooks/hooks.json, mcp.json.
type cursorCompiler struct{}

func newCursorCompiler() *cursorCompiler { return &cursorCompiler{} }

func (c *cursorCompiler) Target() mapping.Target { return mapping.TargetCursor }

func (c *cursorCompiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return c.TranslateWithOptions(r, compile.Options{})
}

func (c *cursorCompiler) TranslateWithOptions(r ir.Resource, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	switch r.Kind() {
	case ir.KindSkill:
		return c.translateSkill(r.(*ir.Skill))
	case ir.KindCommand:
		return c.translateCommand(r.(*ir.Command))
	case ir.KindSubagent:
		return c.translateSubagent(r.(*ir.Subagent), opts)
	case ir.KindHook:
		return c.translateHook(r.(*ir.Hook), opts)
	case ir.KindMcpServer:
		return c.translateMcpServer(r.(*ir.McpServer), opts)
	case ir.KindRule:
		return c.translateRule(r.(*ir.Rule))
	default:
		return nil, nil, fmt.Errorf("cursor: unsupported resource kind %v", r.Kind())
	}
}

// translateRule emits rules/<name>.mdc (materialized under .cursor/rules/) with
// YAML frontmatter (description, globs, alwaysApply) followed by the Markdown
// body. Cursor and Windsurf share the .mdc rule format.
func (c *cursorCompiler) translateRule(r *ir.Rule) ([]artifact.Artifact, []diag.Diagnostic, error) {
	body, diags, err := mdc.RenderWithDiagnostics(r, mapping.TargetCursor)
	if err != nil {
		return nil, nil, fmt.Errorf("cursor: render rule frontmatter: %w", err)
	}
	return []artifact.Artifact{{Path: fmt.Sprintf("rules/%s.mdc", r.Name()), Bytes: []byte(body)}}, diags, nil
}

// translateSkill emits skills/<name>/SKILL.md with YAML frontmatter.
func (c *cursorCompiler) translateSkill(s *ir.Skill) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var fm strings.Builder
	fm.WriteString("---\n")
	if err := writeYAMLKey(&fm, "name", s.Name()); err != nil {
		return nil, nil, err
	}
	if err := writeYAMLKey(&fm, "description", s.Description); err != nil {
		return nil, nil, err
	}
	if s.Triggers != nil {
		if err := writeYAMLKey(&fm, "triggers", s.Triggers); err != nil {
			return nil, nil, err
		}
	}
	if s.ArgumentHint != "" {
		if err := writeYAMLKey(&fm, "argument-hint", s.ArgumentHint); err != nil {
			return nil, nil, err
		}
	}
	if s.Model != "" {
		if err := writeYAMLKey(&fm, "model", s.Model); err != nil {
			return nil, nil, err
		}
	}
	allowedTools, toolDiags := compile.PreApprovalSkillTools(s, mapping.TargetCursor)
	if allowedTools != nil {
		if err := writeYAMLKey(&fm, "allowed-tools", allowedTools); err != nil {
			return nil, nil, err
		}
	}
	permissions := compile.SkillPermissionsValueWithExtra(s.Permissions.Allow, s.Permissions.Ask, s.Permissions.Deny, s.Permissions.Extra, s.PermissionsSet)
	if permissions != nil {
		if err := writeYAMLKey(&fm, "permissions", permissions); err != nil {
			return nil, nil, err
		}
	}
	if s.Subagent != "" {
		if err := writeYAMLKey(&fm, "subagent", s.Subagent); err != nil {
			return nil, nil, err
		}
	}
	if s.Agent != "" {
		if err := writeYAMLKey(&fm, "agent", s.Agent); err != nil {
			return nil, nil, err
		}
	}
	extraKeys, extraDiags := compile.ExtraKeys(s.Extra, compile.PresentFields(map[string]bool{"name": true, "description": true, "triggers": s.Triggers != nil, "argument-hint": s.ArgumentHint != "", "model": s.Model != "", "allowed-tools": s.AllowedTools != nil, "permissions": permissions != nil, "subagent": s.Subagent != "", "agent": s.Agent != ""}), mapping.TargetCursor, s.Name(), diag.SourceRef(s.Source))
	extraDiags = append(append(toolDiags, compile.SupportFileDiagnostics(s, mapping.TargetCursor)...), extraDiags...)
	for _, key := range extraKeys {
		if err := writeYAMLKey(&fm, key, s.Extra[key]); err != nil {
			return nil, nil, err
		}
	}
	fm.WriteString("---\n")
	fm.WriteString(s.Body)

	return []artifact.Artifact{
		{
			Path:  fmt.Sprintf("skills/%s/SKILL.md", s.Name()),
			Bytes: []byte(fm.String()),
		},
	}, extraDiags, nil
}

func writeYAMLKey(builder *strings.Builder, key string, value any) error {
	line, err := compile.RenderYAMLKey(key, value)
	if err != nil {
		return err
	}
	builder.WriteString(line)
	return nil
}

// translateCommand emits commands/<name>.md with YAML frontmatter.
func (c *cursorCompiler) translateCommand(cmd *ir.Command) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var fm strings.Builder
	fm.WriteString("---\n")
	if err := writeYAMLKey(&fm, "name", cmd.Name()); err != nil {
		return nil, nil, err
	}
	if err := writeYAMLKey(&fm, "description", cmd.Description); err != nil {
		return nil, nil, err
	}
	extra, toolDiags := compile.PreApprovalCommandExtra(cmd, mapping.TargetCursor)
	extraKeys, diags := compile.ExtraKeys(extra, compile.PresentFields(map[string]bool{"name": true, "description": true}), mapping.TargetCursor, cmd.Name(), diag.SourceRef(cmd.Source))
	diags = append(toolDiags, diags...)
	for _, key := range extraKeys {
		fm.WriteString("\n")
		if err := writeYAMLKey(&fm, key, extra[key]); err != nil {
			return nil, nil, err
		}
	}
	fm.WriteString("---\n")
	fm.WriteString(cmd.Body)
	return []artifact.Artifact{{Path: fmt.Sprintf("commands/%s.md", cmd.Name()), Bytes: []byte(fm.String())}}, diags, nil
}

// translateSubagent emits agents/<name>.md with YAML frontmatter.
// Cursor uses a schema closer to Claude's: model and tools are mapped via
// the mapping tables.
func (c *cursorCompiler) translateSubagent(sub *ir.Subagent, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var dgs []diag.Diagnostic
	if len(sub.DisallowedTools) > 0 {
		dgs = append(dgs, diag.Diagnostic{Code: diag.CodeSubagentSecurityRestrictionUnsupported, Severity: diag.SeverityError, Target: string(mapping.TargetCursor), Resource: sub.Name(), Message: "Cursor cannot represent disallowedTools; subagent artifact suppressed", Hint: "Use a target that supports disallowedTools or remove the restriction explicitly", Source: diag.SourceRef(sub.Source)})
	}
	if sub.Isolation != "" {
		dgs = append(dgs, diag.Diagnostic{Code: diag.CodeSubagentSecurityRestrictionUnsupported, Severity: diag.SeverityError, Target: string(mapping.TargetCursor), Resource: sub.Name(), Message: "Cursor cannot represent isolation; subagent artifact suppressed", Hint: "Use a target that supports isolation or remove the restriction explicitly", Source: diag.SourceRef(sub.Source)})
	}
	if len(dgs) > 0 {
		return nil, dgs, nil
	}

	// Resolve model and reasoning together through the shared catalog.
	selection, modelDiags, err := compile.SubagentModel(sub, mapping.TargetCursor, opts)
	if err != nil {
		return nil, nil, err
	}
	mappedModel := selection.Model
	dgs = append(dgs, modelDiags...)

	// Map tools via the shipped tool catalogs. Cursor subagents have no
	// tool allow-list surface at all, so any non-empty restriction is
	// unenforceable and suppresses the artifact rather than emitting a field
	// Cursor ignores (design D5). An explicit Cursor extension Tools field is
	// an authored-native value and bypasses translation, as before.
	var mappedTools []string
	extTools := sub.Ext.Cursor != nil && sub.Ext.Cursor.Tools != nil
	emittedTools, uncoveredTools, toolErr := toolpolicy.TranslateToolList(mapping.Target(sub.ToolSource), mapping.TargetCursor, toolpolicy.SurfaceSubagentTools, sub.Tools, opts.ToolOverride())
	switch {
	case errors.Is(toolErr, toolpolicy.ErrNoSurface) && extTools:
		// Sidecar-carried native tools are emitted verbatim below.
	case errors.Is(toolErr, toolpolicy.ErrNoSurface):
		dgs = append(dgs, diag.Diagnostic{
			Code:     diag.CodeSubagentCursorToolUnknown,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetCursor),
			Resource: sub.Name(),
			Message:  "Cursor cannot map an explicit subagent tool; artifact suppressed",
			Hint:     "Add the tool to the mapping table or remove the explicit tool",
			Source:   diag.SourceRef(sub.Source),
		})
	case toolErr != nil:
		return nil, nil, fmt.Errorf("cursor: translate subagent tools: %w", toolErr)
	default:
		mappedTools = emittedTools
		if sub.Tools != nil && mappedTools == nil {
			mappedTools = []string{}
		}
		if extTools {
			break
		}
		for range uncoveredTools {
			dgs = append(dgs, diag.Diagnostic{
				Code:     diag.CodeSubagentCursorToolUnknown,
				Severity: diag.SeverityError,
				Target:   string(mapping.TargetCursor),
				Resource: sub.Name(),
				Message:  "Cursor cannot map an explicit subagent tool; artifact suppressed",
				Hint:     "Add the tool to the mapping table or remove the explicit tool",
				Source:   diag.SourceRef(sub.Source),
			})
		}
	}
	if diag.HasErrors(dgs) {
		return nil, dgs, nil
	}

	effort := selection.Effort
	if sidecar := sub.Ext.Cursor; sidecar != nil {
		if !compile.SidecarToolsWithinCanonical(mapping.Target(sub.ToolSource), sub.Tools, sidecar.Tools, mapping.TargetCursor, opts.ToolOverride()) {
			return nil, append(dgs, diag.Diagnostic{
				Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
				Severity: diag.SeverityError,
				Target:   string(mapping.TargetCursor),
				Resource: sub.Name(),
				Message:  "A preserved Cursor tool list grants tools the canonical subagent does not; subagent suppressed",
				Hint:     "Remove the preserved sidecar or align it with the canonical tool list",
				Source:   diag.SourceRef(sub.Source),
			}), nil
		}
		if sidecar.Tools != nil {
			mappedTools = sidecar.Tools
		}
	}

	// Build YAML frontmatter (Cursor schema is closer-to-Claude).
	var fm strings.Builder
	fm.WriteString("---\n")
	if err := writeYAMLKey(&fm, "name", sub.Name()); err != nil {
		return nil, nil, err
	}
	if err := writeYAMLKey(&fm, "description", sub.Description); err != nil {
		return nil, nil, err
	}
	if mappedModel != "" {
		if err := writeYAMLKey(&fm, "model", mappedModel); err != nil {
			return nil, nil, err
		}
	}
	if mappedTools != nil {
		if err := writeYAMLKey(&fm, "tools", mappedTools); err != nil {
			return nil, nil, err
		}
	}
	if effort != "" {
		if err := writeYAMLKey(&fm, "effort", effort); err != nil {
			return nil, nil, err
		}
	}
	if sub.MaxNesting != nil {
		if err := writeYAMLKey(&fm, "max-nesting", *sub.MaxNesting); err != nil {
			return nil, nil, err
		}
	}
	extra := sub.Extra
	if compile.SubagentReadOnly(sub) {
		extra = compile.WithExtraKey(extra, "readonly", true)
	}
	extraKeys, extraDiags := compile.ModelExtraKeys(extra, compile.PresentFields(map[string]bool{"name": true, "description": true, "model": mappedModel != "", "tools": mappedTools != nil, "effort": effort != "", "disallowedTools": false, "isolation": false, "max-nesting": sub.MaxNesting != nil, "maxNesting": false}), mapping.TargetCursor, sub.Name(), diag.SourceRef(sub.Source))
	for _, key := range extraKeys {
		if err := writeYAMLKey(&fm, key, extra[key]); err != nil {
			return nil, nil, err
		}
	}
	dgs = append(dgs, extraDiags...)
	fm.WriteString("---\n")
	fm.WriteString(sub.Body)

	return []artifact.Artifact{
		{
			Path:  fmt.Sprintf("agents/%s.md", sub.Name()),
			Bytes: []byte(fm.String()),
		},
	}, dgs, nil
}

// translateHook emits a hooks/hooks.json fragment.
//
// Event, matcher, timeout, handler type, and command translation come from
// the catalog-driven hookpolicy resolver (OpenSpec native-hook-integration,
// task 3.6): canonical events compile to Cursor's native names (preToolUse,
// beforeSubmitPrompt, ...), matchers translate through the tool catalogs,
// and handler types the catalog does not declare suppress this one entry
// with an error rather than passing through unchecked.
func (c *cursorCompiler) translateHook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	scope := hookpolicy.ScopeRepository
	if opts.Scope() == compile.ScopeUser {
		scope = hookpolicy.ScopeMachine
	}
	resolved := hookpolicy.Resolve(h, target.TargetCursor, scope, opts.PluginRoot())
	if resolved.Failed() {
		return nil, resolved.Diagnostics, nil
	}
	// The handler type selects the payload: a command hook carrying a
	// prompt has no Cursor representation and must not emit both payloads.
	if h.Prompt != "" && resolved.Type != "prompt" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookPromptUnsupported, Severity: diag.SeverityError, Target: string(mapping.TargetCursor), Resource: h.Name(), Message: "Cursor cannot represent a command hook carrying a prompt payload; hook artifact suppressed", Hint: "Use a prompt hook or remove the prompt payload", Source: diag.SourceRef(h.Source)}}, nil
	}

	hookType := resolved.Type
	if sidecar := h.Ext.Cursor; sidecar != nil && sidecar.HookType != "" && sidecar.HookType != resolved.Type {
		return nil, []diag.Diagnostic{{
			Code:     diag.CodeHookCursorUnsupportedType,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetCursor),
			Resource: h.Name(),
			Message:  "A preserved Cursor hook type contradicts the catalog-resolved type; hook suppressed",
			Hint:     "Remove the preserved sidecar or align it with the declared hook type",
			Source:   diag.SourceRef(h.Source),
		}}, nil
	}

	hookEntry := map[string]any{
		"event": resolved.Event,
		"type":  hookType,
	}
	if hookType == "command" && resolved.Command != "" {
		hookEntry["command"] = resolved.Command
	}
	if hookType == "prompt" && h.Prompt != "" {
		hookEntry["prompt"] = h.Prompt
	}
	if resolved.Matcher != "" {
		hookEntry["matcher"] = resolved.Matcher
	}
	if resolved.Timeout != nil {
		hookEntry["timeout"] = *resolved.Timeout
	}
	extraKeys, diags := compile.HookExtraKeys(h.Extra, compile.PresentFields(map[string]bool{"event": true, "matcher": resolved.Matcher != "", "type": true, "command": resolved.Command != "", "prompt": h.Prompt != "", "timeout": resolved.Timeout != nil}), mapping.TargetCursor, h.Name(), diag.SourceRef(h.Source))
	for _, key := range extraKeys {
		hookEntry[key] = h.Extra[key]
	}
	diags = append(resolved.Diagnostics, diags...)

	// Wrap in the hooks envelope matching Claude format.
	hooksDoc := map[string]any{
		"hooks": map[string]any{
			resolved.Event: []any{hookEntry},
		},
	}

	bytes, err := json.MarshalIndent(hooksDoc, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("cursor: marshal hook JSON: %w", err)
	}

	art := artifact.Artifact{
		Path:        "hooks/hooks.json",
		Bytes:       bytes,
		Fragment:    true,
		FragmentKey: "hooks",
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""

	return []artifact.Artifact{art}, diags, nil
}

// translateMcpServer emits an mcp.json fragment.
func (c *cursorCompiler) translateMcpServer(m *ir.McpServer, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	serverEntry, canonical := compile.McpServerEntry(m, func(s string) string { return s }, true)
	extraKeys, diags := compile.ExtraKeys(m.Extra, canonical, mapping.TargetCursor, m.Name(), diag.SourceRef(m.Source))
	diags = append(compile.McpCredentialDiagnostics(m, mapping.TargetCursor, compile.WithMcpOptions(opts)), diags...)
	for _, k := range extraKeys {
		serverEntry[k] = m.Extra[k]
	}

	doc := map[string]any{
		"mcpServers": map[string]any{
			m.Name(): serverEntry,
		},
	}

	bytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("cursor: marshal MCP JSON: %w", err)
	}

	art := artifact.Artifact{
		Path:        "mcp.json",
		Bytes:       bytes,
		Fragment:    true,
		FragmentKey: "mcp",
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""
	return []artifact.Artifact{art}, diags, nil
}
