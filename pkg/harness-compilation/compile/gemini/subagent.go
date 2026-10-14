package gemini

import (
	"fmt"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
)

const (
	geminiSubagentPathTemplate = ".gemini/agents/%s.md"
	geminiSubagentKind         = "local"
)

// translateSubagent emits .gemini/agents/<name>.md with YAML frontmatter.
//
// Gemini subagent frontmatter fields:
//   - name: subagent name
//   - description: one-line description
//   - kind: always "local" for compiled subagents
//   - model: resolved through the shared model catalog
//   - tools: translated via toolpolicy.TranslateToolList for the Gemini catalog
//
// Effort is omitted with a warning; agent Markdown has no field. Temperature,
// max_turns, timeout_mins, and inline mcpServers are Gemini-specific fields
// populated from Ext.Gemini.
func translateSubagent(sub *ir.Subagent, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var diags []diag.Diagnostic
	if len(sub.DisallowedTools) > 0 {
		diags = append(diags, diag.Diagnostic{Code: diag.CodeSubagentSecurityRestrictionUnsupported, Severity: diag.SeverityError, Target: string(mapping.TargetGemini), Resource: sub.Name(), Message: "Gemini cannot represent disallowedTools; subagent artifact suppressed", Hint: "Use a target that supports disallowedTools or remove the restriction explicitly", Source: diag.SourceRef(sub.Source)})
	}
	if sub.Isolation != "" {
		diags = append(diags, diag.Diagnostic{Code: diag.CodeSubagentSecurityRestrictionUnsupported, Severity: diag.SeverityError, Target: string(mapping.TargetGemini), Resource: sub.Name(), Message: "Gemini cannot represent isolation; subagent artifact suppressed", Hint: "Use a target that supports isolation or remove the restriction explicitly", Source: diag.SourceRef(sub.Source)})
	}
	if len(diags) > 0 {
		return nil, diags, nil
	}
	kind := geminiSubagentKind
	var temperature *float64
	var maxTurns *int
	var timeoutMinutes *int
	var inlineMCPServers map[string]extMCPServer

	if sidecar := sub.Ext.Gemini; sidecar != nil {
		if sidecar.KindSet || sidecar.Kind != "" {
			kind = sidecar.Kind
		}
		temperature = sidecar.Temperature
		maxTurns = sidecar.MaxTurns
		timeoutMinutes = sidecar.TimeoutMinutes
		if sidecar.InlineMCPServers != nil {
			inlineMCPServers = make(map[string]extMCPServer, len(sidecar.InlineMCPServers))
			names := make([]string, 0, len(sidecar.InlineMCPServers))
			for name := range sidecar.InlineMCPServers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				server := sidecar.InlineMCPServers[name]
				inlineMCPServers[name] = extMCPServer{
					Command: replaceEnvTokens(server.Command),
					Args:    replaceEnvTokenSlice(server.Args),
					Env:     replaceEnvTokenMap(server.Env),
					URL:     replaceEnvTokens(server.URL),
				}
				diags = append(diags, compile.McpCredentialDiagnosticsForFields(
					sub.Name(), diag.SourceRef(sub.Source), mapping.TargetGemini,
					server.Env, nil, "", "mcpServers."+name+".", compile.WithMcpOptions(opts),
				)...)
			}
		}
	}

	// Resolve model and reasoning through the shared catalog.
	selection, modelDiags, err := compile.SubagentModel(sub, mapping.TargetGemini, opts)
	if err != nil {
		return nil, nil, err
	}
	mappedModel := selection.Model
	// Unknown cross-target models are omitted without suppressing the agent.
	// Gemini agent Markdown has no standalone effort representation.
	diags = append(diags, modelDiags...)

	// Map tools via the shipped tool catalogs (design D5).
	var mappedTools []string
	emittedTools, uncoveredTools, toolErr := toolpolicy.TranslateToolList(mapping.Target(sub.ToolSource), mapping.TargetGemini, toolpolicy.SurfaceSubagentTools, sub.Tools, opts.ToolOverride())
	if toolErr != nil {
		return nil, nil, fmt.Errorf("gemini: translate subagent tools: %w", toolErr)
	}
	if sub.Tools != nil {
		mappedTools = emittedTools
		if mappedTools == nil {
			mappedTools = []string{}
		}
	}
	for range uncoveredTools {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeSubagentGeminiToolUnknown,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetGemini),
			Resource: sub.Name(),
			Message:  "Gemini cannot map an explicit subagent tool; artifact suppressed",
			Hint:     "Add the tool to the mapping table or remove the explicit tool",
			Source:   compile.DiagSource(sub.Source),
		})
	}
	if diag.HasErrors(diags) {
		return nil, diags, nil
	}

	// Build YAML frontmatter.
	fm := &compile.Frontmatter{}
	fm.Set("name", sub.Name())
	fm.Set("description", sub.Description)
	fm.Set("kind", kind)
	if mappedModel != "" {
		fm.Set("model", mappedModel)
	}
	if mappedTools != nil {
		fm.Set("tools", mappedTools)
	}
	if temperature != nil {
		fm.Set("temperature", *temperature)
	}
	if maxTurns != nil {
		fm.Set("max_turns", *maxTurns)
	}
	if timeoutMinutes != nil {
		fm.Set("timeout_mins", *timeoutMinutes)
	}
	if sub.MaxNesting != nil {
		fm.Set("max-nesting", *sub.MaxNesting)
	}

	extraKeys, extraDiags := compile.ModelExtraKeys(sub.Extra, compile.PresentFields(map[string]bool{
		"name":            true,
		"description":     true,
		"kind":            true,
		"model":           mappedModel != "",
		"tools":           mappedTools != nil,
		"effort":          sub.Effort != "",
		"disallowedTools": false,
		"isolation":       false,
		"max-nesting":     sub.MaxNesting != nil,
		"maxNesting":      false,
		"temperature":     temperature != nil,
		"max_turns":       maxTurns != nil,
		"timeout_mins":    timeoutMinutes != nil,
		"mcpServers":      true,
	}), mapping.TargetGemini, sub.Name(), diag.SourceRef(sub.Source))
	for _, key := range extraKeys {
		fm.Set(key, sub.Extra[key])
	}
	diags = append(diags, extraDiags...)

	if len(inlineMCPServers) > 0 {
		entries := make(map[string]any, len(inlineMCPServers))
		names := make([]string, 0, len(inlineMCPServers))
		for name := range inlineMCPServers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			server := inlineMCPServers[name]
			entry := make(map[string]any)
			if server.Command != "" {
				entry["command"] = server.Command
			}
			if len(server.Args) > 0 {
				entry["args"] = server.Args
			}
			if len(server.Env) > 0 {
				entry["env"] = server.Env
			}
			if server.URL != "" {
				entry["url"] = server.URL
			}
			entries[name] = entry
		}
		fm.Set("mcpServers", entries)
	}

	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: render subagent frontmatter: %w", err)
	}
	content := frontmatter + sub.Body

	art := artifact.Artifact{
		Path:  fmt.Sprintf(geminiSubagentPathTemplate, sub.Name()),
		Bytes: []byte(content),
	}

	return []artifact.Artifact{art}, diags, nil
}

type extMCPServer struct {
	Command string
	Args    []string
	Env     map[string]string
	URL     string
}
