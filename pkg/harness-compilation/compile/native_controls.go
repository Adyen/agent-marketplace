package compile

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// ClaudeExtraWithNativeControls returns the extension fields Claude output
// emits. A Claude execution control comes only from the Claude sidecar that
// Claude ingestion records; the same key arriving from any other source is
// omitted with compile.claude.native-control-omitted. A permissionMode of plan
// is a read-only restriction that SubagentReadOnly keeps, so dropping its
// extension copy reports nothing.
func ClaudeExtraWithNativeControls(kind ir.ResourceKind, extra map[string]any, sidecar *extpkg.Claude, target Target, resource string, source ir.SourceRef) (map[string]any, []diag.Diagnostic) {
	var native map[string]any
	if sidecar != nil {
		native = sidecar.NativeControls
	}
	changed, omitted := false, false
	out := make(map[string]any, len(extra)+len(native))
	for key, value := range extra {
		if !targetmeta.IsClaudeControlKey(kind, key) {
			out[key] = value
			continue
		}
		changed = true
		if !hasFoldedKey(native, key) && !readOnlyPermissionMode(key, value) {
			omitted = true
		}
	}
	for key, value := range native {
		if targetmeta.IsClaudeControlKey(kind, key) {
			out[key] = value
			changed = true
		}
	}
	if !changed {
		return extra, nil
	}
	if !omitted {
		return out, nil
	}
	return out, []diag.Diagnostic{{
		Code:     diag.CodeCompileClaudeNativeControlOmitted,
		Severity: diag.SeverityWarning,
		Target:   string(target),
		Resource: resource,
		Message:  "A Claude execution control from a non-Claude source was omitted; Claude output emits these controls only from Claude-authored resources",
		Hint:     "Author the control in the Claude layout if the resource needs it on Claude",
		Source:   DiagSource(source),
	}}
}

// SubagentReadOnly reports whether a subagent carries a documented read-only
// restriction: Cursor readonly: true, Claude permissionMode: plan, Codex
// sandbox_mode read-only, or Factory tools: read-only. Extension fields count
// as well as typed sidecars, so a restriction is never lost for lack of
// provenance.
func SubagentReadOnly(s *ir.Subagent) bool {
	if readOnlyFields(s.Extra) {
		return true
	}
	if s.Ext.Claude != nil && readOnlyFields(s.Ext.Claude.NativeControls) {
		return true
	}
	if s.Ext.Codex != nil && s.Ext.Codex.SandboxMode == "read-only" {
		return true
	}
	return s.Ext.Factory != nil && s.Ext.Factory.ToolsCategory == "read-only"
}

// normalizeFactoryToolsCategory reads a Factory tools list that holds only the
// read-only category, as Factory ingestion recorded it before the category
// had a sidecar field, as that category rather than as a tool name.
func normalizeFactoryToolsCategory(r ir.Resource) ir.Resource {
	s, ok := r.(*ir.Subagent)
	if !ok || s.ToolSource != string(targetmeta.TargetFactory) || len(s.Tools) != 1 || s.Tools[0] != "read-only" {
		return r
	}
	normalized := *s
	normalized.Tools, normalized.ToolSource = nil, ""
	factory := extpkg.Factory{}
	if s.Ext.Factory != nil {
		factory = *s.Ext.Factory
	}
	factory.ToolsCategory = "read-only"
	normalized.Ext.Factory = &factory
	return &normalized
}

// ReadOnlyUnsupported is the refusal for a target without a read-only
// subagent mode.
func ReadOnlyUnsupported(target Target, s *ir.Subagent) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     diag.CodeSubagentSecurityRestrictionUnsupported,
		Severity: diag.SeverityError,
		Target:   string(target),
		Resource: s.Name(),
		Message:  "Target has no read-only subagent mode; subagent artifact suppressed",
		Hint:     "Use a target with a read-only mode (Claude, Cursor, Codex, Factory) or remove the restriction explicitly",
		Source:   DiagSource(s.Source),
	}
}

// HasReadOnlyClaudeMode reports whether fields already declare permissionMode
// plan, in any letter case.
func HasReadOnlyClaudeMode(fields map[string]any) bool {
	for key, value := range fields {
		if readOnlyPermissionMode(key, value) {
			return true
		}
	}
	return false
}

// HasReadOnlyCursorFlag reports whether fields already declare a true
// readonly flag, in any letter case.
func HasReadOnlyCursorFlag(fields map[string]any) bool {
	for key, value := range fields {
		if !strings.EqualFold(key, "readonly") {
			continue
		}
		if flag, ok := value.(bool); ok && flag {
			return true
		}
		if flag, ok := value.(string); ok && strings.EqualFold(strings.TrimSpace(flag), "true") {
			return true
		}
	}
	return false
}

// WithExtraKey returns a copy of extra in which key, in any letter case, holds
// only value.
func WithExtraKey(extra map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(extra)+1)
	for k, v := range extra {
		if !strings.EqualFold(k, key) {
			out[k] = v
		}
	}
	out[key] = value
	return out
}

func readOnlyFields(fields map[string]any) bool {
	for key, value := range fields {
		if readOnlyPermissionMode(key, value) {
			return true
		}
		if mode, ok := value.(string); ok && (strings.EqualFold(key, "sandbox_mode") || strings.EqualFold(key, "tools")) && strings.EqualFold(strings.TrimSpace(mode), "read-only") {
			return true
		}
		if !strings.EqualFold(key, "readonly") {
			continue
		}
		switch typed := value.(type) {
		case bool:
			if typed {
				return true
			}
		case string:
			if strings.EqualFold(strings.TrimSpace(typed), "true") {
				return true
			}
		}
	}
	return false
}

func readOnlyPermissionMode(key string, value any) bool {
	mode, ok := value.(string)
	return ok && strings.EqualFold(key, "permissionMode") && strings.EqualFold(strings.TrimSpace(mode), "plan")
}

func hasFoldedKey(fields map[string]any, key string) bool {
	for k := range fields {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}
