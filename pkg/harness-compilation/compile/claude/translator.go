package claude

import (
	"encoding/json"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
)

func init() {
	compile.Register(&claudeCompiler{})
}

// claudeCompiler is a near-identity passthrough translator for Claude Code.
// All 5 resource types pass through with minimal transformation.
type claudeCompiler struct{}

func (c *claudeCompiler) Target() mapping.Target {
	return mapping.TargetClaude
}

func (c *claudeCompiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return c.TranslateWithOptions(r, compile.Options{})
}

func (c *claudeCompiler) TranslateWithOptions(r ir.Resource, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	switch r.Kind() {
	case ir.KindSkill:
		return c.translateSkill(r.(*ir.Skill), opts)
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
		return nil, nil, fmt.Errorf("claude: unsupported resource kind %v", r.Kind())
	}
}

// translateRule drops the rule with a diagnostic: Claude Code has no rules
// directory primitive (project guidance lives in a single CLAUDE.md file).
func (c *claudeCompiler) translateRule(r *ir.Rule) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return nil, []diag.Diagnostic{{
		Code:     diag.CodeRuleClaudeNoPrimitive,
		Severity: diag.SeverityWarning,
		Target:   string(mapping.TargetClaude),
		Resource: r.Name(),
		Message:  "Claude Code has no rules directory primitive; rule was dropped",
		Hint:     "Claude reads project guidance from CLAUDE.md; reference the rule content there instead",
		Source:   compile.DiagSource(r.Source),
	}}, nil
}

func (c *claudeCompiler) translateSkill(s *ir.Skill, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	selection, modelDiags, err := compile.SkillModel(s, mapping.TargetClaude, opts)
	if err != nil {
		return nil, nil, err
	}
	// Build frontmatter with name, description, triggers, and extra fields.
	fm := compile.Frontmatter{}
	fm.Set("name", s.Name())
	fm.Set("description", s.Description)
	if s.Triggers != nil {
		fm.Set("triggers", s.Triggers)
	}
	if s.ArgumentHint != "" {
		fm.Set("argument-hint", s.ArgumentHint)
	}
	if selection.Model != "" {
		fm.Set("model", selection.Model)
	}
	if s.AllowedTools != nil {
		fm.Set("allowed-tools", s.AllowedTools)
	}
	permissions := compile.SkillPermissionsValueWithExtra(s.Permissions.Allow, s.Permissions.Ask, s.Permissions.Deny, s.Permissions.Extra, s.PermissionsSet)
	if permissions != nil {
		fm.Set("permissions", permissions)
	}
	if s.Subagent != "" {
		fm.Set("subagent", s.Subagent)
	}
	if s.Agent != "" {
		fm.Set("agent", s.Agent)
	}
	canonical := compile.PresentFields(map[string]bool{
		"name": true, "description": true, "triggers": s.Triggers != nil,
		"argument-hint": s.ArgumentHint != "", "model": s.Model != "",
		"allowed-tools": s.AllowedTools != nil, "permissions": permissions != nil,
		"subagent": s.Subagent != "", "agent": s.Agent != "",
	})
	extraKeys, extraDiags := compile.SkillModelExtraKeys(s.Extra, canonical, mapping.TargetClaude, s.Name(), diag.SourceRef(s.Source))
	extraDiags = append(append(modelDiags, compile.SupportFileDiagnostics(s, mapping.TargetClaude)...), extraDiags...)
	for _, k := range extraKeys {
		fm.Set(k, s.Extra[k])
	}

	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("claude: render skill frontmatter: %w", err)
	}
	content := frontmatter + s.Body

	// Skill body artifact at skills/<name>/SKILL.md
	bodyArt := artifact.Artifact{
		Path:  fmt.Sprintf("skills/%s/SKILL.md", s.Name()),
		Bytes: []byte(content),
	}

	// plugin.json fragment with skills entry
	skillEntry := map[string]any{
		"name":        s.Name(),
		"description": s.Description,
	}
	if s.Triggers != nil {
		skillEntry["triggers"] = s.Triggers
	}
	// Merge in Extra fields
	for _, k := range extraKeys {
		skillEntry[k] = s.Extra[k]
	}

	pluginFragment := map[string]any{
		"skills": []any{skillEntry},
	}
	pluginBytes, err := json.Marshal(pluginFragment)
	if err != nil {
		return nil, nil, fmt.Errorf("claude: marshal plugin.json: %w", err)
	}

	pluginArt := artifact.Artifact{
		Path:        ".claude-plugin/plugin.json",
		Bytes:       pluginBytes,
		Fragment:    true,
		FragmentKey: "plugin.json",
	}
	pluginArt.ManagedByField, pluginArt.ManagedByValue = opts.ManagedBy()
	pluginArt.MergeWithExisting = pluginArt.ManagedByField != ""

	return []artifact.Artifact{bodyArt, pluginArt}, extraDiags, nil
}

