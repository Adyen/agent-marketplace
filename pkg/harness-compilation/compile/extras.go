package compile

import (
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var (
	defaultMcpCredentialKeyTokens = []string{
		"token", "secret", "password", "api-key", "api_key", "apikey",
		"authorization", "credential", "auth",
	}
	defaultMcpInterpolationPattern = regexp.MustCompile(`\$\{(?:[A-Za-z_][A-Za-z0-9_]*|env:[A-Za-z_][A-Za-z0-9_]*|file:[^{}]+)\}`)
)

// SkillPermissionsValue preserves nil (absent) versus empty (explicit) lists.
func SkillPermissionsValue(allow, ask, deny []string, explicit bool) map[string]any {
	return SkillPermissionsValueWithExtra(allow, ask, deny, nil, explicit)
}

// SkillPermissionsValueWithExtra preserves explicit policy lists and unknown
// nested permission keys while keeping absent permissions distinct from {}.
func SkillPermissionsValueWithExtra(allow, ask, deny []string, extra map[string]any, explicit bool) map[string]any {
	if !explicit && allow == nil && ask == nil && deny == nil && len(extra) == 0 {
		return nil
	}
	value := make(map[string]any, len(extra)+3)
	if allow != nil {
		value["allow"] = allow
	}
	if ask != nil {
		value["ask"] = ask
	}
	if deny != nil {
		value["deny"] = deny
	}
	for key, nested := range extra {
		if _, known := value[key]; !known {
			value[key] = nested
		}
	}
	return value
}

// CanonicalFields builds a set of modeled field names for ExtraKeys.
func CanonicalFields(fields ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		result[field] = struct{}{}
	}
	return result
}

// PresentFields returns only modeled fields that the translator will emit.
func PresentFields(fields map[string]bool) map[string]struct{} {
	result := make(map[string]struct{}, len(fields))
	for field, present := range fields {
		if present {
			result[field] = struct{}{}
		}
	}
	return result
}

func HookExtraKeys(extra map[string]any, canonical map[string]struct{}, target Target, resource string, source diag.SourceRef) ([]string, []diag.Diagnostic) {
	reserved := CanonicalFields("event", "type", "matcher", "command", "prompt", "timeout", "hooks")
	for key := range canonical {
		reserved[key] = struct{}{}
	}
	fields := make([]string, 0, len(reserved))
	for field := range reserved {
		fields = append(fields, field)
	}
	for key := range extra {
		for _, field := range fields {
			if strings.EqualFold(key, field) {
				reserved[key] = struct{}{}
				break
			}
		}
	}
	return ExtraKeys(extra, reserved, target, resource, source)
}

const McpPublishingNamespace = "_agent-marketplace"

// ExtraKeys returns preserved extra-field keys in stable order and reports every
// canonical field that would otherwise be duplicated. Translators must emit
// canonical fields first and use these keys only for non-conflicting extras.
func ExtraKeys(extra map[string]any, canonical map[string]struct{}, target Target, resource string, source diag.SourceRef) ([]string, []diag.Diagnostic) {
	return selectExtraKeys(extra, canonical, target, resource, source, false)
}

func McpExtraKeys(extra map[string]any, canonical map[string]struct{}, target Target, resource string, source diag.SourceRef) ([]string, []diag.Diagnostic) {
	return selectExtraKeys(extra, canonical, target, resource, source, true)
}

func selectExtraKeys(extra map[string]any, canonical map[string]struct{}, target Target, resource string, source diag.SourceRef, excludePublishingNamespace bool) ([]string, []diag.Diagnostic) {
	keys := make([]string, 0, len(extra))
	var diags []diag.Diagnostic
	for key := range extra {
		if excludePublishingNamespace && strings.EqualFold(key, McpPublishingNamespace) {
			continue
		}
		if _, conflict := canonical[key]; conflict {
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeCanonicalFieldOverridesExtra,
				Severity: diag.SeverityWarning,
				Target:   string(target),
				Resource: resource,
				Message:  fmt.Sprintf("canonical field %q overrides preserved extra", key),
				Hint:     "The canonical modeled value is emitted; rename the extension field to preserve both values",
				Source:   source,
			})
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sort.Slice(diags, func(i, j int) bool { return diags[i].Message < diags[j].Message })
	return keys, diags
}

// SupportFileDiagnostics reports that SupportFiles are source-relative names
// only; this compilation layer has no bytes to materialize for them.
func SupportFileDiagnostics(skill *ir.Skill, target Target) []diag.Diagnostic {
	if len(skill.SupportFiles) == 0 {
		return nil
	}
	slog.Debug("skill support files deferred", "target", target, "resource", skill.Name(), "count", len(skill.SupportFiles))
	return []diag.Diagnostic{{
		Code:     diag.CodeSkillSupportFilesDeferred,
		Severity: diag.SeverityWarning,
		Target:   string(target),
		Resource: skill.Name(),
		Message:  "skill support files were not materialized because the IR carries names without file bytes",
		Hint:     "Materialize the source-relative support files in the owning layer before publishing this target",
		Source:   diag.SourceRef(skill.Source),
	}}
}

