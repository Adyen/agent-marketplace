// Package mdc renders ir.Rule resources into the .mdc format shared by the
// Cursor and Windsurf harnesses: YAML frontmatter (description, globs,
// alwaysApply, plus any preserved extra keys) followed by the Markdown body.
//
// Both harnesses emit byte-identical rule content; only the artifact path
// differs (Cursor: rules/<name>.mdc, Windsurf: .windsurf/rules/<name>.mdc),
// so the renderer lives in one place to keep them in lock-step.
package mdc

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Render returns the full .mdc file content for a rule: YAML frontmatter
// followed by the Markdown body, terminated with a trailing newline.
//
// The frontmatter always carries alwaysApply (a meaningful boolean, default
// false); description and globs are emitted only when set. Globs are joined
// into Cursor's native comma-separated form. Preserved extra frontmatter keys
// are appended in sorted order.
func Render(r *ir.Rule) (string, error) {
	body, _, err := RenderWithDiagnostics(r, compile.TargetCursor)
	return body, err
}

// RenderWithDiagnostics renders an MDC rule and reports canonical-extra
// conflicts for the requested target.
func RenderWithDiagnostics(r *ir.Rule, target compile.Target) (string, []diag.Diagnostic, error) {
	var sb strings.Builder
	sb.WriteString("---\n")
	fields := []struct {
		key   string
		value any
		show  bool
	}{
		{key: "description", value: r.Description, show: r.Description != ""},
		{key: "globs", value: strings.Join(r.Globs, ","), show: len(r.Globs) > 0},
		{key: "alwaysApply", value: r.AlwaysApply, show: true},
	}
	for _, field := range fields {
		if !field.show {
			continue
		}
		line, err := compile.RenderYAMLKey(field.key, field.value)
		if err != nil {
			return "", nil, err
		}
		sb.WriteString(line)
	}
	present := make(map[string]bool, len(fields))
	for _, field := range fields {
		present[field.key] = field.show
	}
	extraKeys, diags := compile.ExtraKeys(r.Extra, compile.PresentFields(present), target, r.Name(), diag.SourceRef(r.Source))
	for _, k := range extraKeys {
		line, err := compile.RenderYAMLKey(k, r.Extra[k])
		if err != nil {
			return "", nil, err
		}
		sb.WriteString(line)
	}
	sb.WriteString("---\n")
	sb.WriteString(r.Body)

	body := sb.String()
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return body, diags, nil
}
