package windsurf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	compilepkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mdc"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const claudePluginRootToken = "${CLAUDE_PLUGIN_ROOT}"

var (
	envInterpolationPattern  = regexp.MustCompile(`^\$\{([A-Z0-9_]+)\}$`)
	embeddedEnvPattern       = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)
	shellEnvPattern          = regexp.MustCompile(`^\$([A-Z0-9_]+)$`)
	fileInterpolationPattern = regexp.MustCompile(`^\$\{file:(.+)\}$`)
)

// Compiler translates IR resources into Windsurf artifacts.
type Compiler struct{}

func init() {
	compilepkg.Register(Compiler{})
}

// Target returns the Windsurf compilation target.
func (Compiler) Target() compilepkg.Target { return compilepkg.TargetWindsurf }

// Translate compiles a resource with default options.
func (c Compiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return c.TranslateWithOptions(r, compilepkg.Options{})
}

// TranslateWithOptions compiles a resource into Windsurf artifacts and diagnostics.
func (c Compiler) TranslateWithOptions(r ir.Resource, opts compilepkg.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return compilepkg.GuardNativeCodexTranslation(r, c.Target(), func(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
		return c.translateWithOptions(r, opts)
	})
}

func (Compiler) translateWithOptions(r ir.Resource, opts compilepkg.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	switch v := r.(type) {
	case *ir.Skill:
		return translateSkill(v)
	case *ir.Command:
		return translateCommand(v)
	case *ir.Subagent:
		artifacts, diags := translateSubagent(v)
		return artifacts, diags, nil
	case *ir.Hook:
		return translateHook(v, opts)
	case *ir.McpServer:
		return translateMcpServer(v, opts)
	case *ir.Rule:
		return translateRule(v)
	default:
		return nil, nil, fmt.Errorf("windsurf: unsupported resource type %T", r)
	}
}

// translateRule emits .windsurf/rules/<name>.mdc with YAML frontmatter
// (description, globs, alwaysApply) followed by the Markdown body. Windsurf
// rules use the same .mdc format as Cursor.
func translateRule(rule *ir.Rule) ([]artifact.Artifact, []diag.Diagnostic, error) {
	body, diags, err := mdc.RenderWithDiagnostics(rule, compilepkg.TargetWindsurf)
	if err != nil {
		return nil, nil, fmt.Errorf("windsurf: render rule frontmatter: %w", err)
	}
	return []artifact.Artifact{{Path: ".windsurf/rules/" + rule.Name() + ".mdc", Bytes: []byte(body)}}, diags, nil
}

func translateSkill(skill *ir.Skill) ([]artifact.Artifact, []diag.Diagnostic, error) {
	// Windsurf supports the agentskills.io format (.windsurf/skills/<name>/SKILL.md).
	// Render YAML frontmatter with name, description, triggers, and extra fields.
	fm := &compilepkg.Frontmatter{}
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
	if skill.AllowedTools != nil {
		fm.Set("allowed-tools", skill.AllowedTools)
	}
	permissions := compilepkg.SkillPermissionsValueWithExtra(skill.Permissions.Allow, skill.Permissions.Ask, skill.Permissions.Deny, skill.Permissions.Extra, skill.PermissionsSet)
	if permissions != nil {
		fm.Set("permissions", permissions)
	}
	if skill.Subagent != "" {
		fm.Set("subagent", skill.Subagent)
	}
	if skill.Agent != "" {
		fm.Set("agent", skill.Agent)
	}
	present := make(map[string]bool, len(fm.Keys()))
	for _, k := range fm.Keys() {
		present[k] = true
	}
	extraKeys, extraDiags := compilepkg.ExtraKeys(skill.Extra, compilepkg.PresentFields(present), compilepkg.TargetWindsurf, skill.Name(), diag.SourceRef(skill.Source))
	extraDiags = append(compilepkg.SupportFileDiagnostics(skill, compilepkg.TargetWindsurf), extraDiags...)
	for _, k := range extraKeys {
		fm.Set(k, skill.Extra[k])
	}

	frontmatter, err := fm.Render()
	if err != nil {
		return nil, nil, fmt.Errorf("windsurf: render skill frontmatter: %w", err)
	}
	body := frontmatter + skill.Body
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}

	return []artifact.Artifact{{
		Path:  ".windsurf/skills/" + skill.Name() + "/SKILL.md",
		Bytes: []byte(body),
	}}, extraDiags, nil
}

