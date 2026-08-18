package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
	if _, _, err := ir.ValidateResource(r); err != nil {
		return nil, nil, err
	}
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
	default:
		return nil, nil, fmt.Errorf("codex: unsupported resource kind %s", r.Kind())
	}
}

func skill(s *ir.Skill, command bool) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var frontmatter compile.Frontmatter
	frontmatter.Set("name", s.Name())
	frontmatter.Set("description", s.Description)
	if command {
		frontmatter.Set("x-agent-marketplace-kind", "command")
	}
	header, err := frontmatter.Render()
	if err != nil {
		return nil, nil, err
	}
	return []artifact.Artifact{{Path: ".agents/skills/" + s.Name() + "/SKILL.md", Bytes: []byte(header + s.Body)}}, nil, nil
}

func command(c *ir.Command) ([]artifact.Artifact, []diag.Diagnostic, error) {
	s := ir.NewSkill(c.Name(), c.Description, c.Body, nil, c.Extra, c.Source)
	return skill(s, true)
}

func subagent(s *ir.Subagent) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var b strings.Builder
	tomlString(&b, "name", s.Name())
	tomlString(&b, "description", s.Description)
	tomlString(&b, "model", s.Model)
	tomlString(&b, "model_reasoning_effort", s.Effort)
	tomlString(&b, "developer_instructions", s.Body)
	return []artifact.Artifact{{Path: ".codex/agents/" + s.Name() + ".toml", Bytes: []byte(b.String())}}, nil, nil
}

func hook(h *ir.Hook) ([]artifact.Artifact, []diag.Diagnostic, error) {
	if h.Type != "command" {
		return nil, nil, fmt.Errorf("codex: hook %q has unsupported type %q", h.Name(), h.Type)
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
	return []artifact.Artifact{{Path: ".codex/hooks.json", Bytes: raw, Fragment: true, FragmentKey: "hooks", FragmentOrderKey: h.Name(), MergeWithExisting: true, DedupeFragmentEntries: true}}, nil, nil
}

func mcp(m *ir.McpServer) ([]artifact.Artifact, []diag.Diagnostic, error) {
	var b strings.Builder
	section := "mcp_servers." + strconv.Quote(m.Name())
	b.WriteByte('[')
	b.WriteString(section)
	b.WriteString("]\n")
	if m.Command != "" {
		tomlString(&b, "command", m.Command)
	}
	if len(m.Args) > 0 {
		b.WriteString("args = [")
		for i, arg := range m.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(arg))
		}
		b.WriteString("]\n")
	}
	if m.URL != "" {
		tomlString(&b, "url", m.URL)
	}
	if len(m.Env) > 0 {
		b.WriteString("[" + section + ".env]\n")
		keys := sortedKeys(m.Env)
		for _, key := range keys {
			tomlString(&b, key, m.Env[key])
		}
	}
	return []artifact.Artifact{{Path: ".codex/config.toml", Bytes: []byte(b.String()), Fragment: true, FragmentKey: "mcp_servers", FragmentOrderKey: m.Name(), MergeWithExisting: true}}, nil, nil
}

func tomlString(b *strings.Builder, key, value string) {
	if value != "" {
		b.WriteString(key + " = " + strconv.Quote(value) + "\n")
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
