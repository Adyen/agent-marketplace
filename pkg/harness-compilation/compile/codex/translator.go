package codex

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type Compiler struct{}

const claudePluginRootToken = "${CLAUDE_PLUGIN_ROOT}"

func init() { compile.Register(Compiler{}) }

func (Compiler) Target() compile.Target { return mapping.TargetCodex }

func (c Compiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return c.TranslateWithOptions(r, compile.Options{})
}

func (Compiler) TranslateWithOptions(r ir.Resource, opts compile.Options) (artifacts []artifact.Artifact, diagnostics []diag.Diagnostic, err error) {
	kind, name, validationErr := ir.ValidateResource(r)
	if validationErr != nil {
		slog.Error("codex compile failed", "reason", "invalid_resource")
		return nil, nil, validationErr
	}
	slog.Debug("codex compile start", "kind", kind, "resource", name)
	defer func() {
		logCodexCompileDiagnostics(diagnostics)
		if err != nil {
			slog.Error("codex compile failed", "kind", kind, "resource", name, "reason", "translation_error")
			return
		}
		slog.Debug("codex compile complete", "kind", kind, "resource", name, "artifacts", len(artifacts), "diagnostics", len(diagnostics))
	}()
	switch resource := r.(type) {
	case *ir.Skill:
		return skill(resource, false)
	case *ir.Command:
		return command(resource)
	case *ir.Subagent:
		return subagent(resource, opts)
	case *ir.Hook:
		return hook(resource, opts)
	case *ir.McpServer:
		return mcp(resource, opts)
	case *ir.Rule:
		return nil, []diag.Diagnostic{diagnostic(diag.CodeRuleCodexUnsupported, diag.SeverityWarning, resource.Name(), resource.Source, "Codex rule omitted; no rule or AGENTS.md content was emitted")}, nil
	default:
		return nil, nil, fmt.Errorf("codex: unsupported resource kind %s", kind)
	}
}

func logCodexCompileDiagnostics(diagnostics []diag.Diagnostic) {
	for _, diagnostic := range diagnostics {
		attrs := []any{"code", diagnostic.Code, "severity", diagnostic.Severity.String(), "resource", diagnostic.Resource}
		switch diagnostic.Severity {
		case diag.SeverityError:
			slog.Error("codex compile diagnostic", attrs...)
		case diag.SeverityWarning:
			slog.Warn("codex compile diagnostic", attrs...)
		default:
			slog.Debug("codex compile diagnostic", attrs...)
		}
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
	path, err := codexResourcePath(ir.KindSkill, s.Name())
	if command {
		path, err = codexResourcePath(ir.KindCommand, s.Name())
	}
	if err != nil {
		return nil, nil, err
	}
	return []artifact.Artifact{{Path: path, Bytes: []byte(header + s.Body)}}, diagnostics, nil
}

func command(c *ir.Command) ([]artifact.Artifact, []diag.Diagnostic, error) {
	for key := range c.Extra {
		if strings.EqualFold(key, "allowed-tools") || strings.EqualFold(key, "permissions") {
			return nil, []diag.Diagnostic{diagnostic(diag.CodeSkillCodexSecurityRestrictionUnsupported, diag.SeverityError, c.Name(), c.Source, "Codex cannot enforce the command's explicit tool or permission restriction; artifact suppressed")}, nil
		}
	}
	s := ir.NewSkill(c.Name(), c.Description, c.Body, nil, c.Extra, c.Source)
	return skill(s, true)
}

func subagent(s *ir.Subagent, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	if s.Tools != nil || len(s.DisallowedTools) > 0 || s.Isolation != "" || s.MaxNesting != nil {
		slog.Warn("codex subagent suppressed", "resource", s.Name(), "reason", "security_restriction")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError, s.Name(), s.Source, "Codex subagent output cannot preserve an explicit tool, isolation, or nesting restriction; artifact suppressed")}, nil
	}
	if s.Ext.Codex != nil && !mapping.CodexSandboxModeSupported(s.Ext.Codex.SandboxMode) {
		slog.Warn("codex subagent suppressed", "resource", s.Name(), "reason", "invalid_sandbox_mode")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeSubagentSecurityRestrictionUnsupported, diag.SeverityError, s.Name(), s.Source, "Codex subagent output cannot safely emit an invalid native sandbox mode; artifact suppressed")}, nil
	}
	var b strings.Builder
	selection, diagnostics, err := compile.SubagentModel(s, mapping.TargetCodex, opts)
	if err != nil {
		return nil, nil, err
	}
	model, effort := selection.Model, selection.Effort
	writeTOMLString(&b, "name", s.Name())
	writeTOMLString(&b, "description", s.Description)
	// Codex model and effort fields were verified 2026-08-19 against
	// https://developers.openai.com/codex/subagents. Native Codex model values
	// and documented reasoning-effort names are preserved exactly.
	writeTOMLString(&b, "model", model)
	writeTOMLString(&b, "model_reasoning_effort", effort)
	writeTOMLString(&b, "developer_instructions", s.Body)
	if s.Ext.Codex != nil {
		writeTOMLString(&b, "sandbox_mode", s.Ext.Codex.SandboxMode)
	}
	if len(s.Extra) > 0 {
		diagnostics = append(diagnostics, diagnostic(diag.CodeCodexFieldDropped, diag.SeverityWarning, s.Name(), s.Source, "Codex subagent output omitted unmodeled extension fields"))
	}
	path, err := codexResourcePath(ir.KindSubagent, s.Name())
	if err != nil {
		return nil, nil, err
	}
	return []artifact.Artifact{{Path: path, Bytes: []byte(b.String())}}, diagnostics, nil
}