func translateCommand(command *ir.Command) ([]artifact.Artifact, []diag.Diagnostic, error) {
	// Windsurf workflows are plain markdown with optional frontmatter; the
	// slash-command name derives from the filename, so no name/resource keys
	// are emitted. Auto-execution uses Windsurf's snake_case key.
	fm := &compilepkg.Frontmatter{}
	if command.Description != "" {
		fm.Set("description", command.Description)
	}
	if sidecar := command.Ext.Windsurf; sidecar != nil && sidecar.AutoExecutionMode != "" {
		fm.Set("auto_execution_mode", sidecar.AutoExecutionMode)
	}
	present := make(map[string]bool, len(fm.Keys()))
	for _, k := range fm.Keys() {
		present[k] = true
	}
	extraKeys, diags := compilepkg.ExtraKeys(command.Extra, compilepkg.PresentFields(present), compilepkg.TargetWindsurf, command.Name(), diag.SourceRef(command.Source))
	for _, key := range extraKeys {
		fm.Set(key, command.Extra[key])
	}

	var buf bytes.Buffer
	if len(fm.Keys()) > 0 {
		frontmatter, err := fm.Render()
		if err != nil {
			return nil, nil, fmt.Errorf("windsurf: render command frontmatter: %w", err)
		}
		buf.WriteString(frontmatter)
	}
	buf.WriteString(command.Body)
	body := buf.Bytes()
	if len(body) > 0 && !bytes.HasSuffix(body, []byte{'\n'}) {
		body = append(body, '\n')
	}

	return []artifact.Artifact{{
		Path:  ".windsurf/workflows/" + command.Name() + ".md",
		Bytes: body,
	}}, diags, nil
}

func translateSubagent(subagent *ir.Subagent) ([]artifact.Artifact, []diag.Diagnostic) {
	if len(subagent.DisallowedTools) > 0 || subagent.Isolation != "" {
		return nil, []diag.Diagnostic{{
			Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
			Severity: diag.SeverityError,
			Target:   string(compilepkg.TargetWindsurf),
			Resource: subagent.Name(),
			Message:  "Windsurf cannot represent subagent security restrictions; artifact suppressed",
			Hint:     "Use a target that represents the restrictions or remove them explicitly",
			Source:   compilepkg.DiagSource(subagent.Source),
		}}
	}
	return nil, []diag.Diagnostic{{
		Code:     diag.CodeSubagentWindsurfNoPrimitive,
		Severity: diag.SeverityWarning,
		Target:   string(compilepkg.TargetWindsurf),
		Resource: subagent.Name(),
		Message:  "Windsurf has no subagent primitive; resource was dropped",
		Hint:     "Consider expressing this behavior as a Windsurf workflow instead",
		Source:   compilepkg.DiagSource(subagent.Source),
	}}
}

