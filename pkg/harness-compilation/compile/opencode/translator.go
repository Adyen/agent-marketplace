// Package opencode compiles resources into OpenCode's native layout.
package opencode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	ext "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"gopkg.in/yaml.v3"
)

type Compiler struct{}

func init() { compile.Register(Compiler{}) }

func (Compiler) Target() compile.Target { return "opencode" }
func (c Compiler) Translate(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
	return c.TranslateWithOptions(r, compile.Options{})
}
func (c Compiler) TranslateWithOptions(r ir.Resource, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	if _, _, err := ir.ValidateResource(r); err != nil {
		return nil, nil, err
	}
	return compile.GuardNativeCodexTranslation(r, c.Target(), func(r ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
		switch v := r.(type) {
		case *ir.Skill:
			return skill(v)
		case *ir.Command:
			return command(v, opts)
		case *ir.Subagent:
			return agent(v, opts)
		case *ir.McpServer:
			return mcp(v, opts)
		case *ir.Hook:
			return hook(v, opts)
		case *ir.Rule:
			return nil, []diag.Diagnostic{diagnostic(diag.CodeRuleOpenCodeUnsupported, diag.SeverityWarning, v.Name(), v.Source, "Rules and shared instruction generation are outside the OpenCode target")}, nil
		default:
			return nil, nil, fmt.Errorf("opencode: unsupported resource %T", r)
		}
	})
}

func diagnostic(code string, severity diag.Severity, name string, source ir.SourceRef, message string) diag.Diagnostic {
	return diag.Diagnostic{Code: code, Severity: severity, Target: "opencode", Resource: name, Source: compile.DiagSource(source), Message: message}
}

func document(file string, fm *compile.Frontmatter, body string, diagnostics []diag.Diagnostic) ([]artifact.Artifact, []diag.Diagnostic, error) {
	header, err := fm.Render()
	if err != nil {
		return nil, diagnostics, err
	}
	return []artifact.Artifact{{Path: file, Bytes: []byte(header + body), Mode: 0644}}, diagnostics, nil
}

func skill(s *ir.Skill) ([]artifact.Artifact, []diag.Diagnostic, error) {
	restricted := s.AllowedTools != nil || s.PermissionsSet || s.Agent != "" || s.Subagent != ""
	for _, key := range []string{"allowed-tools", "allowedTools", "permissions", "permission", "tools", "agent", "subagent"} {
		if _, ok := s.Extra[key]; ok {
			restricted = true
		}
	}
	if restricted {
		return nil, []diag.Diagnostic{diagnostic(diag.CodeSkillOpenCodeRestrictionUnsupported, diag.SeverityError, s.Name(), s.Source, "OpenCode skill metadata cannot enforce this execution restriction; skill omitted")}, nil
	}
	fm := &compile.Frontmatter{}
	fm.Set("name", s.Name())
	fm.Set("description", s.Description)
	var d []diag.Diagnostic
	for _, key := range sortedKeys(s.Extra) {
		switch key {
		case "license", "compatibility", "metadata":
			fm.Set(key, s.Extra[key])
		default:
			d = append(d, dropped(s.Name(), s.Source))
		}
	}
	if s.ArgumentHint != "" || s.Model != "" || s.Triggers != nil {
		d = append(d, dropped(s.Name(), s.Source))
	}
	d = append(d, compile.SupportFileDiagnostics(s, "opencode")...)
	return document("skills/"+s.Name()+"/SKILL.md", fm, s.Body, d)
}

func dropped(name string, source ir.SourceRef) diag.Diagnostic {
	return diagnostic(diag.CodeOpenCodeFieldDropped, diag.SeverityWarning, name, source, "Metadata has no OpenCode equivalent and was omitted")
}