func hook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	if h.Ext.Codex != nil && h.Ext.Codex.AdditionalContextLimit != nil && *h.Ext.Codex.AdditionalContextLimit < 0 {
		slog.Warn("codex hook suppressed", "resource", h.Name(), "reason", "invalid_additional_context_limit")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeHookCodexUnsupported, diag.SeverityError, h.Name(), h.Source, "Codex hook has an invalid additional context limit; hook suppressed")}, nil
	}
	// Event, matcher, type, timeout, and command translation always come from
	// the catalog-driven hookpolicy resolver (OpenSpec native-hook-integration,
	// task 3.4). Native hooks arrive here with HookSource provenance — the
	// guard normalizes BASE-era Ext.Codex.NativeHook hooks that lack it — so
	// the resolver's same-harness path keeps the native event spelling while
	// still applying the declared-type, SessionEnd timeout-bound, and
	// plugin-root refusal rules.
	var diagnostics []diag.Diagnostic
	if h.HookSource == string(targetmeta.TargetCodex) {
		// A native Codex hook is valid only where the catalog declares it;
		// undeclared events and matchers on matcher-less events are refused so
		// the hook cannot silently widen.
		if !hookpolicy.IsDeclaredEvent(targetmeta.TargetCodex, h.Event) {
			slog.Warn("codex hook suppressed", "resource", h.Name(), "event", h.Event, "handler_type", h.Type)
			return nil, []diag.Diagnostic{diagnostic(diag.CodeHookCodexUnsupported, diag.SeverityError, h.Name(), h.Source, "Codex does not execute this hook event or handler; hook suppressed")}, nil
		}
		if h.Matcher != "" && hookpolicy.NativeEventMatcherKind(targetmeta.TargetCodex, h.Event) == hookpolicy.MatcherKindNone {
			// A matcher on an event without matchers would widen the hook to
			// every occurrence of the event; fail closed.
			slog.Warn("codex hook suppressed", "resource", h.Name(), "event", h.Event, "handler_type", h.Type)
			return nil, []diag.Diagnostic{diagnostic(diag.CodeHookCodexUnsupported, diag.SeverityError, h.Name(), h.Source, "Codex does not execute this hook event or handler; hook suppressed")}, nil
		}
	}
	scope := hookpolicy.ScopeRepository
	if opts.Scope() == compile.ScopeUser {
		scope = hookpolicy.ScopeMachine
	}
	resolved := hookpolicy.Resolve(h, targetmeta.TargetCodex, scope, opts.PluginRoot())
	if resolved.Failed() {
		slog.Warn("codex hook suppressed", "resource", h.Name(), "event", h.Event, "handler_type", h.Type)
		return nil, resolved.Diagnostics, nil
	}
	event, matcher, command, timeout := resolved.Event, resolved.Matcher, resolved.Command, resolved.Timeout
	diagnostics = append(diagnostics, resolved.Diagnostics...)
	sessionEndAsync := event == "SessionEnd" && h.Ext.Codex != nil && h.Ext.Codex.HookAsync != nil && *h.Ext.Codex.HookAsync
	if command == "" {
		slog.Warn("codex hook suppressed", "resource", h.Name(), "event", h.Event, "handler_type", h.Type)
		return nil, []diag.Diagnostic{diagnostic(diag.CodeHookCodexUnsupported, diag.SeverityError, h.Name(), h.Source, "Codex does not execute this hook event or handler; hook suppressed")}, nil
	}
	handler := map[string]any{"type": h.Type, "command": command}
	if timeout != nil {
		handler["timeout"] = *timeout
	}
	if h.Ext.Codex != nil {
		if h.Ext.Codex.HookAsync != nil && !sessionEndAsync {
			handler["async"] = *h.Ext.Codex.HookAsync
		}
		if h.Ext.Codex.HookStatusMessage != nil {
			handler["statusMessage"] = *h.Ext.Codex.HookStatusMessage
		}
		if h.Ext.Codex.CommandWindows != nil {
			handler["commandWindows"] = *h.Ext.Codex.CommandWindows
		}
		if h.Ext.Codex.AdditionalContextLimit != nil {
			handler["additionalContextLimit"] = *h.Ext.Codex.AdditionalContextLimit
		}
	}
	entry := map[string]any{"hooks": []any{handler}}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	raw, err := json.Marshal(map[string]any{"hooks": map[string]any{event: []any{entry}}})
	if err != nil {
		return nil, nil, err
	}
	if sessionEndAsync {
		diagnostics = append(diagnostics, diagnostic(diag.CodeCodexFieldDropped, diag.SeverityWarning, h.Name(), h.Source, "SessionEnd always runs synchronously; async was omitted"))
	}
	if h.Prompt != "" || len(h.Extra) > 0 {
		diagnostics = append(diagnostics, diagnostic(diag.CodeCodexFieldDropped, diag.SeverityWarning, h.Name(), h.Source, "Codex hook output omitted fields without a documented command-handler equivalent"))
	}
	path, err := codexAggregatePath(ir.KindHook)
	if err != nil {
		return nil, nil, err
	}
	return []artifact.Artifact{{
		Path:                           path,
		Bytes:                          raw,
		Fragment:                       true,
		FragmentKey:                    "hooks",
		FragmentOrderKey:               h.Name(),
		DedupeFragmentEntries:          true,
		Reconciliation:                 artifact.ReconciliationPolicy{Mode: artifact.ReconciliationExternal},
		ExternalReconciliationRequired: true,
	}}, diagnostics, nil
}

