package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const (
	geminiEnvSourceToken      = "${CLAUDE_PLUGIN_ROOT}"
	geminiExtensionPath       = "gemini-extension.json"
	geminiCommandPathTemplate = "commands/%s.toml"
	geminiSkillPathTemplate   = ".gemini/skills/%s/SKILL.md"
)

type compiler struct{}

type commandDocument struct {
	Name        string   `toml:"name,omitempty"`
	Description string   `toml:"description,omitempty"`
	Prompt      string   `toml:"prompt"`
	Triggers    []string `toml:"triggers,omitempty"`
}

type commandMetadata struct {
	Name        string   `toml:"name,omitempty"`
	Description string   `toml:"description,omitempty"`
	Triggers    []string `toml:"triggers,omitempty"`
}

func init() {
	compile.Register(compiler{})
}

func (compiler) Target() compile.Target {
	return compile.TargetGemini
}

func (compiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return compiler{}.TranslateWithOptions(r, compile.Options{})
}

func (compiler) TranslateWithOptions(r ir.Resource, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	switch resource := r.(type) {
	case *ir.Skill:
		return translateSkill(resource)
	case *ir.Command:
		return translateCommand(resource)
	case *ir.Subagent:
		return translateSubagent(resource, opts)
	case *ir.Hook:
		return translateHook(resource, opts)
	case *ir.McpServer:
		return translateMCPServer(resource, opts)
	case *ir.Rule:
		return translateRule(resource)
	default:
		return nil, nil, fmt.Errorf("gemini: unsupported resource type %T", r)
	}
}

// translateRule drops the rule with a diagnostic: Gemini CLI has no rules
// directory primitive (project guidance lives in a single GEMINI.md file).
func translateRule(rule *ir.Rule) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return nil, []diag.Diagnostic{{
		Code:     diag.CodeRuleGeminiNoPrimitive,
		Severity: diag.SeverityWarning,
		Target:   string(compile.TargetGemini),
		Resource: rule.Name(),
		Message:  "Gemini CLI has no rules directory primitive; rule was dropped",
		Hint:     "Gemini reads project guidance from GEMINI.md; reference the rule content there instead",
		Source:   compile.DiagSource(rule.Source),
	}}, nil
}

func translateSkill(skill *ir.Skill) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fm := &compile.Frontmatter{}
	fm.Set("name", skill.Name())
	fm.Set("description", skill.Description)
	if skill.Triggers != nil {
		fm.Set("triggers", skill.Triggers)
	}
	if skill.ArgumentHint != "" {
		fm.Set("argument-hint", skill.ArgumentHint)
	}
	if skill.Model != "" {
		fm.Set("model", skill.Model)
	}
	allowedTools, toolDiags := compile.PreApprovalSkillTools(skill, mapping.TargetGemini)
	if allowedTools != nil {
		fm.Set("allowed-tools", allowedTools)
	}
	permissions := compile.SkillPermissionsValueWithExtra(skill.Permissions.Allow, skill.Permissions.Ask, skill.Permissions.Deny, skill.Permissions.Extra, skill.PermissionsSet)
	if permissions != nil {
		fm.Set("permissions", permissions)
	}
	if skill.Subagent != "" {
		fm.Set("subagent", skill.Subagent)
	}
	if skill.Agent != "" {
		fm.Set("agent", skill.Agent)
	}
	extraKeys, extraDiags := compile.ExtraKeys(skill.Extra, compile.PresentFields(map[string]bool{"name": true, "description": true, "triggers": skill.Triggers != nil, "argument-hint": skill.ArgumentHint != "", "model": skill.Model != "", "allowed-tools": skill.AllowedTools != nil, "permissions": permissions != nil, "subagent": skill.Subagent != "", "agent": skill.Agent != ""}), mapping.TargetGemini, skill.Name(), diag.SourceRef(skill.Source))
	extraDiags = append(append(toolDiags, compile.SupportFileDiagnostics(skill, mapping.TargetGemini)...), extraDiags...)
	for _, k := range extraKeys {
		fm.Set(k, skill.Extra[k])
	}

	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, err
	}
	content := frontmatter + skill.Body

	art := artifact.Artifact{
		Path:  fmt.Sprintf(geminiSkillPathTemplate, skill.Name()),
		Bytes: []byte(content),
	}
	return []artifact.Artifact{art}, extraDiags, nil
}

func translateCommand(command *ir.Command) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var triggers []string
	triggersPresent := false
	if sidecar := command.Ext.Gemini; sidecar != nil && sidecar.TOMLCommand != nil {
		triggers = append([]string(nil), sidecar.TOMLCommand.Triggers...)
		triggersPresent = sidecar.TOMLCommand.TriggersSet || sidecar.TOMLCommand.Triggers != nil
	}
	present := compile.PresentFields(map[string]bool{
		"name":        true,
		"description": command.Description != "",
		"prompt":      true,
		"triggers":    triggersPresent,
	})
	commandExtra, toolDiags := compile.PreApprovalCommandExtra(command, mapping.TargetGemini)
	extraKeys, diags := compile.ExtraKeys(commandExtra, present, mapping.TargetGemini, command.Name(), diag.SourceRef(command.Source))
	diags = append(toolDiags, diags...)
	extra := make(map[string]any, len(extraKeys))
	for _, key := range extraKeys {
		extra[key] = commandExtra[key]
	}

	out, err := promptArtifact(command.Name(), command.Description, replaceEnvTokens(command.Body), triggers, extra)
	if err != nil {
		return nil, nil, err
	}
	return []artifact.Artifact{out}, diags, nil
}

