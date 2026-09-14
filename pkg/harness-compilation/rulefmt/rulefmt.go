// Package rulefmt implements the trigger-based Markdown rule format shared by
// Windsurf and Devin.
package rulefmt

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

var validTriggers = map[string]bool{
	"always_on":      true,
	"manual":         true,
	"model_decision": true,
	"agent":          true,
	"glob":           true,
}

// Render serializes a rule using native trigger frontmatter.
func Render(rule *ir.Rule) ([]byte, string, error) {
	body, trigger, _, err := RenderWithDiagnostics(rule, compile.TargetWindsurf)
	return body, trigger, err
}

// RenderWithDiagnostics serializes a rule while reporting metadata that the
// selected trigger cannot represent.
func RenderWithDiagnostics(rule *ir.Rule, target compile.Target) ([]byte, string, []diag.Diagnostic, error) {
	trigger := rule.Trigger
	if trigger == "" {
		switch {
		case rule.AlwaysApply:
			trigger = "always_on"
		case len(rule.Globs) > 0:
			trigger = "glob"
		case rule.Description != "":
			trigger = "model_decision"
		default:
			trigger = "manual"
		}
	}
	if !validTriggers[trigger] {
		return nil, "", nil, fmt.Errorf("unsupported rule trigger %q", trigger)
	}
	if trigger == "glob" && len(rule.Globs) == 0 {
		return nil, "", nil, fmt.Errorf("glob rule %q requires at least one glob", rule.Name())
	}
	if trigger == "model_decision" && rule.Description == "" {
		return nil, "", nil, fmt.Errorf("model_decision rule %q requires a description", rule.Name())
	}

	fm := &compile.Frontmatter{}
	fm.Set("trigger", trigger)
	present := map[string]bool{"trigger": true}
	// Description is legal for every documented trigger. It is required only
	// for model_decision and serves as useful display/context metadata elsewhere.
	if rule.Description != "" {
		fm.Set("description", rule.Description)
		present["description"] = true
	}
	if trigger == "glob" {
		fm.Set("globs", rule.Globs)
		present["globs"] = true
	}
	extraKeys, diags := compile.ExtraKeys(rule.Extra, compile.PresentFields(present), target, rule.Name(), diag.SourceRef(rule.Source))
	for _, key := range extraKeys {
		fm.Set(key, rule.Extra[key])
	}
	if len(rule.Globs) > 0 && trigger != "glob" {
		code := diag.CodeRuleWindsurfMetadataDropped
		if target == compile.TargetDevin {
			code = diag.CodeRuleDevinGlobsDropped
		}
		diags = append(diags, diag.Diagnostic{
			Code: code, Severity: diag.SeverityWarning, Target: string(target),
			Resource: rule.Name(), Source: diag.SourceRef(rule.Source),
			Message: "Rule globs cannot be represented by the selected trigger and were omitted",
			Hint:    "Use trigger: glob to preserve glob-scoped application",
		})
	}
	if rule.AlwaysApply && trigger != "always_on" {
		code := diag.CodeRuleWindsurfMetadataDropped
		if target == compile.TargetDevin {
			code = diag.CodeRuleDevinAlwaysApplyDropped
		}
		diags = append(diags, diag.Diagnostic{
			Code: code, Severity: diag.SeverityWarning, Target: string(target),
			Resource: rule.Name(), Source: diag.SourceRef(rule.Source),
			Message: "Rule alwaysApply metadata conflicts with the selected trigger and was omitted",
			Hint:    "Use trigger: always_on to preserve always-on application",
		})
	}
	frontmatter, err := fm.Render()
	if err != nil {
		return nil, "", nil, fmt.Errorf("render rule frontmatter: %w", err)
	}
	body := frontmatter + rule.Body
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return []byte(body), trigger, diags, nil
}

// Load reads a trigger-based Markdown rule.
func Load(src fs.FS, rulePath, sourcePrefix, target string) (*ir.Rule, error) {
	data, err := fs.ReadFile(src, rulePath)
	if err != nil {
		return nil, fmt.Errorf("%s: read rule %s: %w", target, rulePath, err)
	}
	fm, body, err := yamlfm.ParseYAMLFrontmatter(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: parse rule %s: %w", target, rulePath, err)
	}
	trigger, _ := fm["trigger"].(string)
	description, _ := fm["description"].(string)
	globs := stringList(fm["globs"])
	extra := make(map[string]any)
	for key, value := range fm {
		switch key {
		case "trigger", "description", "globs":
		default:
			extra[key] = value
		}
	}
	if !validTriggers[trigger] {
		if trigger == "" {
			return nil, fmt.Errorf("%s: rule %s has no trigger", target, rulePath)
		}
		return nil, fmt.Errorf("%s: rule %s has unsupported trigger %q", target, rulePath, trigger)
	}
	if trigger == "model_decision" && description == "" {
		return nil, fmt.Errorf("%s: model_decision rule %s has no description", target, rulePath)
	}
	if trigger == "glob" && len(globs) == 0 {
		return nil, fmt.Errorf("%s: glob rule %s has no globs", target, rulePath)
	}
	name := strings.TrimSuffix(path.Base(rulePath), path.Ext(rulePath))
	if err := ir.ValidateName(name); err != nil {
		return nil, fmt.Errorf("%s: invalid rule filename %s: %w", target, rulePath, err)
	}
	rule := ir.NewRule(name, description, body, globs, trigger == "always_on", extra, ir.SourceRef{
		File: strings.TrimPrefix(rulePath, sourcePrefix), Line: 1,
	})
	rule.Trigger = trigger
	return rule, nil
}

func stringList(raw any) []string {
	var values []string
	switch raw := raw.(type) {
	case []any:
		for _, value := range raw {
			if value, ok := value.(string); ok && strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
	case string:
		for _, value := range strings.Split(raw, ",") {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}