func mcp(m *ir.McpServer, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	stdio, remote := m.Command != "", m.URL != ""
	cwd, cwdPresent, cwdInvalid := "", false, false
	if value, ok := m.Extra["cwd"]; ok {
		cwdPresent = true
		cwd, ok = value.(string)
		cwdInvalid = !ok || cwd == ""
	}
	unsupportedTransport := m.Transport != "" && m.Transport != "stdio" && m.Transport != "streamable-http"
	transportMismatch := (stdio && m.Transport != "" && m.Transport != "stdio") || (remote && m.Transport != "" && m.Transport != "streamable-http")
	ext := m.Ext.Codex
	stdioFieldMismatch := stdio && (len(m.Headers) > 0 || m.OAuthResource != "" || ext != nil && (ext.BearerTokenEnvVar != "" || len(ext.EnvHTTPHeaders) > 0))
	remoteFieldMismatch := remote && (len(m.Args) > 0 || len(m.Env) > 0 || cwdPresent)
	if stdio == remote || cwdInvalid || unsupportedTransport || transportMismatch || stdioFieldMismatch || remoteFieldMismatch || m.OAuthClientID != "" || m.OAuthClientSecret != "" {
		slog.Warn("codex MCP server suppressed", "resource", m.Name(), "reason", "security_or_transport_loss")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeMcpCodexSecurityUnsupported, diag.SeverityError, m.Name(), m.Source, "Codex MCP output cannot preserve the requested authentication or transport semantics; server suppressed")}, nil
	}
	var entry mcpOutput
	var unresolvedPluginRoot, interpolationUnsupported, nativeEnvVarsInvalid bool
	if ext != nil && ext.MCP != nil {
		envVars, invalid := codexMCPEnvVarOutput(ext.MCP.Env, ext.MCP.EnvVars, ext.MCP.RemoteEnvVars)
		nativeEnvVarsInvalid = invalid
		entry = mcpOutput{
			Command:                 ext.MCP.Command,
			Args:                    slices.Clone(ext.MCP.Args),
			CWD:                     ext.MCP.CWD,
			ExperimentalEnvironment: ext.MCP.ExperimentalEnvironment,
			Env:                     cloneStringMap(ext.MCP.Env),
			EnvVars:                 envVars,
			URL:                     ext.MCP.URL,
			HTTPHeaders:             cloneStringMap(ext.MCP.HTTPHeaders),
			EnvHTTPHeaders:          cloneStringMap(ext.EnvHTTPHeaders),
		}
	} else {
		command, commandUnresolved := resolvePluginRoot(m.Command, opts)
		args, argsUnresolved := resolvePluginRootSlice(m.Args, opts)
		cwd, cwdUnresolved := resolvePluginRoot(cwd, opts)
		env, envUnresolved := resolvePluginRootMap(m.Env, opts)
		url, urlUnresolved := resolvePluginRoot(m.URL, opts)
		headers, headersUnresolved := resolvePluginRootMap(m.Headers, opts)
		unresolvedPluginRoot = commandUnresolved || argsUnresolved || cwdUnresolved || envUnresolved || urlUnresolved || headersUnresolved
		literalEnv, envVars, envUnsupported := codexMCPEnv(env)
		literalHeaders, envHTTPHeaders, headersUnsupported := codexMCPHeaders(headers)
		interpolationUnsupported = envUnsupported || headersUnsupported || unsupportedCodexMCPInterpolation(command) || unsupportedCodexMCPInterpolation(cwd) || unsupportedCodexMCPInterpolation(url)
		for _, arg := range args {
			interpolationUnsupported = interpolationUnsupported || unsupportedCodexMCPInterpolation(arg)
		}
		entry.EnvVars, nativeEnvVarsInvalid = codexMCPEnvVarOutput(literalEnv, envVars, nil)
		entry.Command, entry.Args, entry.CWD, entry.Env, entry.URL = command, args, cwd, literalEnv, url
		entry.HTTPHeaders, entry.EnvHTTPHeaders = literalHeaders, envHTTPHeaders
	}
	entry.OAuthResource = m.OAuthResource
	if ext != nil {
		entry.BearerTokenEnvVar = ext.BearerTokenEnvVar
		if entry.EnvHTTPHeaders == nil && m.Ext.Codex.EnvHTTPHeaders != nil {
			entry.EnvHTTPHeaders = make(map[string]string, len(m.Ext.Codex.EnvHTTPHeaders))
		}
		for header, envVar := range m.Ext.Codex.EnvHTTPHeaders {
			if existing, exists := entry.EnvHTTPHeaders[header]; exists && existing != envVar {
				interpolationUnsupported = true
				continue
			}
			entry.EnvHTTPHeaders[header] = envVar
		}
	}
	entryStdio, entryRemote := entry.Command != "", entry.URL != ""
	remoteExecution := entry.ExperimentalEnvironment == "remote"
	validEnvironment := entry.ExperimentalEnvironment == "" || entry.ExperimentalEnvironment == "local" || remoteExecution
	entryInvalid := nativeEnvVarsInvalid || entryStdio == entryRemote || entryStdio != stdio || entryRemote != remote ||
		!validEnvironment || ext != nil && ext.MCP != nil && len(ext.MCP.RemoteEnvVars) > 0 && !remoteExecution ||
		entryStdio && (len(entry.HTTPHeaders) > 0 || len(entry.EnvHTTPHeaders) > 0 || entry.BearerTokenEnvVar != "") ||
		entryRemote && (len(entry.Args) > 0 || entry.CWD != "" || entry.ExperimentalEnvironment != "" || len(entry.Env) > 0 || len(entry.EnvVars) > 0)
	if entryInvalid {
		slog.Warn("codex MCP server suppressed", "resource", m.Name(), "reason", "native_sidecar_transport_loss")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeMcpCodexSecurityUnsupported, diag.SeverityError, m.Name(), m.Source, "Codex MCP output cannot preserve the requested authentication or transport semantics; server suppressed")}, nil
	}
	if interpolationUnsupported {
		slog.Warn("codex MCP server suppressed", "resource", m.Name(), "reason", "interpolation_loss")
		return nil, []diag.Diagnostic{diagnostic(diag.CodeMcpCodexSecurityUnsupported, diag.SeverityError, m.Name(), m.Source, "Codex MCP output cannot preserve an environment or file interpolation; server suppressed")}, nil
	}
	if m.DisabledSet {
		enabled := !m.Disabled
		entry.Enabled = &enabled
	}
	raw, err := toml.Marshal(mcpConfig{Servers: map[string]mcpOutput{m.Name(): entry}})
	if err != nil {
		return nil, nil, fmt.Errorf("codex: marshal MCP server %q: %w", m.Name(), err)
	}
	raw = []byte(strings.TrimPrefix(string(raw), "[mcp_servers]\n"))
	diagnostics := compile.McpCredentialDiagnostics(m, mapping.TargetCodex)
	if unresolvedPluginRoot {
		diagnostics = append(diagnostics, diagnostic(diag.CodeMcpCodexPluginRootUnresolved, diag.SeverityWarning, m.Name(), m.Source, "Codex MCP configuration references the plugin root but no plugin root was configured; the token was left unexpanded"))
	}
	var unmodeledExtra bool
	for key := range m.Extra {
		unmodeledExtra = unmodeledExtra || (key != "cwd" && !strings.EqualFold(key, compile.McpPublishingNamespace))
	}
	if unmodeledExtra {
		diagnostics = append(diagnostics, diagnostic(diag.CodeCodexFieldDropped, diag.SeverityWarning, m.Name(), m.Source, "Codex MCP output omitted unmodeled extension fields"))
	}
	path, err := codexAggregatePath(ir.KindMcpServer)
	if err != nil {
		return nil, nil, err
	}
	return []artifact.Artifact{{
		Path:                           path,
		Bytes:                          raw,
		Fragment:                       true,
		FragmentKey:                    "mcp_servers",
		FragmentOrderKey:               m.Name(),
		Reconciliation:                 artifact.ReconciliationPolicy{Mode: artifact.ReconciliationExternal},
		ExternalReconciliationRequired: true,
	}}, diagnostics, nil
}

