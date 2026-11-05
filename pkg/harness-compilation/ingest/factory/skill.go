package factory

import (
	"fmt"
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadSkill reads a Factory skill directory. Factory skill bodies are stored in
// SKILL.md and may optionally include Claude-style frontmatter.
func LoadSkill(src fs.FS, path string) (*ir.Skill, error) {
	fm, body, err := parseMarkdownResource(src, path+"/SKILL.md")
	if err != nil {
		return nil, err
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = dirName(path)
	}
	if err := validateKebabName(name); err != nil {
		return nil, err
	}

	description, _ := fm["description"].(string)
	var triggers []string
	if value, ok := fm["triggers"]; ok {
		triggers, err = toStringSlice(value)
		if err != nil {
			return nil, fmt.Errorf("ingest/factory: invalid triggers in %s: %w", path, err)
		}
	}
	extra := extraFields(fm, skillKnownKeys)

	options := []ir.SkillOption{
		ir.WithSkillArgumentHint(stringValue(fm["argument-hint"])), ir.WithSkillModel(stringValue(fm["model"])),
		ir.WithSkillSubagent(stringValue(fm["subagent"])), ir.WithSkillAgent(stringValue(fm["agent"])),
	}
	if value, ok := fm["allowed-tools"]; ok {
		allowed, err := toStringSlice(value)
		if err != nil {
			return nil, fmt.Errorf("ingest/factory: invalid allowed-tools in %s: %w", path, err)
		}
		options = append(options, ir.WithSkillAllowedTools(allowed), ir.WithSkillToolSource("factory"))
	}
	if value, ok := fm["permissions"]; ok {
		permissions, err := skillPermissions(value)
		if err != nil {
			return nil, fmt.Errorf("ingest/factory: invalid permissions in %s: %w", path, err)
		}
		options = append(options, ir.WithSkillPermissions(permissions))
	}
	skill := ir.NewSkillWithOptions(name, description, body, triggers, extra, sourceRef(path+"/SKILL.md"), options...)
	skill.ModelSource = "factory"
	files, err := supportFiles(src, path)
	if err != nil {
		return nil, fmt.Errorf("ingest/factory: walk support files %s: %w", path, err)
	}
	skill.SupportFiles = files
	return skill, nil
}
