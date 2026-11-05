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
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
)

const factoryPluginRootSource = "${CLAUDE_PLUGIN_ROOT}"

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
//   - model and effort via the shared catalog resolver
//   - tools via toolpolicy.TranslateToolList (Write expands to Edit,Create per the factory catalog)
func (f *factoryCompiler) translateSubagent(s *ir.Subagent, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var diags []diag.Diagnostic
	isolation := ""
	if sidecar := s.Ext.Factory; sidecar != nil {
		if sidecar.Isolation != "" {
			isolation = sidecar.Isolation
		}
	}

	// Map tools. Factory's droid frontmatter rejects unknown tool identifiers,
	// and omitting the tools key allows Factory's default tool set. An unmapped
	// tool therefore suppresses the droid instead of broadening its permissions.
	var mappedTools []string
	if s.Tools != nil {
		mappedTools = make([]string, 0, len(s.Tools))
	}
	emittedTools, uncoveredTools, toolErr := toolpolicy.TranslateToolList(mapping.Target(s.ToolSource), mapping.TargetFactory, toolpolicy.SurfaceSubagentTools, s.Tools, opts.ToolOverride())
	if toolErr != nil {
		return nil, nil, fmt.Errorf("factory: translate subagent tools: %w", toolErr)
	}
	if s.Tools != nil {
		mappedTools = emittedTools
		if mappedTools == nil {
			mappedTools = []string{}
		}
	}
	toolsUnknown := false
	for range uncoveredTools {
		toolsUnknown = true
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeSubagentFactoryToolUnknown,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetFactory),
			Resource: s.Name(),
			Message:  "Factory cannot map an explicit subagent tool; subagent artifact suppressed",
			Hint:     "Use a supported tool mapping or remove the restriction explicitly",
			Source:   compile.DiagSource(s.Source),
		})
	}
	if toolsUnknown {
		slog.Warn("factory subagent tool unrepresentable", "target", mapping.TargetFactory, "resource", s.Name(), "outcome", "artifact suppressed")
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
	toolsCategory := ""
	if s.Ext.Factory != nil {
		toolsCategory = s.Ext.Factory.ToolsCategory
	}
	if compile.SubagentReadOnly(s) {
		toolsCategory = "read-only"
		if s.Tools != nil {
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
				Severity: diag.SeverityError,
				Target:   string(mapping.TargetFactory),
				Resource: s.Name(),
				Message:  "Factory cannot combine its read-only tools category with an explicit tool list; subagent artifact suppressed",
				Hint:     "Remove the explicit tool list or the read-only restriction",
				Source:   compile.DiagSource(s.Source),
			})
		}
	}
	if diag.HasErrors(diags) {
		return nil, diags, nil
	}

	// Map model
	selection, modelDiags, err := compile.SubagentModel(s, mapping.TargetFactory, opts)
	if err != nil {
		return nil, nil, err
	}
	mappedModel, reasoningEffort := selection.Model, selection.Effort
	// Unresolved models are omitted; unlike explicit unknown tools, a model
	// preference never suppresses the subagent's remaining configuration.
	diags = append(diags, modelDiags...)

	// Build frontmatter with Factory renormalizations
	fm := &compile.Frontmatter{}
	fm.Set("name", s.Name())
	fm.Set("description", s.Description)
	if mappedModel != "" {
		fm.Set("model", mappedModel)
	}
	if mappedTools != nil {
		fm.Set("tools", mappedTools)
	} else if toolsCategory != "" {
		fm.Set("tools", toolsCategory)
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
	extraKeys, extraDiags := compile.ModelExtraKeys(s.Extra, compile.PresentFields(map[string]bool{"name": true, "description": true, "model": mappedModel != "", "tools": mappedTools != nil || toolsCategory != "", "effort": false, "reasoningEffort": reasoningEffort != "", "disallowedTools": false, "isolation": isolation != "", "max-nesting": s.MaxNesting != nil, "maxNesting": false}), mapping.TargetFactory, s.Name(), diag.SourceRef(s.Source))
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

// translateHook translates to hooks/hooks.json fragment.
//
// Event, matcher, timeout, handler type, and command translation come from
// the catalog-driven hookpolicy resolver (OpenSpec native-hook-integration,
// task 3.7). Factory declares only command handlers, so prompt and other
// undeclared types suppress this one entry with an error; a Factory-sourced
// hook keeps its native matcher spelling and group-level fields such as
// commandRegex rather than round-tripping through canonical tool names.
func (f *factoryCompiler) translateHook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	scope := hookpolicy.ScopeRepository
	if opts.Scope() == compile.ScopeUser {
		scope = hookpolicy.ScopeMachine
	}
	resolved := hookpolicy.Resolve(h, target.TargetFactory, scope, opts.PluginRoot())
	if resolved.Failed() {
		return nil, resolved.Diagnostics, nil
	}
	// A stray prompt payload has no Factory representation either; never
	// emit both a command and a prompt for one handler.
	if h.Prompt != "" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookPromptUnsupported, Severity: diag.SeverityError, Target: string(mapping.TargetFactory), Resource: h.Name(), Message: "Factory cannot represent prompt hooks; hook artifact suppressed", Hint: "Use a command hook on Factory", Source: diag.SourceRef(h.Source)}}, nil
	}

	// Build the hook entry (type + command + extra fields)
	hookEntry := map[string]any{"type": resolved.Type}
	if resolved.Command != "" {
		hookEntry["command"] = resolved.Command
	}
	if resolved.Timeout != nil {
		hookEntry["timeout"] = *resolved.Timeout
	}
	extraKeys, diags := compile.HookExtraKeys(h.Extra, compile.PresentFields(map[string]bool{"event": true, "matcher": resolved.Matcher != "", "type": true, "command": resolved.Command != "", "prompt": h.Prompt != "", "timeout": resolved.Timeout != nil}), mapping.TargetFactory, h.Name(), diag.SourceRef(h.Source))

	// Build the rule group: matcher + hooks array. commandRegex is a
	// Factory-only group-level field, not a handler member.
	ruleGroup := map[string]any{
		"hooks": []any{hookEntry},
	}
	if resolved.Matcher != "" {
		ruleGroup["matcher"] = resolved.Matcher
	}
	for _, k := range extraKeys {
		if k == "commandRegex" {
			ruleGroup[k] = h.Extra[k]
			continue
		}
		hookEntry[k] = h.Extra[k]
	}
	diags = append(resolved.Diagnostics, diags...)

	// Factory hooks.json: top-level keys are event names, each mapping to
	// an array of rule groups (matcher + hooks).
	fragment := map[string]any{
		resolved.Event: []any{ruleGroup},
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
	pluginRoot, _ := mapping.EnvToken(factoryPluginRootSource, mapping.TargetFactory)
	serverEntry, canonical := compile.McpServerEntry(m, func(s string) string { return strings.ReplaceAll(s, factoryPluginRootSource, pluginRoot) }, true)
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

// getCompiler returns the registered Factory compiler. Used by tests.
func getCompiler() (compile.Compiler, bool) {
	return compile.Lookup(mapping.TargetFactory)
}