func codexResourcePath(kind ir.ResourceKind, name string) (string, error) {
	path, ok := targetmeta.NativeResourcePath(targetmeta.TargetCodex, kind, name)
	if !ok {
		return "", fmt.Errorf("codex: native path for %s is not registered", kind)
	}
	return path, nil
}

func codexAggregatePath(kind ir.ResourceKind) (string, error) {
	path, ok := targetmeta.NativeAggregatePath(targetmeta.TargetCodex, kind)
	if !ok {
		return "", fmt.Errorf("codex: native aggregate path for %s is not registered", kind)
	}
	return path, nil
}

func resolvePluginRoot(value string, opts compile.Options) (string, bool) {
	if !strings.Contains(value, claudePluginRootToken) {
		return value, false
	}
	replacement, literal := mapping.EnvToken(claudePluginRootToken, mapping.TargetCodex)
	if literal {
		replacement = opts.PluginRoot()
	}
	if replacement == "" {
		return value, true
	}
	return strings.ReplaceAll(value, claudePluginRootToken, replacement), false
}

func resolvePluginRootSlice(values []string, opts compile.Options) ([]string, bool) {
	resolved := make([]string, len(values))
	var unresolved bool
	for i, value := range values {
		var current bool
		resolved[i], current = resolvePluginRoot(value, opts)
		unresolved = unresolved || current
	}
	return resolved, unresolved
}

