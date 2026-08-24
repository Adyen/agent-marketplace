package factory

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func init() {
	compile.Register(&factoryCompiler{})
}

// factoryCompiler translates Claude-format IR into Factory Droid format.
type factoryCompiler struct{}

func (f *factoryCompiler) Target() mapping.Target {
	return mapping.TargetFactory
}

func (f *factoryCompiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return f.TranslateWithOptions(r, compile.Options{})
}

func (f *factoryCompiler) TranslateWithOptions(r ir.Resource, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	switch r.Kind() {
	case ir.KindSkill:
		return f.translateSkill(r.(*ir.Skill), opts)
	case ir.KindCommand:
		return f.translateCommand(r.(*ir.Command))
	case ir.KindSubagent:
		return f.translateSubagent(r.(*ir.Subagent), opts)
	case ir.KindHook:
		return f.translateHook(r.(*ir.Hook), opts)
	case ir.KindMcpServer:
		return f.translateMcpServer(r.(*ir.McpServer), opts)
	case ir.KindRule:
		return f.translateRule(r.(*ir.Rule))
	default:
		return nil, nil, fmt.Errorf("factory: unsupported resource kind %v", r.Kind())
	}
}

// translateRule produces rules/<name>.md as plain Markdown. Factory rules
// (.factory/rules/) are plain-markdown convention files with no frontmatter
// and no glob support (https://docs.factory.ai/guides/power-user/rules-conventions),
// so any .mdc metadata (globs/alwaysApply) is dropped with a diagnostic and the
// rule body is emitted verbatim.
func (f *factoryCompiler) translateRule(r *ir.Rule) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var diags []diag.Diagnostic
	if len(r.Globs) > 0 || r.AlwaysApply || r.Description != "" {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeRuleFactoryMetadataDropped,
			Severity: diag.SeverityWarning,
			Target:   string(mapping.TargetFactory),
			Resource: r.Name(),
			Message:  "Factory rules are plain markdown; description/globs/alwaysApply metadata was dropped",
			Hint:     "Factory does not support rule frontmatter (description or conditional glob-scoped application); the rule body is emitted verbatim",
			Source:   compile.DiagSource(r.Source),
		})
	}

	art := artifact.Artifact{
		Path:  fmt.Sprintf("rules/%s.md", r.Name()),
		Bytes: []byte(r.Body),
	}
	return []artifact.Artifact{art}, diags, nil
}

// translateSkill produces skills/<name>/SKILL.md with YAML frontmatter
// containing name, description, triggers, and any extra fields.
func (f *factoryCompiler) translateSkill(s *ir.Skill, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fm := &compile.Frontmatter{}
	fm.Set("name", s.Name())
	fm.Set("description", s.Description)
	if s.Triggers != nil {
		fm.Set("triggers", s.Triggers)
	}
	if s.ArgumentHint != "" {
		fm.Set("argument-hint", s.ArgumentHint)
	}
	if s.Model != "" {
		fm.Set("model", s.Model)
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
		"name":          true,
		"description":   true,
		"triggers":      s.Triggers != nil,
		"argument-hint": s.ArgumentHint != "",
		"model":         s.Model != "",
		"allowed-tools": s.AllowedTools != nil,
		"permissions":   permissions != nil,
		"subagent":      s.Subagent != "",
		"agent":         s.Agent != "",
	})
	extraKeys, extraDiags := compile.ExtraKeys(s.Extra, canonical, mapping.TargetFactory, s.Name(), diag.SourceRef(s.Source))
	extraDiags = append(compile.SupportFileDiagnostics(s, mapping.TargetFactory), extraDiags...)
	for _, k := range extraKeys {
		fm.Set(k, s.Extra[k])
	}

	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("factory: render skill frontmatter: %w", err)
	}
	content := frontmatter + s.Body

	bodyArt := artifact.Artifact{
		Path:  fmt.Sprintf("skills/%s/SKILL.md", s.Name()),
		Bytes: []byte(content),
	}

	// Factory discovers skills by scanning the skills/ directory, not from
	// plugin.json arrays. The plugin.json manifest is a simple identity file
	// (name/description/version) owned by Factory — generating skill entries
	// in it only causes collisions with Factory's own manifest.
	return []artifact.Artifact{bodyArt}, extraDiags, nil
}

// translateCommand is the same as Claude: commands/<name>.md.
func (f *factoryCompiler) translateCommand(cmd *ir.Command) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fm := &compile.Frontmatter{}
	if cmd.Description != "" {
		fm.Set("description", cmd.Description)
	}
	extraKeys, diags := compile.ExtraKeys(cmd.Extra, compile.PresentFields(map[string]bool{"description": cmd.Description != ""}), mapping.TargetFactory, cmd.Name(), diag.SourceRef(cmd.Source))
	for _, k := range extraKeys {
		fm.Set(k, cmd.Extra[k])
	}

	content := cmd.Body
	if len(fm.Keys()) > 0 {
		frontmatter, err := fm.Render()
		if err != nil {
			return nil, nil, fmt.Errorf("factory: render command frontmatter: %w", err)
		}
		content = frontmatter + cmd.Body
	}

	art := artifact.Artifact{
		Path:  fmt.Sprintf("commands/%s.md", cmd.Name()),
		Bytes: []byte(content),
	}
	return []artifact.Artifact{art}, diags, nil
}