// McpCredentialDiagnostics warns about literal credential-like MCP values
// without ever copying their values into a diagnostic. Canonical-looking keys
// in Extra are inspected too, because JSON ingestion represents their object
// values as map[string]any rather than typed IR fields.
func McpCredentialDiagnostics(server *ir.McpServer, target Target, opts ...Option) []diag.Diagnostic {
	return mcpCredentialDiagnosticsForFields(
		server.Name(), diag.SourceRef(server.Source), target,
		server.Env, server.Headers, server.OAuthClientSecret, server.Extra, "", opts...,
	)
}

// McpCredentialDiagnosticsForFields is shared by top-level MCP resources and
// target-native inline MCP definitions. fieldPrefix identifies nested values
// in logs only; secret values are never included in diagnostics or logs.
func McpCredentialDiagnosticsForFields(resource string, source diag.SourceRef, target Target, env, headers map[string]string, oauthClientSecret, fieldPrefix string, opts ...Option) []diag.Diagnostic {
	return mcpCredentialDiagnosticsForFields(resource, source, target, env, headers, oauthClientSecret, nil, fieldPrefix, opts...)
}

func mcpCredentialDiagnosticsForFields(resource string, source diag.SourceRef, target Target, env, headers map[string]string, oauthClientSecret string, extra map[string]any, fieldPrefix string, opts ...Option) []diag.Diagnostic {
	cfg := Options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	fields := make([]string, 0)
	for key, value := range env {
		if cfg.isCredentialKey(key) && value != "" && !cfg.isInterpolation(value) {
			fields = append(fields, fieldPrefix+"env."+key)
		}
	}
	for key, value := range headers {
		if cfg.isCredentialKey(key) && value != "" && !cfg.isInterpolation(value) {
			fields = append(fields, fieldPrefix+"headers."+key)
		}
	}
	if oauthClientSecret != "" && !cfg.isInterpolation(oauthClientSecret) {
		fields = append(fields, fieldPrefix+"oauthClientSecret")
	}
	fields = append(fields, mcpCredentialExtraFields(extra, fieldPrefix, cfg)...)
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
	slog.Warn("literal MCP credential detected", "target", target, "resource", resource, "fields", strings.Join(fields, ","))
	return []diag.Diagnostic{{
		Code:     diag.CodeMcpLiteralCredential,
		Severity: diag.SeverityWarning,
		Target:   string(target),
		Resource: resource,
		Message:  "MCP configuration contains a literal credential value",
		Hint:     "Use environment interpolation or an external secret reference instead of embedding credentials",
		Source:   source,
	}}
}

func mcpCredentialExtraFields(extra map[string]any, fieldPrefix string, cfg Options) []string {
	fields := make([]string, 0)
	for _, field := range []string{"env", "headers"} {
		value, ok := extra[field]
		if !ok {
			continue
		}
		switch values := value.(type) {
		case map[string]any:
			for key, raw := range values {
				value, ok := raw.(string)
				if ok && cfg.isCredentialKey(key) && value != "" && !cfg.isInterpolation(value) {
					fields = append(fields, fieldPrefix+field+"."+key)
				}
			}
		case map[string]string:
			for key, value := range values {
				if cfg.isCredentialKey(key) && value != "" && !cfg.isInterpolation(value) {
					fields = append(fields, fieldPrefix+field+"."+key)
				}
			}
		}
	}
	if value, ok := extra["oauthClientSecret"].(string); ok && value != "" && !cfg.isInterpolation(value) {
		fields = append(fields, fieldPrefix+"oauthClientSecret")
	}
	return fields
}

func (o Options) isCredentialKey(key string) bool {
	key = strings.ToLower(key)
	for _, token := range o.McpCredentialKeyTokens() {
		if strings.Contains(key, token) {
			return true
		}
	}
	return false
}

// isInterpolation accepts only recognized complete ${...} interpolation
// tokens, optionally embedded in surrounding text. A bare $literal or a
// malformed token is not treated as a safe secret reference.
func (o Options) isInterpolation(value string) bool {
	matches := o.McpInterpolationPattern().FindAllStringIndex(value, -1)
	if len(matches) == 0 {
		return false
	}
	remaining := o.McpInterpolationPattern().ReplaceAllString(value, "")
	return !strings.Contains(remaining, "$")
}

// DiagSource converts an IR source reference into a diagnostic source
// reference. Every target translator shares this conversion so diagnostics
// report source locations identically.
func DiagSource(source ir.SourceRef) diag.SourceRef {
	return diag.SourceRef{File: source.File, Line: source.Line}
}

// SourceOf returns the diagnostic source reference for any canonical IR
// resource. Unknown or nil resources yield the zero SourceRef.
func SourceOf(resource ir.Resource) diag.SourceRef {
	switch typed := resource.(type) {
	case *ir.Skill:
		return DiagSource(typed.Source)
	case *ir.Command:
		return DiagSource(typed.Source)
	case *ir.Subagent:
		return DiagSource(typed.Source)
	case *ir.Hook:
		return DiagSource(typed.Source)
	case *ir.McpServer:
		return DiagSource(typed.Source)
	case *ir.Rule:
		return DiagSource(typed.Source)
	}
	return diag.SourceRef{}
}
