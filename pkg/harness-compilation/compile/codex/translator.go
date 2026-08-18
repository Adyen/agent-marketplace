package codex

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type Compiler struct{}

func init() { compile.Register(Compiler{}) }

func (Compiler) Target() compile.Target { return mapping.TargetCodex }

func (Compiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	kind, name, err := ir.ValidateResource(r)
	if err != nil {
		return nil, nil, err
	}
	slog.Debug("codex compile start", "kind", kind, "resource", name)
	switch resource := r.(type) {
	case *ir.Skill:
		return skill(resource, false)
	case *ir.Command:
		return command(resource)
	case *ir.Subagent:
		return subagent(resource)
	case *ir.Hook:
		return hook(resource)
	case *ir.McpServer:
		return mcp(resource)
	case *ir.Rule:
		return nil, []diag.Diagnostic{diagnostic(diag.CodeRuleCodexUnsupported, diag.SeverityWarning, resource.Name(), resource.Source, "Codex rule omitted; no rule or AGENTS.md content was emitted")}, nil
	default:
		return nil, nil, fmt.Errorf("codex: unsupported resource kind %s", kind)
	}
}

func skill(s *ir.Skill, command bool) ([]artifact.Artifact, []diag.Diagnostic, error) {
	if s.AllowedTools != nil || s.PermissionsSet {
		slog.Warn("codex skill suppressed", "resource", s.Name(), "reason", "security_restriction")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeSkillCodexSecurityRestrictionUnsupported, diag.SeverityError, s.Name(), s.Source, "Codex cannot enforce the skill's explicit tool or permission restriction; artifact suppressed")}, nil
	}
	var frontmatter compile.Frontmatter
	frontmatter.Set("name", s.Name())
	frontmatter.Set("description", s.Description)
	if command {
		frontmatter.Set("x-agent-marketplace-kind", "command")
	}
	reserved := compile.PresentFields(map[string]bool{"name": true, "description": true, "x-agent-marketplace-kind": command})
	extraKeys, diagnostics := compile.ExtraKeys(s.Extra, reserved, mapping.TargetCodex, s.Name(), diag.SourceRef(s.Source))
	for _, key := range extraKeys {
		frontmatter.Set(key, s.Extra[key])
	}
	header, err := frontmatter.Render()
	if err != nil {
		return nil, nil, err
	}
	diagnostics = append(diagnostics, compile.SupportFileDiagnostics(s, mapping.TargetCodex)...)
	if s.ArgumentHint != "" || s.Model != "" || s.Triggers != nil || s.Subagent != "" || s.Agent != "" {
		diagnostics = append(diagnostics, diagnostic(diag.CodeCodexFieldDropped, diag.SeverityWarning, s.Name(), s.Source, "Codex skill output omitted benign activation or execution metadata without a documented equivalent"))
	}
	return []artifact.Artifact{{Path: ".agents/skills/" + s.Name() + "/SKILL.md", Bytes: []byte(header + s.Body)}}, diagnostics, nil
}

func command(c *ir.Command) ([]artifact.Artifact, []diag.Diagnostic, error) {
	s := ir.NewSkill(c.Name(), c.Description, c.Body, nil, c.Extra, c.Source)
	return skill(s, true)
}

func subagent(s *ir.Subagent) ([]artifact.Artifact, []diag.Diagnostic, error) {
	if s.Tools != nil || len(s.DisallowedTools) > 0 || s.Isolation != "" || s.MaxNesting != nil {
		slog.Warn("codex subagent suppressed", "resource", s.Name(), "reason", "security_restriction")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError, s.Name(), s.Source, "Codex subagent output cannot preserve an explicit tool, isolation, or nesting restriction; artifact suppressed")}, nil
	}
	var b strings.Builder
	writeTOMLString(&b, "name", s.Name())
	writeTOMLString(&b, "description", s.Description)
	writeTOMLString(&b, "model", s.Model)
	writeTOMLString(&b, "model_reasoning_effort", s.Effort)
	writeTOMLString(&b, "developer_instructions", s.Body)
	var diagnostics []diag.Diagnostic
	if len(s.Extra) > 0 {
		diagnostics = append(diagnostics, diagnostic(diag.CodeCodexFieldDropped, diag.SeverityWarning, s.Name(), s.Source, "Codex subagent output omitted unmodeled extension fields"))
	}
	return []artifact.Artifact{{Path: ".codex/agents/" + s.Name() + ".toml", Bytes: []byte(b.String())}}, diagnostics, nil
}