// translateSubagent translates to droids/<name>.md with Factory renormalizations:
//   - effort → reasoningEffort
//   - disallowedTools → drop + emit diagnostic
//   - isolation: worktree → drop + emit diagnostic
//   - model via mapping.Model()
//   - tools via mapping.Tool() (expand Write → Edit,Create)
func (f *factoryCompiler) translateSubagent(s *ir.Subagent, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var diags []diag.Diagnostic
	reasoningEffort := s.Effort
	isolation := ""
	if sidecar := s.Ext.Factory; sidecar != nil {
		if sidecar.ReasoningEffort != "" {
			reasoningEffort = sidecar.ReasoningEffort
		}
		if sidecar.Isolation != "" {
			isolation = sidecar.Isolation
		}
	}

	// Map model
	mappedModel := s.Model
	if s.Model != "" {
		m, ok := mapping.ModelWithOverride(s.Model, mapping.TargetFactory, opts.ModelOverride())
		if ok {
			mappedModel = m
		}
		// Unknown models remain passthrough; unlike explicit unknown tools, a model
		// identifier does not widen the subagent's tool permissions.
	}

	// Map tools. Factory's droid frontmatter rejects unknown tool identifiers,
	// and omitting the tools key allows Factory's default tool set. An unmapped
	// tool therefore omits the whole tools key but still publishes the droid.
	var mappedTools []string
	if s.Tools != nil {
		mappedTools = make([]string, 0, len(s.Tools))
	}
	toolsUnknown := false
	for _, t := range s.Tools {
		mapped, ok := mapping.ToolWithOverride(t, mapping.TargetFactory, opts.ToolOverride())
		if ok {
			mappedTools = append(mappedTools, mapped...)
		} else {
			toolsUnknown = true
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeSubagentFactoryToolUnknown,
				Severity: diag.SeverityWarning,
				Target:   string(mapping.TargetFactory),
				Resource: s.Name(),
				Message:  "Factory cannot map an explicit subagent tool; the tools key was omitted and Factory's default tool set applies",
				Hint:     "Add the tool to the mapping table or remove the explicit tool to keep the restriction, or accept Factory's default tool set for this subagent",
				Source:   compile.DiagSource(s.Source),
			})
		}
	}
	if toolsUnknown {
		slog.Warn("factory subagent tool unrepresentable", "target", mapping.TargetFactory, "resource", s.Name(), "outcome", "tools key omitted")
		mappedTools = nil
	}

	// Factory cannot represent these security restrictions. Fail closed rather
	// than emitting a broader-permission droid artifact.
	if len(s.DisallowedTools) > 0 {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeSubagentFactoryDisallowedToolsDropped,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetFactory),
			Resource: s.Name(),
			Message:  "Factory cannot represent disallowedTools; subagent artifact suppressed",
			Hint:     "Use a target that supports disallowedTools or remove the restriction explicitly",
			Source:   compile.DiagSource(s.Source),
		})
	}
	if s.Isolation != "" || isolation != "" {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeSubagentFactoryIsolationDropped,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetFactory),
			Resource: s.Name(),
			Message:  "Factory cannot represent isolation; subagent artifact suppressed",
			Hint:     "Use a target that supports isolation or remove the restriction explicitly",
			Source:   compile.DiagSource(s.Source),
		})
	}
	if diag.HasErrors(diags) {
		return nil, diags, nil
	}

	// Build frontmatter with Factory renormalizations
	fm := &compile.Frontmatter{}
	fm.Set("name", s.Name())
	fm.Set("description", s.Description)
	if mappedModel != "" {
		fm.Set("model", mappedModel)
	}
	if mappedTools != nil {
		fm.Set("tools", mappedTools)
	}
	if reasoningEffort != "" {
		fm.Set("reasoningEffort", reasoningEffort)
	}
	if isolation != "" {
		fm.Set("isolation", isolation)
	}
	if s.MaxNesting != nil {
		fm.Set("max-nesting", *s.MaxNesting)
	}
	extraKeys, extraDiags := compile.ExtraKeys(s.Extra, compile.PresentFields(map[string]bool{"name": true, "description": true, "model": mappedModel != "", "tools": mappedTools != nil, "effort": false, "reasoningEffort": reasoningEffort != "", "disallowedTools": false, "isolation": isolation != "", "max-nesting": s.MaxNesting != nil, "maxNesting": false}), mapping.TargetFactory, s.Name(), diag.SourceRef(s.Source))
	for _, k := range extraKeys {
		fm.Set(k, s.Extra[k])
	}

	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("factory: render subagent frontmatter: %w", err)
	}
	content := frontmatter + s.Body

	agentArt := artifact.Artifact{
		Path:  fmt.Sprintf("droids/%s.md", s.Name()),
		Bytes: []byte(content),
	}

	// Factory discovers droids by scanning the droids/ directory, not from
	// plugin.json arrays. See translateSkill for rationale.
	return []artifact.Artifact{agentArt}, append(diags, extraDiags...), nil
}