func (c *claudeCompiler) translateCommand(cmd *ir.Command) ([]artifact.Artifact, []diag.Diagnostic, error) {
	extraKeys, diags := compile.ExtraKeys(cmd.Extra, compile.PresentFields(map[string]bool{"name": true, "description": cmd.Description != ""}), mapping.TargetClaude, cmd.Name(), diag.SourceRef(cmd.Source))
	if len(extraKeys) == 0 && cmd.Description == "" {
		return []artifact.Artifact{{Path: fmt.Sprintf("commands/%s.md", cmd.Name()), Bytes: []byte(cmd.Body)}}, diags, nil
	}
	fm := compile.Frontmatter{}
	fm.Set("name", cmd.Name())
	if cmd.Description != "" {
		fm.Set("description", cmd.Description)
	}
	for _, key := range extraKeys {
		fm.Set(key, cmd.Extra[key])
	}
	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("claude: render command frontmatter: %w", err)
	}
	return []artifact.Artifact{{Path: fmt.Sprintf("commands/%s.md", cmd.Name()), Bytes: []byte(frontmatter + cmd.Body)}}, diags, nil
}

func (c *claudeCompiler) translateSubagent(s *ir.Subagent, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	selection, modelDiags, err := compile.SubagentModel(s, mapping.TargetClaude, opts)
	if err != nil {
		return nil, nil, err
	}
	model, effort := selection.Model, selection.Effort
	tools := s.Tools
	var diags []diag.Diagnostic
	if source := mapping.Target(s.ToolSource); source != "" && source != mapping.TargetClaude {
		// A foreign-harness tool list is re-emitted with Claude catalog
		// names; a name no Claude tool covers suppresses the artifact
		// rather than emitting an unenforced restriction (design D5).
		emitted, uncovered, toolErr := toolpolicy.TranslateToolList(source, mapping.TargetClaude, toolpolicy.SurfaceSubagentTools, s.Tools, opts.ToolOverride())
		if toolErr != nil {
			return nil, nil, fmt.Errorf("claude: translate subagent tools: %w", toolErr)
		}
		tools = emitted
		if s.Tools != nil && tools == nil {
			tools = []string{}
		}
		for range uncovered {
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeSubagentClaudeToolUnknown,
				Severity: diag.SeverityError,
				Target:   string(mapping.TargetClaude),
				Resource: s.Name(),
				Message:  "Claude cannot map an explicit subagent tool; artifact suppressed",
				Hint:     "Use a tool name the Claude tool catalog covers or remove the restriction explicitly",
				Source:   diag.SourceRef(s.Source),
			})
		}
		if len(uncovered) > 0 {
			return nil, append(modelDiags, diags...), nil
		}
	}
	isolation := s.Isolation
	if sidecar := s.Ext.Claude; sidecar != nil {
		if sidecar.Tools != nil {
			tools = sidecar.Tools
		}
		if sidecar.Isolation != "" {
			isolation = sidecar.Isolation
		}
	}

	// Build frontmatter
	fm := compile.Frontmatter{}
	fm.Set("name", s.Name())
	fm.Set("description", s.Description)
	if model != "" {
		fm.Set("model", model)
	}
	if tools != nil {
		fm.Set("tools", tools)
	}
	if effort != "" {
		fm.Set("effort", effort)
	}
	if len(s.DisallowedTools) > 0 {
		fm.Set("disallowedTools", s.DisallowedTools)
	}
	if isolation != "" {
		fm.Set("isolation", isolation)
	}
	if s.MaxNesting != nil {
		fm.Set("max-nesting", *s.MaxNesting)
	}
	extraKeys, extraDiags := compile.ModelExtraKeys(s.Extra, compile.PresentFields(map[string]bool{
		"name": true, "description": true, "model": model != "", "tools": tools != nil,
		"effort": effort != "", "disallowedTools": len(s.DisallowedTools) > 0,
		"isolation": isolation != "", "max-nesting": s.MaxNesting != nil, "maxNesting": false,
	}), mapping.TargetClaude, s.Name(), diag.SourceRef(s.Source))
	for _, k := range extraKeys {
		fm.Set(k, s.Extra[k])
	}

	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("claude: render subagent frontmatter: %w", err)
	}
	content := frontmatter + s.Body

	agentArt := artifact.Artifact{
		Path:  fmt.Sprintf("agents/%s.md", s.Name()),
		Bytes: []byte(content),
	}

	// plugin.json fragment with agents entry
	agentEntry := map[string]any{
		"name":        s.Name(),
		"description": s.Description,
	}
	if model != "" {
		agentEntry["model"] = model
	}
	if tools != nil {
		agentEntry["tools"] = tools
	}
	if effort != "" {
		agentEntry["effort"] = effort
	}
	for _, k := range extraKeys {
		agentEntry[k] = s.Extra[k]
	}

	pluginFragment := map[string]any{
		"agents": []any{agentEntry},
	}
	pluginBytes, err := json.Marshal(pluginFragment)
	if err != nil {
		return nil, nil, fmt.Errorf("claude: marshal plugin.json: %w", err)
	}

	pluginArt := artifact.Artifact{
		Path:        ".claude-plugin/plugin.json",
		Bytes:       pluginBytes,
		Fragment:    true,
		FragmentKey: "plugin.json",
	}
	pluginArt.ManagedByField, pluginArt.ManagedByValue = opts.ManagedBy()
	pluginArt.MergeWithExisting = pluginArt.ManagedByField != ""

	return []artifact.Artifact{agentArt, pluginArt}, append(diags, append(modelDiags, extraDiags...)...), nil
}

