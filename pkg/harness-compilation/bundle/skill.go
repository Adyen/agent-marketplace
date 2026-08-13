package bundle

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadSkill reads a skill directory from src at the given path.
// The directory must contain a SKILL.md file with YAML frontmatter.
// Name defaults to the directory name if not specified in frontmatter.
func LoadSkill(src fs.FS, path string) (*ir.Skill, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}

	mdPath := pathJoin(path, "SKILL.md")
	f, err := src.Open(mdPath)
	if err != nil {
		return nil, fmt.Errorf("bundle: open %s: %w", mdPath, err)
	}
	defer func() { _ = f.Close() }()

	// Name defaults to directory name if not in frontmatter.
	defaultName := dirName(path)
	return parseSkillFromFS(src, f, path, mdPath, defaultName)
}

// LoadRootSkill reads a skill from the root of src (SKILL.md at ".").
// Unlike LoadSkill which takes a subdirectory path, this handles the
// case where the filesystem root IS the skill directory — the name
// must come from frontmatter since dirName(".") is not a valid name.
func LoadRootSkill(src fs.FS) (*ir.Skill, error) {
	f, err := src.Open("SKILL.md")
	if err != nil {
		return nil, fmt.Errorf("bundle: open SKILL.md: %w", err)
	}
	defer func() { _ = f.Close() }()

	// No default name at root — frontmatter must provide one.
	return parseSkillFromFS(src, f, ".", "SKILL.md", "")
}

// parseSkillFromFS is the shared parsing logic for LoadSkill and LoadRootSkill.
// It reads and parses the SKILL.md frontmatter from the already-opened file f,
// builds the ir.Skill, and collects support files. defaultName is used when
// frontmatter doesn't specify a name; when empty, name is required in frontmatter.
func parseSkillFromFS(src fs.FS, f fs.File, skillPath, mdPath, defaultName string) (*ir.Skill, error) {
	fm, body, err := yamlfm.ParseYAMLFrontmatter(f)
	if err != nil {
		return nil, fmt.Errorf("bundle: parse frontmatter %s: %w", mdPath, err)
	}

	name, _ := fm["name"].(string)
	if name == "" {
		if defaultName == "" {
			return nil, fmt.Errorf("bundle: root SKILL.md must declare a 'name' field (directory name is unavailable at root level)")
		}
		name = defaultName
	}
	if err := validateKebabName(name); err != nil {
		return nil, err
	}

	description, _ := fm["description"].(string)
	var triggers []string
	if value, ok := fm["triggers"]; ok {
		triggers, err = toStringSlice(value)
		if err != nil {
			return nil, fmt.Errorf("bundle: invalid triggers in %s: %w", mdPath, err)
		}
	}
	extra := extraFields(fm, skillKnownKeys)
	source := ir.SourceRef{File: mdPath, Line: 1}

	var options []ir.SkillOption
	stringOption := func(key string, apply func(string) ir.SkillOption) error {
		value, ok := fm[key]
		if !ok {
			return nil
		}
		s, err := stringValueStrict(value)
		if err != nil {
			return fmt.Errorf("bundle: invalid %s in %s: %w", key, mdPath, err)
		}
		options = append(options, apply(s))
		return nil
	}

	if err := stringOption("argument-hint", ir.WithSkillArgumentHint); err != nil {
		return nil, err
	}
	if err := stringOption("model", ir.WithSkillModel); err != nil {
		return nil, err
	}
	if err := stringOption("subagent", ir.WithSkillSubagent); err != nil {
		return nil, err
	}
	if err := stringOption("agent", ir.WithSkillAgent); err != nil {
		return nil, err
	}
	if value, ok := fm["allowed-tools"]; ok {
		allowed, err := toStringSlice(value)
		if err != nil {
			return nil, fmt.Errorf("bundle: invalid allowed-tools in %s: %w", mdPath, err)
		}
		options = append(options, ir.WithSkillAllowedTools(allowed))
	}
	if value, ok := fm["permissions"]; ok {
		permissions, err := skillPermissions(value)
		if err != nil {
			return nil, fmt.Errorf("bundle: invalid permissions in %s: %w", mdPath, err)
		}
		options = append(options, ir.WithSkillPermissions(permissions))
	}
	skill := ir.NewSkillWithOptions(name, description, body, triggers, extra, source, options...)
	supportFiles, err := skillSupportFiles(src, skillPath)
	if err != nil {
		return nil, fmt.Errorf("bundle: walk support files %s: %w", skillPath, err)
	}
	skill.SupportFiles = supportFiles

	return skill, nil
}

// pathJoin joins path elements, ensuring no double slashes.
func pathJoin(base, elem string) string {
	return path.Join(base, elem)
}

func skillSupportFiles(src fs.FS, root string) ([]string, error) {
	var files []string
	err := fs.WalkDir(src, root, func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel := strings.TrimPrefix(current, root+"/")
		if rel == current {
			rel = current
		}
		if rel == "SKILL.md" {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}