func translateMCPServer(server *ir.McpServer, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	entry, canonical := compile.McpServerEntry(server, replaceEnvTokens, false)
	extraKeys, diags := compile.ExtraKeys(server.Extra, canonical, compile.TargetGemini, server.Name(), diag.SourceRef(server.Source))
	diags = append(compile.McpCredentialDiagnostics(server, compile.TargetGemini, compile.WithMcpOptions(opts)), diags...)
	for _, key := range extraKeys {
		entry[key] = server.Extra[key]
	}
	if containsBareEnvReference(entry) {
		return nil, append(diags, diag.Diagnostic{
			Code:     diag.CodeMcpGeminiInterpolationUnrepresentable,
			Severity: diag.SeverityError,
			Target:   string(mapping.TargetGemini),
			Resource: server.Name(),
			Message:  "A value contains a bare $NAME that Gemini would expand into an environment lookup; server entry skipped",
			Hint:     "Write the value as ${NAME} if it is an environment reference, or remove the dollar sign from the literal",
			Source:   compile.DiagSource(server.Source),
		}), nil
	}
	body := map[string]any{"mcpServers": map[string]any{server.Name(): entry}}

	bytes, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: marshal mcp server %q: %w", server.Name(), err)
	}

	art := artifact.Artifact{
		Path:        geminiExtensionPath,
		Bytes:       bytes,
		Fragment:    true,
		FragmentKey: geminiExtensionPath,
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""
	return []artifact.Artifact{art}, diags, nil
}

func containsBareEnvReference(value any) bool {
	switch typed := value.(type) {
	case string:
		return mapping.ContainsBareEnvReference(typed)
	case []string:
		for _, item := range typed {
			if mapping.ContainsBareEnvReference(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsBareEnvReference(item) {
				return true
			}
		}
	case map[string]string:
		for _, item := range typed {
			if mapping.ContainsBareEnvReference(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if containsBareEnvReference(item) {
				return true
			}
		}
	}
	return false
}

func promptArtifact(name, description, prompt string, triggers []string, extras map[string]any) (artifact.Artifact, error) {
	metadata := commandMetadata{
		Name:        name,
		Description: description,
		Triggers:    triggers,
	}

	metadataBytes, err := toml.Marshal(metadata)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("gemini: marshal TOML for %q: %w", name, err)
	}
	metadataBytes = bytes.TrimSpace(metadataBytes)
	out := appendPrompt(metadataBytes, prompt)
	if len(extras) > 0 {
		extraBytes, err := toml.Marshal(extras)
		if err != nil {
			return artifact.Artifact{}, fmt.Errorf("gemini: marshal TOML extras for %q: %w", name, err)
		}
		out = append(out, '\n')
		out = append(out, bytes.TrimSpace(extraBytes)...)
	}
	var decoded commandDocument
	if err := toml.Unmarshal(out, &decoded); err != nil {
		return artifact.Artifact{}, fmt.Errorf("gemini: validate TOML for %q: %w", name, err)
	}

	return artifact.Artifact{
		Path:  fmt.Sprintf(geminiCommandPathTemplate, name),
		Bytes: out,
	}, nil
}

func replaceEnvTokens(input string) string {
	replacement, _ := mapping.EnvToken(geminiEnvSourceToken, mapping.TargetGemini)
	return strings.ReplaceAll(input, geminiEnvSourceToken, replacement)
}

func replaceEnvTokenSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	replaced := make([]string, len(values))
	for i, value := range values {
		replaced[i] = replaceEnvTokens(value)
	}
	return replaced
}

func replaceEnvTokenMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	replaced := make(map[string]string, len(values))
	for key, value := range values {
		replaced[key] = replaceEnvTokens(value)
	}
	return replaced
}

func appendPrompt(metadata []byte, prompt string) []byte {
	var builder strings.Builder
	if len(metadata) > 0 {
		builder.Write(metadata)
		builder.WriteString("\n")
	}
	builder.WriteString("prompt = \"\"\"")
	builder.WriteString(escapeMultilineBasicString(prompt))
	builder.WriteString("\"\"\"\n")
	return []byte(builder.String())
}

func escapeMultilineBasicString(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\b", "\\b",
		"\f", "\\f",
		"\r", "\\r",
	)
	escaped := replacer.Replace(value)
	return strings.ReplaceAll(escaped, "\"\"\"", "\\\"\\\"\\\"")
}