func hook(h *ir.Hook) ([]artifact.Artifact, []diag.Diagnostic, error) {
	if h.Type != "command" || !codexHookEvents[h.Event] {
		slog.Warn("codex hook suppressed", "resource", h.Name(), "event", h.Event, "handler_type", h.Type)
		return nil, []diag.Diagnostic{diagnostic(diag.CodeHookCodexUnsupported, diag.SeverityError, h.Name(), h.Source, "Codex does not execute this hook event or handler; hook suppressed")}, nil
	}
	handler := map[string]any{"type": "command", "command": h.Command}
	entry := map[string]any{"hooks": []any{handler}}
	if h.Matcher != "" {
		entry["matcher"] = h.Matcher
	}
	raw, err := json.Marshal(map[string]any{"hooks": map[string]any{h.Event: []any{entry}}})
	if err != nil {
		return nil, nil, err
	}
	var diagnostics []diag.Diagnostic
	if h.Timeout != nil || h.Prompt != "" || len(h.Extra) > 0 {
		diagnostics = append(diagnostics, diagnostic(diag.CodeCodexFieldDropped, diag.SeverityWarning, h.Name(), h.Source, "Codex hook output omitted fields without a documented command-handler equivalent"))
	}
	return []artifact.Artifact{{Path: ".codex/hooks.json", Bytes: raw, Fragment: true, FragmentKey: "hooks", FragmentOrderKey: h.Name(), MergeWithExisting: true, ExternalReconciliationRequired: true, DedupeFragmentEntries: true}}, diagnostics, nil
}

func mcp(m *ir.McpServer) ([]artifact.Artifact, []diag.Diagnostic, error) {
	stdio, remote := m.Command != "", m.URL != ""
	unsupportedTransport := m.Transport != "" && m.Transport != "stdio" && m.Transport != "streamable-http"
	if stdio == remote || unsupportedTransport || m.OAuthClientID != "" || m.OAuthClientSecret != "" || m.OAuthResource != "" {
		slog.Warn("codex MCP server suppressed", "resource", m.Name(), "reason", "security_or_transport_loss")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeMcpCodexSecurityUnsupported, diag.SeverityError, m.Name(), m.Source, "Codex MCP output cannot preserve the requested authentication or transport semantics; server suppressed")}, nil
	}
	entry := mcpOutput{Command: m.Command, Args: m.Args, Env: m.Env, URL: m.URL, HTTPHeaders: m.Headers}
	if m.DisabledSet {
		enabled := !m.Disabled
		entry.Enabled = &enabled
	}
	raw, err := toml.Marshal(mcpConfig{Servers: map[string]mcpOutput{m.Name(): entry}})
	if err != nil {
		return nil, nil, fmt.Errorf("codex: marshal MCP server %q: %w", m.Name(), err)
	}
	diagnostics := compile.McpCredentialDiagnostics(m, mapping.TargetCodex)
	if len(m.Extra) > 0 {
		diagnostics = append(diagnostics, diagnostic(diag.CodeCodexFieldDropped, diag.SeverityWarning, m.Name(), m.Source, "Codex MCP output omitted unmodeled extension fields"))
	}
	return []artifact.Artifact{{Path: ".codex/config.toml", Bytes: raw, Fragment: true, FragmentKey: "mcp_servers", FragmentOrderKey: m.Name(), MergeWithExisting: true, ExternalReconciliationRequired: true}}, diagnostics, nil
}

type mcpConfig struct {
	Servers map[string]mcpOutput `toml:"mcp_servers"`
}
type mcpOutput struct {
	Command     string            `toml:"command,omitempty"`
	Args        []string          `toml:"args,omitempty"`
	Env         map[string]string `toml:"env,omitempty"`
	URL         string            `toml:"url,omitempty"`
	HTTPHeaders map[string]string `toml:"http_headers,omitempty"`
	Enabled     *bool             `toml:"enabled,omitempty"`
}

func writeTOMLString(b *strings.Builder, key, value string) {
	if value != "" {
		raw, _ := toml.Marshal(map[string]string{key: value})
		b.Write(raw)
	}
}

var codexHookEvents = map[string]bool{"SessionStart": true, "SessionEnd": true, "UserPromptSubmit": true, "PreToolUse": true, "PostToolUse": true, "Stop": true, "SubagentStop": true, "PreCompact": true, "Notification": true}

func diagnostic(code string, severity diag.Severity, resource string, source ir.SourceRef, message string) diag.Diagnostic {
	return diag.Diagnostic{Code: code, Severity: severity, Target: string(mapping.TargetCodex), Resource: resource, Message: message, Source: diag.SourceRef(source)}
}