func translateHook(hook *ir.Hook, opts compilepkg.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	command := hook.Command
	var diags []diag.Diagnostic

	if hook.Type == "prompt" || hook.Prompt != "" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookPromptUnsupported, Severity: diag.SeverityWarning, Target: string(compilepkg.TargetWindsurf), Resource: hook.Name(), Message: "Windsurf cannot represent prompt hooks; hook artifact suppressed", Hint: "Use a command hook on Windsurf", Source: compilepkg.DiagSource(hook.Source)}}, nil
	}
	if hook.Type != "command" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookWindsurfUnsupportedType, Severity: diag.SeverityWarning, Target: string(compilepkg.TargetWindsurf), Resource: hook.Name(), Message: "Windsurf cannot represent this hook type; hook artifact suppressed", Hint: "Use a command hook on Windsurf", Source: compilepkg.DiagSource(hook.Source)}}, nil
	}

	// Map Claude event to Windsurf event using the mapping table.
	windsurfEvent := ""
	if sidecar := hook.Ext.Windsurf; sidecar != nil && sidecar.OriginalHookEvent != "" {
		windsurfEvent = sidecar.OriginalHookEvent
	} else {
		mappedEvent, ok := mapping.HookEvent(hook.Event, hook.Matcher, compilepkg.TargetWindsurf)
		if !ok {
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeHookWindsurfEventUnmappable,
				Severity: diag.SeverityWarning,
				Target:   string(compilepkg.TargetWindsurf),
				Resource: hook.Name(),
				Message:  fmt.Sprintf("Claude hook event %q has no Windsurf equivalent; hook dropped", hook.Event),
				Hint:     "Consider using a mappable event (PreToolUse with a known tool matcher, UserPromptSubmit, Stop, or SubagentStop)",
				Source:   compilepkg.DiagSource(hook.Source),
			})
			return nil, diags, nil
		}
		windsurfEvent = mappedEvent
	}

	if strings.Contains(command, claudePluginRootToken) {
		if pluginRoot := opts.PluginRoot(); pluginRoot != "" {
			command = strings.ReplaceAll(command, claudePluginRootToken, pluginRoot)
		} else {
			_, literal := mapping.EnvToken(claudePluginRootToken, compilepkg.TargetWindsurf)
			hint := "Provide compile.WithPluginRoot(...) to materialize ${CLAUDE_PLUGIN_ROOT}; leaving the raw token in place may reduce portability across Windsurf workspaces."
			if !literal {
				hint = "Windsurf requires a literal plugin path for ${CLAUDE_PLUGIN_ROOT}; provide compile.WithPluginRoot(...)."
			}
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeHookWindsurfEnvTokenExpanded,
				Severity: diag.SeverityWarning,
				Target:   string(compilepkg.TargetWindsurf),
				Resource: hook.Name(),
				Message:  "Windsurf could not materialize ${CLAUDE_PLUGIN_ROOT} without a compile-time plugin root",
				Hint:     hint,
				Source:   compilepkg.DiagSource(hook.Source),
			})
		}
	}

	body := map[string]any{"command": command, "event": windsurfEvent, "type": hook.Type}
	if hook.Matcher != "" {
		body["matcher"] = hook.Matcher
	}
	if hook.Timeout != nil {
		body["timeout"] = *hook.Timeout
	}
	extraKeys, extraDiags := compilepkg.ExtraKeys(hook.Extra, compilepkg.PresentFields(map[string]bool{"event": true, "matcher": hook.Matcher != "", "type": true, "command": true, "timeout": hook.Timeout != nil}), compilepkg.TargetWindsurf, hook.Name(), compilepkg.DiagSource(hook.Source))
	for _, key := range extraKeys {
		body[key] = hook.Extra[key]
	}
	diags = append(diags, extraDiags...)
	if sidecar := hook.Ext.Windsurf; sidecar != nil {
		if sidecar.HookPowerShell != "" {
			body["powershell"] = sidecar.HookPowerShell
		}
		if sidecar.HookShowOutput != nil {
			body["show_output"] = *sidecar.HookShowOutput
		}
		if sidecar.HookWorkingDir != "" {
			body["working_dir"] = sidecar.HookWorkingDir
		}
	}

	content, err := marshalIndentedJSON(body)
	if err != nil {
		return nil, nil, err
	}

	return []artifact.Artifact{{
		Path:  ".windsurf/hooks/" + windsurfEvent + ".json",
		Bytes: content,
	}}, diags, nil
}

