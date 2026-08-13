package e2e_test

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ---------- skill fixture builder ----------

// Skill describes a skill resource for fixture creation.
type Skill struct {
	Name        string
	Description string
	Tags        []string
	Requires    []string
}

// SkillFile returns the SKILL.md content for this skill.
func (s Skill) SkillFile() string {
	desc := s.Description
	if desc == "" {
		desc = s.Name + " skill"
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + s.Name + "\n")
	b.WriteString("description: " + desc + "\n")
	if len(s.Tags) > 0 || len(s.Requires) > 0 {
		b.WriteString("metadata:\n")
	}
	if len(s.Tags) > 0 {
		b.WriteString("  tags:\n")
		for _, tag := range s.Tags {
			b.WriteString("    - " + tag + "\n")
		}
	}
	if len(s.Requires) > 0 {
		b.WriteString("  requires:\n")
		for _, requirement := range s.Requires {
			b.WriteString("    - " + requirement + "\n")
		}
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + s.Name + "\n")
	return b.String()
}

// SkillPath returns the path within a registry for this skill's SKILL.md.
func (s Skill) SkillPath() string {
	return fmt.Sprintf("skills/%s/SKILL.md", s.Name)
}

// ---------- command fixture builder ----------

// Command describes a command resource for fixture creation.
type Command struct {
	Name        string
	Description string
	Command     string
	Tags        []string
}

// CommandFile returns the COMMAND.md content.
func (c Command) CommandFile() string {
	desc := c.Description
	if desc == "" {
		desc = c.Name + " command"
	}
	cmd := c.Command
	if cmd == "" {
		cmd = "echo " + c.Name
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + c.Name + "\n")
	b.WriteString("description: " + desc + "\n")
	b.WriteString("command: " + cmd + "\n")
	if len(c.Tags) > 0 {
		b.WriteString("metadata:\n")
		b.WriteString("  tags:\n")
		for _, tag := range c.Tags {
			b.WriteString("    - " + tag + "\n")
		}
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + c.Name + "\n")
	return b.String()
}

// CommandPath returns the path within a registry.
func (c Command) CommandPath() string {
	return fmt.Sprintf("commands/%s.md", c.Name)
}

// ---------- subagent fixture builder ----------

// Subagent describes a subagent resource for fixture creation.
type Subagent struct {
	Name            string
	Description     string
	Tools           []string
	Model           string   // e.g. "sonnet", "opus"
	Effort          string   // e.g. "high", "low"
	DisallowedTools []string // e.g. "Bash"
	Isolation       string   // e.g. "worktree"
}

// SubagentFile returns the AGENT.md content.
func (s Subagent) SubagentFile() string {
	desc := s.Description
	if desc == "" {
		desc = s.Name + " agent"
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + s.Name + "\n")
	b.WriteString("description: " + desc + "\n")
	if s.Model != "" {
		b.WriteString("model: " + s.Model + "\n")
	}
	if len(s.Tools) > 0 {
		b.WriteString("tools:\n")
		for _, tool := range s.Tools {
			b.WriteString("  - " + tool + "\n")
		}
	}
	if s.Effort != "" {
		b.WriteString("effort: " + s.Effort + "\n")
	}
	if len(s.DisallowedTools) > 0 {
		b.WriteString("disallowedTools:\n")
		for _, dt := range s.DisallowedTools {
			b.WriteString("  - " + dt + "\n")
		}
	}
	if s.Isolation != "" {
		b.WriteString("isolation: " + s.Isolation + "\n")
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + s.Name + "\n")
	return b.String()
}

// SubagentPath returns the path within a registry.
func (s Subagent) SubagentPath() string {
	return fmt.Sprintf("agents/%s.md", s.Name)
}

// ---------- MCP server fixture builder ----------

// MCPServer describes an MCP server resource for fixture creation.
type MCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

// MCPFile returns the .mcp.json content.
func (m MCPServer) MCPFile() string {
	// Build JSON manually with proper escaping to keep it simple and deterministic.
	var b strings.Builder
	b.WriteString(`{"mcpServers":{`)
	b.WriteString(strconv.Quote(m.Name))
	b.WriteString(`:{`)
	b.WriteString(`"command":`)
	b.WriteString(strconv.Quote(m.Command))
	if len(m.Args) > 0 {
		b.WriteString(`,"args":[`)
		for i, a := range m.Args {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(a))
		}
		b.WriteByte(']')
	}
	if len(m.Env) > 0 {
		keys := make([]string, 0, len(m.Env))
		for k := range m.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString(`,"env":{`)
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(k))
			b.WriteByte(':')
			b.WriteString(strconv.Quote(m.Env[k]))
		}
		b.WriteByte('}')
	}
	b.WriteString("}}}")
	return b.String()
}

// MCPPath returns the path within a registry.
// Uses .mcp.json (Claude convention) so that autodetect discovers MCP
// servers even when the registry is detected as Claude layout (which
// is checked before Factory in the autodetect order).
func (m MCPServer) MCPPath() string {
	return ".mcp.json"
}

// ---------- registry builder ----------

// RegistryFiles builds a files map suitable for Env.CreateBareRepo from a
// mix of skills, commands, subagents, hooks, and MCP servers.
func RegistryFiles(skills []Skill, commands []Command, subagents []Subagent) map[string]string {
	files := make(map[string]string)
	for _, s := range skills {
		files[s.SkillPath()] = s.SkillFile()
	}
	for _, c := range commands {
		files[c.CommandPath()] = c.CommandFile()
	}
	for _, a := range subagents {
		files[a.SubagentPath()] = a.SubagentFile()
	}
	return files
}

// RegistryFilesEx builds a files map from all resource kinds including
// MCP servers. When multiple MCP servers are present, they are merged
// into a single mcp.json file.
func RegistryFilesEx(skills []Skill, commands []Command, subagents []Subagent, mcps []MCPServer) map[string]string {
	files := RegistryFiles(skills, commands, subagents)
	if len(mcps) > 0 {
		files[".mcp.json"] = MergeMCPFile(mcps)
	}
	return files
}

// MergeMCPFile produces a single mcp.json containing all given MCP servers.
func MergeMCPFile(mcps []MCPServer) string {
	var b strings.Builder
	b.WriteString(`{"mcpServers":{`)
	for i, m := range mcps {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(m.Name))
		b.WriteString(`:{`)
		b.WriteString(`"command":`)
		b.WriteString(strconv.Quote(m.Command))
		if len(m.Args) > 0 {
			b.WriteString(`,"args":[`)
			for j, a := range m.Args {
				if j > 0 {
					b.WriteByte(',')
				}
				b.WriteString(strconv.Quote(a))
			}
			b.WriteByte(']')
		}
		if len(m.Env) > 0 {
			keys := make([]string, 0, len(m.Env))
			for k := range m.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString(`,"env":{`)
			for j, k := range keys {
				if j > 0 {
					b.WriteByte(',')
				}
				b.WriteString(strconv.Quote(k))
				b.WriteByte(':')
				b.WriteString(strconv.Quote(m.Env[k]))
			}
			b.WriteByte('}')
		}
		b.WriteByte('}')
	}
	b.WriteString("}}")
	return b.String()
}

// SkillsOnly is a convenience for registries with only skills.
func SkillsOnly(skills ...Skill) map[string]string {
	return RegistryFiles(skills, nil, nil)
}