func command(c *ir.Command, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fm := &compile.Frontmatter{}
	fm.Set("description", c.Description)
	for _, key := range []string{"allowed-tools", "allowedTools", "permissions", "permission", "tools"} {
		if _, ok := c.Extra[key]; ok {
			return nil, []diag.Diagnostic{diagnostic(diag.CodeCommandOpenCodeRestrictionUnsupported, diag.SeverityError, c.Name(), c.Source, "Command execution restriction has no native equivalent; command omitted")}, nil
		}
	}
	var d []diag.Diagnostic
	native := c.Ext.OpenCode
	for _, key := range []string{"agent", "model", "subtask"} {
		value, present := c.Extra[key]
		if native != nil {
			switch key {
			case "agent":
				if native.Agent != "" {
					value, present = native.Agent, true
				}
			case "model":
				if native.Model != "" {
					value, present = native.Model, true
				}
			case "subtask":
				if native.Subtask != nil {
					value, present = *native.Subtask, true
				}
			}
		}
		if present {
			valid := false
			if key == "subtask" {
				_, valid = value.(bool)
			} else {
				_, valid = value.(string)
			}
			if !valid {
				return nil, []diag.Diagnostic{diagnostic(diag.CodeOpenCodeInvalidField, diag.SeverityError, c.Name(), c.Source, "Native command field has an invalid type; command omitted")}, nil
			}
			if key == "model" {
				model := value.(string)
				if override, ok := opts.ModelOverride()[model]; ok {
					value = override
				} else if native == nil || native.Model == "" {
					mapped, ok := mapping.Model(model, mapping.TargetOpenCode)
					switch {
					case ok:
						value = mapped
					case mapped == "":
						// The canonical model has no supported native
						// identifier: omit the selection and report the loss
						// rather than emit a known-invalid model.
						d = append(d, diagnostic(diag.CodeOpenCodeFieldDropped, diag.SeverityWarning, c.Name(), c.Source, "Canonical model has no supported OpenCode identifier; the model selection was omitted"))
						continue
					}
				}
			}
			fm.Set(key, value)
		}
	}
	for key := range c.Extra {
		if key != "agent" && key != "model" && key != "subtask" && key != "name" && key != "description" {
			d = append(d, dropped(c.Name(), c.Source))
		}
	}
	return document("commands/"+c.Name()+".md", fm, c.Body, d)
}

func agent(a *ir.Subagent, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fail := func() ([]artifact.Artifact, []diag.Diagnostic, error) {
		return nil, []diag.Diagnostic{diagnostic(diag.CodeSubagentOpenCodeRestrictionUnsupported, diag.SeverityError, a.Name(), a.Source, "Agent execution restriction has no native equivalent; agent omitted")}, nil
	}
	if a.Isolation != "" || a.MaxNesting != nil {
		return fail()
	}
	for _, key := range []string{"tools", "permission", "permissions", "disallowedTools", "disallowed-tools", "isolation", "max-nesting", "maxNesting"} {
		if _, ok := a.Extra[key]; ok {
			return fail()
		}
	}
	fm := &compile.Frontmatter{}
	fm.Set("description", a.Description)
	native := a.Ext.OpenCode
	mode, model := "subagent", a.Model
	if native != nil {
		if native.Mode != "" {
			mode = native.Mode
		}
		if native.Model != "" {
			model = native.Model
		}
	}
	var d []diag.Diagnostic
	if override, ok := opts.ModelOverride()[a.Model]; ok {
		model = override
	} else if native == nil || native.Model == "" {
		mapped, ok := mapping.Model(model, mapping.TargetOpenCode)
		switch {
		case ok:
			model = mapped
		case mapped == "" && model != "":
			// No supported native identifier exists for this canonical model:
			// omit the selection and report the loss rather than emit a
			// known-invalid model. Explicit native models and overrides are
			// preserved above.
			model = ""
			d = append(d, diagnostic(diag.CodeOpenCodeFieldDropped, diag.SeverityWarning, a.Name(), a.Source, "Canonical model has no supported OpenCode identifier; the model selection was omitted"))
		}
	}
	if mode != "subagent" && mode != "primary" && mode != "all" {
		return fail()
	}
	fm.Set("mode", mode)
	if model != "" {
		fm.Set("model", model)
	}
	var permission *ext.OpenCodePermission
	if a.Tools != nil || a.DisallowedTools != nil {
		permission = &ext.OpenCodePermission{}
		if a.Tools != nil {
			permission.Rules = append(permission.Rules, ext.OpenCodePermissionRule{Tool: "*", Action: "deny"})
		}
		indices := map[string]int{}
		for _, group := range []struct {
			list   []string
			action string
		}{{a.Tools, "allow"}, {a.DisallowedTools, "deny"}} {
			for _, tool := range group.list {
				// Resolve through the authoritative curated mapping and the
				// consumer override, the same authority as hook matchers. An
				// unmappable tool must fail the subagent rather than widen the
				// restriction to nothing.
				mappedTools, ok := mapping.ToolWithOverride(tool, mapping.TargetOpenCode, opts.ToolOverride())
				if !ok || len(mappedTools) != 1 {
					return fail()
				}
				mapped := mappedTools[0]
				if index, exists := indices[mapped]; exists {
					permission.Rules[index].Action = group.action
				} else {
					indices[mapped] = len(permission.Rules)
					permission.Rules = append(permission.Rules, ext.OpenCodePermissionRule{Tool: mapped, Action: group.action})
				}
			}
		}
	}
	if native != nil {
		if native.Permission != nil {
			if permission != nil {
				return fail()
			}
			permission = native.Permission
		}
		if native.Tools != nil || native.ToolsSet {
			fm.Set("tools", native.Tools)
		}
		if native.Hidden != nil {
			fm.Set("hidden", *native.Hidden)
		}
		if native.Temperature != nil {
			fm.Set("temperature", *native.Temperature)
		}
		if native.Steps != nil {
			if *native.Steps < 1 {
				return fail()
			}
			fm.Set("steps", *native.Steps)
		}
	}
	if permission != nil {
		value, err := PermissionNode(permission)
		if err != nil {
			return fail()
		}
		fm.Set("permission", value)
	}
	if a.Effort != "" || len(a.Extra) > 0 {
		d = append(d, dropped(a.Name(), a.Source))
	}
	return document("agents/"+a.Name()+".md", fm, a.Body, d)
}

