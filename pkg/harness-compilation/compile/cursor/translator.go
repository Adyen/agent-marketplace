package cursor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mdc"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

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
	if s.AllowedTools != nil {
		if err := writeYAMLKey(&fm, "allowed-tools", s.AllowedTools); err != nil {
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
	extraDiags = append(compile.SupportFileDiagnostics(s, mapping.TargetCursor), extraDiags...)
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
	extraKeys, diags := compile.ExtraKeys(cmd.Extra, compile.PresentFields(map[string]bool{"name": true, "description": true}), mapping.TargetCursor, cmd.Name(), diag.SourceRef(cmd.Source))
	for _, key := range extraKeys {
		fm.WriteString("\n")
		if err := writeYAMLKey(&fm, key, cmd.Extra[key]); err != nil {
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

	// Map model via mapping table.
	mappedModel, modelOK := mapping.ModelWithOverride(sub.Model, mapping.TargetCursor, opts.ModelOverride())
	if !modelOK && sub.Model != "" {
		dgs = append(dgs, diag.Diagnostic{
			Code:     diag.CodeSubagentCursorModelPassthrough,
			Severity: diag.SeverityInfo,
			Target:   string(mapping.TargetCursor),
			Resource: sub.Name(),
			Message:  fmt.Sprintf("model %q not in mapping table; passed through unchanged", sub.Model),
			Hint:     "Add the model to the mapping table if a Cursor-specific ID exists",
			Source:   diag.SourceRef(sub.Source),
		})
	}

	// Map tools via mapping table.
	var mappedTools []string
	if sub.Tools != nil {
		mappedTools = make([]string, 0, len(sub.Tools))
	}
	for _, toolID := range sub.Tools {
		mapped, toolOk := mapping.ToolWithOverride(toolID, mapping.TargetCursor, opts.ToolOverride())
		if !toolOk {
			dgs = append(dgs, diag.Diagnostic{
				Code:     diag.CodeSubagentCursorToolUnknown,
				Severity: diag.SeverityError,
				Target:   string(mapping.TargetCursor),
				Resource: sub.Name(),
				Message:  "Cursor cannot map an explicit subagent tool; artifact suppressed",
				Hint:     "Add the tool to the mapping table or remove the explicit tool",
				Source:   diag.SourceRef(sub.Source),
			})
		} else {
			mappedTools = append(mappedTools, mapped...)
		}
	}
	if diag.HasErrors(dgs) {
		return nil, dgs, nil
	}

	effort := sub.Effort
	if sidecar := sub.Ext.Cursor; sidecar != nil {
		if !compile.SidecarToolsWithinCanonical(sub.Tools, sidecar.Tools, mapping.TargetCursor, opts.ToolOverride()) {
			return nil, []diag.Diagnostic{{
				Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
				Severity: diag.SeverityError,
				Target:   string(mapping.TargetCursor),
				Resource: sub.Name(),
				Message:  "A preserved Cursor tool list grants tools the canonical subagent does not; subagent suppressed",
				Hint:     "Remove the preserved sidecar or align it with the canonical tool list",
				Source:   diag.SourceRef(sub.Source),
			}}, nil
		}
		if sidecar.Model != "" {
			mappedModel = sidecar.Model
		}
		if sidecar.Tools != nil {
			mappedTools = sidecar.Tools
		}
		if sidecar.Effort != "" {
			effort = sidecar.Effort
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
	extraKeys, extraDiags := compile.ExtraKeys(sub.Extra, compile.PresentFields(map[string]bool{"name": true, "description": true, "model": mappedModel != "", "tools": mappedTools != nil, "effort": effort != "", "disallowedTools": false, "isolation": false, "max-nesting": sub.MaxNesting != nil, "maxNesting": false}), mapping.TargetCursor, sub.Name(), diag.SourceRef(sub.Source))
	for _, key := range extraKeys {
		if err := writeYAMLKey(&fm, key, sub.Extra[key]); err != nil {
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
// Cursor accepts the same hook format as Claude, but with
// ${CLAUDE_PLUGIN_ROOT} → ${CURSOR_PLUGIN_ROOT} env token swap.
func (c *cursorCompiler) translateHook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	hookType := h.Type
	if sidecar := h.Ext.Cursor; sidecar != nil && sidecar.HookType != "" && sidecar.HookType != h.Type {
		return nil, []diag.Diagnostic{{
			Code:     diag.CodeHookCursorUnsupportedType,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetCursor),
			Resource: h.Name(),
			Message:  "A preserved Cursor hook type contradicts the canonical hook type; hook suppressed",
			Hint:     "Remove the preserved sidecar or align it with the declared hook type",
			Source:   diag.SourceRef(h.Source),
		}}, nil
	}

	// Prompt hooks have no Cursor primitive and must not become empty commands.
	if hookType == "prompt" || h.Prompt != "" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookPromptUnsupported, Severity: diag.SeverityWarning, Target: string(mapping.TargetCursor), Resource: h.Name(), Message: "Cursor cannot represent prompt hooks; hook artifact suppressed", Hint: "Use a command hook on Cursor", Source: diag.SourceRef(h.Source)}}, nil
	}
	// Only command-type hooks are supported.
	if hookType != "command" {
		return nil, []diag.Diagnostic{{
			Code:     diag.CodeHookCursorUnsupportedType,
			Severity: diag.SeverityWarning,
			Target:   string(mapping.TargetCursor),
			Resource: h.Name(),
			Message:  fmt.Sprintf("Cursor only supports command-type hooks; type %q dropped", h.Type),
			Hint:     "Convert the hook to a command type or remove it",
			Source:   diag.SourceRef(h.Source),
		}}, nil
	}

	// Apply env token substitution to the command string.
	command := substituteEnvTokens(h.Command)

	hookEntry := map[string]any{
		"event":   h.Event,
		"command": command,
	}
	if h.Matcher != "" {
		hookEntry["matcher"] = h.Matcher
	}
	if h.Timeout != nil {
		hookEntry["timeout"] = *h.Timeout
	}
	hookEntry["type"] = hookType
	extraKeys, diags := compile.HookExtraKeys(h.Extra, compile.PresentFields(map[string]bool{"event": true, "matcher": h.Matcher != "", "type": hookType != "", "command": h.Command != "", "prompt": false, "timeout": h.Timeout != nil}), mapping.TargetCursor, h.Name(), diag.SourceRef(h.Source))
	for _, key := range extraKeys {
		hookEntry[key] = h.Extra[key]
	}

	// Wrap in the hooks envelope matching Claude format.
	hooksDoc := map[string]any{
		"hooks": map[string]any{
			h.Event: []any{hookEntry},
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

// substituteEnvTokens replaces source env tokens with Cursor equivalents.
func substituteEnvTokens(s string) string {
	replacement, literal := mapping.EnvToken("${CLAUDE_PLUGIN_ROOT}", mapping.TargetCursor)
	if !literal && replacement != "${CLAUDE_PLUGIN_ROOT}" {
		s = strings.ReplaceAll(s, "${CLAUDE_PLUGIN_ROOT}", replacement)
	}
	return s
}