// translateHook translates to hooks/hooks.json fragment with env token swap.
// Non-command type hooks are dropped with a diagnostic.
func (f *factoryCompiler) translateHook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	if h.Type == "prompt" || h.Prompt != "" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookPromptUnsupported, Severity: diag.SeverityError, Target: string(mapping.TargetFactory), Resource: h.Name(), Message: "Factory cannot represent prompt hooks; hook artifact suppressed", Hint: "Use a command hook on Factory", Source: diag.SourceRef(h.Source)}}, nil
	}
	// Drop non-command hook types
	if h.Type != "command" {
		diags := []diag.Diagnostic{
			{
				Code:     diag.CodeHookFactoryNonCommandTypeDropped,
				Severity: diag.SeverityWarning,
				Target:   string(mapping.TargetFactory),
				Resource: h.Name(),
				Message:  fmt.Sprintf("Factory only supports command-type hooks; hook type %q dropped for event %q", h.Type, h.Event),
				Hint:     "Convert the hook to a command type or remove it",
				Source:   compile.DiagSource(h.Source),
			},
		}
		return nil, diags, nil
	}

	// Swap ${CLAUDE_PLUGIN_ROOT} → ${DROID_PLUGIN_ROOT} in the command
	mappedCommand := swapEnvTokens(h.Command)

	// Build the hook entry (type + command + extra fields)
	hookEntry := map[string]any{"type": h.Type, "command": mappedCommand}
	if h.Prompt != "" {
		hookEntry["prompt"] = h.Prompt
	}
	if h.Timeout != nil {
		hookEntry["timeout"] = *h.Timeout
	}
	extraKeys, diags := compile.ExtraKeys(h.Extra, compile.PresentFields(map[string]bool{"event": true, "matcher": h.Matcher != "", "type": true, "command": h.Command != "", "prompt": h.Prompt != "", "timeout": h.Timeout != nil}), mapping.TargetFactory, h.Name(), diag.SourceRef(h.Source))
	for _, k := range extraKeys {
		hookEntry[k] = h.Extra[k]
	}

	// Build the rule group: matcher + hooks array
	ruleGroup := map[string]any{
		"hooks": []any{hookEntry},
	}
	if h.Matcher != "" {
		ruleGroup["matcher"] = h.Matcher
	}

	// Factory hooks.json: top-level keys are event names, each mapping to
	// an array of rule groups (matcher + hooks).
	fragment := map[string]any{
		h.Event: []any{ruleGroup},
	}
	fragBytes, err := json.Marshal(fragment)
	if err != nil {
		return nil, nil, fmt.Errorf("factory: marshal hooks.json: %w", err)
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

// translateMcpServer translates to mcp.json fragment.
func (f *factoryCompiler) translateMcpServer(m *ir.McpServer, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	serverEntry, canonical := compile.McpServerEntry(m, func(s string) string { return s }, true)
	extraKeys, diags := compile.ExtraKeys(m.Extra, canonical, mapping.TargetFactory, m.Name(), diag.SourceRef(m.Source))
	diags = append(compile.McpCredentialDiagnostics(m, mapping.TargetFactory, compile.WithMcpOptions(opts)), diags...)
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
		return nil, nil, fmt.Errorf("factory: marshal mcp.json: %w", err)
	}

	art := artifact.Artifact{
		Path:        "mcp.json",
		Bytes:       fragBytes,
		Fragment:    true,
		FragmentKey: "mcp.json",
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""

	return []artifact.Artifact{art}, diags, nil
}

// swapEnvTokens replaces ${CLAUDE_PLUGIN_ROOT} with ${DROID_PLUGIN_ROOT}
// in the given string, using the mapping.EnvToken function.
func swapEnvTokens(s string) string {
	// Look for known env tokens and replace them.
	// We scan for ${CLAUDE_PLUGIN_ROOT} specifically.
	replacement, literal := mapping.EnvToken("${CLAUDE_PLUGIN_ROOT}", mapping.TargetFactory)
	if literal {
		return s // No substitution for literal replacements
	}
	return strings.ReplaceAll(s, "${CLAUDE_PLUGIN_ROOT}", replacement)
}

// getCompiler returns the registered Factory compiler. Used by tests.
func getCompiler() (compile.Compiler, bool) {
	return compile.Lookup(mapping.TargetFactory)
}