func translateMcpServer(server *ir.McpServer, opts compilepkg.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	serverEntry := map[string]any{}
	if server.Command != "" {
		serverEntry["command"] = translateMcpEnvValue(server.Command)
	}
	if server.Args != nil {
		args := make([]string, len(server.Args))
		for i, arg := range server.Args {
			args[i] = translateMcpEnvValue(arg)
		}
		serverEntry["args"] = args
	}
	if server.Env != nil {
		env := make(map[string]string, len(server.Env))
		for key, value := range server.Env {
			env[key] = translateMcpEnvValue(value)
		}
		serverEntry["env"] = env
	}
	if server.URL != "" {
		serverEntry["url"] = translateMcpEnvValue(server.URL)
	}
	if server.Transport != "" {
		serverEntry["transport"] = server.Transport
	}
	if server.Headers != nil {
		headers := make(map[string]string, len(server.Headers))
		for key, value := range server.Headers {
			headers[key] = translateMcpEnvValue(value)
		}
		serverEntry["headers"] = headers
	}
	if server.DisabledSet || server.Disabled {
		serverEntry["disabled"] = server.Disabled
	}
	if server.OAuthClientID != "" {
		serverEntry["oauthClientId"] = server.OAuthClientID
	}
	if server.OAuthClientSecret != "" {
		serverEntry["oauthClientSecret"] = server.OAuthClientSecret
	}
	if server.OAuthResource != "" {
		serverEntry["oauthResource"] = server.OAuthResource
	}
	// Pass through Extra fields (type, url, headers, disabled, etc.)
	// so that HTTP/SSE MCP servers survive cross-compilation.
	// String values within maps (e.g. header values) undergo the same
	// interpolation translation as env values.
	canonicalMcpKeys := []string{"command", "args", "env", "url", "transport", "headers", "disabled", "oauthClientId", "oauthClientSecret", "oauthResource"}
	present := make(map[string]bool, len(canonicalMcpKeys))
	for _, k := range canonicalMcpKeys {
		if _, ok := serverEntry[k]; ok {
			present[k] = true
		}
	}
	extraKeys, diags := compilepkg.ExtraKeys(server.Extra, compilepkg.PresentFields(present), compilepkg.TargetWindsurf, server.Name(), compilepkg.DiagSource(server.Source))
	diags = append(compilepkg.McpCredentialDiagnostics(server, compilepkg.TargetWindsurf, compilepkg.WithMcpOptions(opts)), diags...)
	for _, k := range extraKeys {
		serverEntry[k] = translateMcpExtraValue(server.Extra[k])
	}

	content, err := marshalIndentedJSON(map[string]any{
		"mcpServers": map[string]any{
			server.Name(): serverEntry,
		},
	})
	if err != nil {
		return nil, nil, err
	}

	art := artifact.Artifact{
		Path:        ".windsurf/mcp_config.json",
		Bytes:       content,
		Fragment:    true,
		FragmentKey: "windsurf-mcp",
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""
	return []artifact.Artifact{art}, diags, nil
}

func translateMcpEnvValue(value string) string {
	switch {
	case strings.HasPrefix(value, "${env:") && strings.HasSuffix(value, "}"):
		return replaceInterpolation(mapping.McpInterpolation("${env:VAR}", compilepkg.TargetWindsurf), "VAR", strings.TrimSuffix(strings.TrimPrefix(value, "${env:"), "}"))
	case strings.HasPrefix(value, "${file:") && strings.HasSuffix(value, "}"):
		return replaceInterpolation(mapping.McpInterpolation("${file:PATH}", compilepkg.TargetWindsurf), "PATH", strings.TrimSuffix(strings.TrimPrefix(value, "${file:"), "}"))
	case envInterpolationPattern.MatchString(value):
		match := envInterpolationPattern.FindStringSubmatch(value)
		return replaceInterpolation(mapping.McpInterpolation("inline", compilepkg.TargetWindsurf), "VAR", match[1])
	case shellEnvPattern.MatchString(value):
		match := shellEnvPattern.FindStringSubmatch(value)
		return replaceInterpolation(mapping.McpInterpolation("inline", compilepkg.TargetWindsurf), "VAR", match[1])
	case fileInterpolationPattern.MatchString(value):
		match := fileInterpolationPattern.FindStringSubmatch(value)
		return replaceInterpolation(mapping.McpInterpolation("${file:PATH}", compilepkg.TargetWindsurf), "PATH", match[1])
	default:
		// For values that contain embedded ${VAR} patterns (e.g.
		// "Bearer ${API_KEY}" in a header), replace each occurrence with
		// Windsurf's ${env:VAR} syntax.
		if !embeddedEnvPattern.MatchString(value) {
			return value
		}
		return embeddedEnvPattern.ReplaceAllStringFunc(value, func(match string) string {
			m := embeddedEnvPattern.FindStringSubmatch(match)
			if len(m) < 2 {
				return match
			}
			return strings.Replace(mapping.McpInterpolation("inline", compilepkg.TargetWindsurf), "VAR", m[1], 1)
		})
	}
}

// translateMcpExtraValue recursively translates string values within Extra
// fields (e.g. header values) using the same interpolation rules as env values.
func translateMcpExtraValue(v any) any {
	switch val := v.(type) {
	case string:
		return translateMcpEnvValue(val)
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = translateMcpExtraValue(v)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, elem := range val {
			result[i] = translateMcpExtraValue(elem)
		}
		return result
	default:
		return v
	}
}

func replaceInterpolation(template, placeholder, value string) string {
	return strings.Replace(template, placeholder, value, 1)
}

func marshalIndentedJSON(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}