func (c *claudeCompiler) translateHook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	// Event, matcher, timeout, handler type, and command translation come
	// from the catalog-driven hookpolicy resolver (OpenSpec
	// native-hook-integration, task 3.5). Canonical input decodes through
	// the Claude source catalog, so events the catalog does not map and
	// handler types it does not declare fail closed instead of passing
	// through unchecked.
	scope := hookpolicy.ScopeRepository
	if opts.Scope() == compile.ScopeUser {
		scope = hookpolicy.ScopeMachine
	}
	resolved := hookpolicy.Resolve(h, mapping.TargetClaude, scope, opts.PluginRoot())
	if resolved.Failed() {
		return nil, resolved.Diagnostics, nil
	}

	hookType := resolved.Type
	if sidecar := h.Ext.Claude; sidecar != nil && sidecar.HookType != "" {
		hookType = sidecar.HookType
	}

	// Build hooks.json fragment without inventing command for prompt-only hooks.
	rule := map[string]any{"type": hookType}
	if resolved.Command != "" {
		rule["command"] = resolved.Command
	}
	if resolved.Matcher != "" {
		rule["matcher"] = resolved.Matcher
	}
	if h.Prompt != "" {
		rule["prompt"] = h.Prompt
	}
	if resolved.Timeout != nil {
		rule["timeout"] = *resolved.Timeout
	}
	extraKeys, diags := compile.HookExtraKeys(h.Extra, compile.PresentFields(map[string]bool{"event": true, "matcher": resolved.Matcher != "", "type": true, "command": resolved.Command != "", "prompt": h.Prompt != "", "timeout": resolved.Timeout != nil}), mapping.TargetClaude, h.Name(), diag.SourceRef(h.Source))
	for _, k := range extraKeys {
		rule[k] = h.Extra[k]
	}
	return c.hookFragmentArtifact(h, opts, resolved.Event, rule, append(resolved.Diagnostics, diags...))
}

// hookFragmentArtifact marshals one resolved rule into the hooks.json
// fragment artifact for the given native event.
func (c *claudeCompiler) hookFragmentArtifact(h *ir.Hook, opts compile.Options, event string, rule map[string]any, diags []diag.Diagnostic) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fragment := map[string]any{
		"hooks": map[string]any{
			event: []any{rule},
		},
	}
	fragBytes, err := json.Marshal(fragment)
	if err != nil {
		return nil, nil, fmt.Errorf("claude: marshal hooks.json: %w", err)
	}

	art := artifact.Artifact{
		Path:        "hooks/hooks.json",
		Bytes:       fragBytes,
		Fragment:    true,
		FragmentKey: "hooks.json",
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""

	return []artifact.Artifact{art}, diags, nil
}

func (c *claudeCompiler) translateMcpServer(m *ir.McpServer, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	// Build .mcp.json fragment: { "mcpServers": { "<name>": { "command": ..., "args": [...], "env": {...} } } }
	serverEntry, canonical := compile.McpServerEntry(m, func(s string) string { return s }, true)
	extraKeys, diags := compile.McpExtraKeys(m.Extra, canonical, mapping.TargetClaude, m.Name(), diag.SourceRef(m.Source))
	diags = append(compile.McpCredentialDiagnostics(m, mapping.TargetClaude, compile.WithMcpOptions(opts)), diags...)
	for _, k := range extraKeys {
		serverEntry[k] = m.Extra[k]
	}

	fragment := map[string]any{
		"mcpServers": map[string]any{
			m.Name(): serverEntry,
		},
	}
	fragBytes, err := json.Marshal(fragment)
	if err != nil {
		return nil, nil, fmt.Errorf("claude: marshal .mcp.json: %w", err)
	}

	art := artifact.Artifact{
		Path:        ".mcp.json",
		Bytes:       fragBytes,
		Fragment:    true,
		FragmentKey: ".mcp.json",
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""

	return []artifact.Artifact{art}, diags, nil
}

// getCompiler returns the registered Claude compiler. Used by tests.
func getCompiler() (compile.Compiler, bool) {
	return compile.Lookup(mapping.TargetClaude)
}