func resolvePluginRootMap(values map[string]string, opts compile.Options) (map[string]string, bool) {
	if values == nil {
		return nil, false
	}
	resolved := make(map[string]string, len(values))
	var unresolved bool
	for key, value := range values {
		var current bool
		resolved[key], current = resolvePluginRoot(value, opts)
		unresolved = unresolved || current
	}
	return resolved, unresolved
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

type mcpConfig struct {
	Servers map[string]mcpOutput `toml:"mcp_servers"`
}
type mcpOutput struct {
	Command                 string            `toml:"command,omitempty"`
	Args                    []string          `toml:"args,omitempty"`
	CWD                     string            `toml:"cwd,omitempty"`
	ExperimentalEnvironment string            `toml:"experimental_environment,omitempty"`
	Env                     map[string]string `toml:"env,omitempty"`
	EnvVars                 []any             `toml:"env_vars,omitempty"`
	URL                     string            `toml:"url,omitempty"`
	HTTPHeaders             map[string]string `toml:"http_headers,omitempty"`
	EnvHTTPHeaders          map[string]string `toml:"env_http_headers,omitempty"`
	BearerTokenEnvVar       string            `toml:"bearer_token_env_var,omitempty"`
	OAuthResource           string            `toml:"oauth_resource,omitempty"`
	Enabled                 *bool             `toml:"enabled,omitempty"`
}

func codexMCPEnvVarOutput(env map[string]string, local, remote []string) ([]any, bool) {
	entries := make([]any, 0, len(local)+len(remote))
	seen := make(map[string]struct{}, cap(entries))
	for _, name := range local {
		if !validEnvironmentName(name) {
			return nil, true
		}
		if _, exists := seen[name]; exists {
			return nil, true
		}
		if value, exists := env[name]; exists && value != "${"+name+"}" {
			return nil, true
		}
		seen[name] = struct{}{}
		entries = append(entries, name)
	}
	for _, name := range remote {
		if !validEnvironmentName(name) {
			return nil, true
		}
		if _, exists := seen[name]; exists {
			return nil, true
		}
		if _, exists := env[name]; exists {
			return nil, true
		}
		seen[name] = struct{}{}
		entries = append(entries, map[string]string{"name": name, "source": "remote"})
	}
	return entries, false
}

func codexMCPEnv(values map[string]string) (map[string]string, []string, bool) {
	literal := make(map[string]string, len(values))
	var envVars []string
	for key, value := range values {
		if envVar, ok := codexEnvironmentReference(value); ok {
			if key != envVar {
				return nil, nil, true
			}
			envVars = append(envVars, envVar)
			continue
		}
		if unsupportedCodexMCPInterpolation(value) {
			return nil, nil, true
		}
		literal[key] = value
	}
	if len(literal) == 0 {
		literal = nil
	}
	slices.Sort(envVars)
	return literal, envVars, false
}

func codexMCPHeaders(values map[string]string) (map[string]string, map[string]string, bool) {
	literal := make(map[string]string, len(values))
	envHeaders := make(map[string]string)
	for key, value := range values {
		if envVar, ok := codexEnvironmentReference(value); ok {
			envHeaders[key] = envVar
			continue
		}
		if unsupportedCodexMCPInterpolation(value) {
			return nil, nil, true
		}
		literal[key] = value
	}
	if len(literal) == 0 {
		literal = nil
	}
	if len(envHeaders) == 0 {
		envHeaders = nil
	}
	return literal, envHeaders, false
}

func codexEnvironmentReference(value string) (string, bool) {
	if value == claudePluginRootToken {
		return "", false
	}
	var name, style string
	switch {
	case strings.HasPrefix(value, "${env:") && strings.HasSuffix(value, "}"):
		name, style = strings.TrimSuffix(strings.TrimPrefix(value, "${env:"), "}"), "${env:VAR}"
	case strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") && !strings.Contains(value[2:len(value)-1], ":"):
		name, style = value[2:len(value)-1], "inline"
	default:
		return "", false
	}
	if name == "" || !validEnvironmentName(name) {
		return "", false
	}
	_, supported := mapping.McpInterpolationSupported(style, mapping.TargetCodex)
	return name, supported
}

func unsupportedCodexMCPInterpolation(value string) bool {
	for rest := value; ; {
		start := strings.Index(rest, "${")
		if start < 0 {
			return false
		}
		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			return false
		}
		token := rest[start : start+end+1]
		if token != claudePluginRootToken {
			if strings.HasPrefix(token, "${file:") {
				_, supported := mapping.McpInterpolationSupported("${file:PATH}", mapping.TargetCodex)
				return !supported
			}
			if _, ok := codexEnvironmentReference(token); ok {
				return true
			}
		}
		rest = rest[start+end+1:]
	}
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func writeTOMLString(b *strings.Builder, key, value string) {
	if value != "" {
		raw, _ := toml.Marshal(map[string]string{key: value})
		b.Write(raw)
	}
}

func diagnostic(code string, severity diag.Severity, resource string, source ir.SourceRef, message string) diag.Diagnostic {
	return diag.Diagnostic{Code: code, Severity: severity, Target: string(mapping.TargetCodex), Resource: resource, Message: message, Source: diag.SourceRef(source)}
}