// PermissionNode retains the authored precedence of native permission rules.
func PermissionNode(p *ext.OpenCodePermission) (*yaml.Node, error) {
	valid := func(s string) bool { return s == "allow" || s == "ask" || s == "deny" }
	scalar := func(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
	if p.Action != "" {
		if !valid(p.Action) || len(p.Rules) > 0 {
			return nil, fmt.Errorf("invalid permission")
		}
		return scalar(p.Action), nil
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := map[string]bool{}
	for _, rule := range p.Rules {
		if rule.Tool == "" || seen[rule.Tool] {
			return nil, fmt.Errorf("duplicate or empty permission tool")
		}
		seen[rule.Tool] = true
		var value *yaml.Node
		if rule.Patterns != nil || rule.PatternsSet {
			if rule.Action != "" {
				return nil, fmt.Errorf("ambiguous permission")
			}
			value = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			patterns := map[string]bool{}
			for _, pattern := range rule.Patterns {
				if patterns[pattern.Pattern] || !valid(pattern.Action) {
					return nil, fmt.Errorf("invalid pattern")
				}
				patterns[pattern.Pattern] = true
				value.Content = append(value.Content, scalar(pattern.Pattern), scalar(pattern.Action))
			}
		} else {
			if !valid(rule.Action) {
				return nil, fmt.Errorf("invalid action")
			}
			value = scalar(rule.Action)
		}
		node.Content = append(node.Content, scalar(rule.Tool), value)
	}
	return node, nil
}

var canonicalRef = regexp.MustCompile(`\$\{(?:(env|file):)?([A-Za-z_][A-Za-z0-9_./-]*)\}`)

func mcp(m *ir.McpServer, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fail := func() ([]artifact.Artifact, []diag.Diagnostic, error) {
		return nil, []diag.Diagnostic{diagnostic(diag.CodeMcpOpenCodeUnrepresentable, diag.SeverityError, m.Name(), m.Source, "MCP fields cannot be represented by OpenCode; server omitted")}, nil
	}
	if (m.Command == "") == (m.URL == "") || m.OAuthResource != "" {
		return fail()
	}
	if m.Transport != "" && m.Transport != "stdio" && m.Transport != "http" && m.Transport != "sse" {
		return fail()
	}
	if m.Command != "" && (m.Headers != nil || m.OAuthClientID != "" || m.OAuthClientSecret != "" || (m.Transport != "" && m.Transport != "stdio")) {
		return fail()
	}
	if m.URL != "" && (m.Args != nil || m.Env != nil || m.Transport == "stdio") {
		return fail()
	}
	native := (*ext.OpenCodeMCP)(nil)
	if m.Ext.OpenCode != nil {
		native = m.Ext.OpenCode.MCP
	}
	unrepresentable := false
	value := func(s string) string {
		if native != nil && native.NativeValues {
			return s
		}
		literal := canonicalRef.ReplaceAllString(s, "")
		if strings.Contains(literal, "{env:") || strings.Contains(literal, "{file:") {
			unrepresentable = true
			return s
		}
		if strings.Contains(s, "${CLAUDE_PLUGIN_ROOT}") {
			if opts.PluginRoot() == "" {
				unrepresentable = true
			} else {
				s = strings.ReplaceAll(s, "${CLAUDE_PLUGIN_ROOT}", opts.PluginRoot())
			}
		}
		if strings.Contains(canonicalRef.ReplaceAllString(s, ""), "${") {
			unrepresentable = true
		}
		return canonicalRef.ReplaceAllStringFunc(s, func(ref string) string {
			parts := canonicalRef.FindStringSubmatch(ref)
			kind := parts[1]
			if kind == "" {
				kind = "env"
			}
			return "{" + kind + ":" + parts[2] + "}"
		})
	}
	server := map[string]any{}
	if m.Command != "" {
		server["type"] = "local"
		argv := []string{value(m.Command)}
		for _, arg := range m.Args {
			argv = append(argv, value(arg))
		}
		server["command"] = argv
		if m.Env != nil {
			env := map[string]string{}
			for key, v := range m.Env {
				env[key] = value(v)
			}
			server["environment"] = env
		}
	} else {
		server["type"] = "remote"
		server["url"] = value(m.URL)
		if m.Headers != nil {
			headers := map[string]string{}
			for key, v := range m.Headers {
				headers[key] = value(v)
			}
			server["headers"] = headers
		}
		if m.OAuthClientID != "" || m.OAuthClientSecret != "" {
			oauth := map[string]string{}
			if m.OAuthClientID != "" {
				oauth["clientId"] = value(m.OAuthClientID)
			}
			if m.OAuthClientSecret != "" {
				oauth["clientSecret"] = value(m.OAuthClientSecret)
			}
			server["oauth"] = oauth
		}
	}
	if m.DisabledSet {
		server["enabled"] = !m.Disabled
	}
	if native != nil {
		if native.Timeout != nil {
			if *native.Timeout < 1 {
				return fail()
			}
			server["timeout"] = *native.Timeout
		}
		if native.OAuthDisabled != nil {
			if m.Command != "" || !*native.OAuthDisabled || server["oauth"] != nil || native.OAuthScope != "" {
				return fail()
			}
			server["oauth"] = false
		}
		if native.OAuthScope != "" {
			if m.Command != "" {
				return fail()
			}
			oauth, _ := server["oauth"].(map[string]string)
			if oauth == nil {
				oauth = map[string]string{}
			}
			oauth["scope"] = value(native.OAuthScope)
			server["oauth"] = oauth
		}
	}
	if unrepresentable || len(m.Extra) > 0 {
		return fail()
	}
	data, err := json.MarshalIndent(map[string]any{"mcp": map[string]any{m.Name(): server}}, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	a := artifact.Artifact{Path: "opencode.json", Bytes: append(data, '\n'), Mode: 0600, Fragment: true, FragmentKey: "opencode.json", Reconciliation: artifact.ReconciliationPolicy{Mode: artifact.ReconciliationExternal}}
	credentialOptions := []compile.Option{compile.WithMcpOptions(opts)}
	if native != nil && native.NativeValues {
		credentialOptions = append(credentialOptions, compile.WithMcpInterpolationPattern(`\{(?:env|file):[^}]+\}`))
	}
	return []artifact.Artifact{a}, compile.McpCredentialDiagnostics(m, "opencode", credentialOptions...), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
