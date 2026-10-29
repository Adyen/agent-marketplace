package bundle

import (
	"fmt"
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadSubagent reads a subagent .md file from src at the given path.
// The file must have YAML frontmatter with at least a name field.
// The "tools" and "disallowedTools" fields can each be a YAML list or a
// comma-separated string such as "Read, Grep, Bash".
func LoadSubagent(src fs.FS, path string) (*ir.Subagent, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}

	f, err := src.Open(path)
	if err != nil {
		return nil, fmt.Errorf("bundle: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	fm, body, err := yamlfm.ParseYAMLFrontmatter(f)
	if err != nil {
		return nil, fmt.Errorf("bundle: parse frontmatter %s: %w", path, err)
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = baseNameWithoutExt(path)
	}
	if err := validateKebabName(name); err != nil {
		return nil, err
	}

	description, _ := fm["description"].(string)
	model, _ := fm["model"].(string)
	effort, _ := fm["effort"].(string)
	var isolation string
	if value, ok := fm["isolation"]; ok {
		var valid bool
		isolation, valid = value.(string)
		if !valid {
			return nil, fmt.Errorf("bundle: invalid isolation in %s: value must be a string", path)
		}
	}

	var tools []string
	if t, ok := fm["tools"]; ok {
		tools, err = toToolList(t)
		if err != nil {
			return nil, fmt.Errorf("bundle: invalid tools in %s: %w", path, err)
		}
	}

	var disallowedTools []string
	if dt, ok := fm["disallowedTools"]; ok {
		disallowedTools, err = toToolList(dt)
		if err != nil {
			return nil, fmt.Errorf("bundle: invalid disallowedTools in %s: %w", path, err)
		}
	}

	extra := extraFields(fm, subagentKnownKeys)
	source := ir.SourceRef{File: path, Line: 1}
	options := []ir.SubagentOption{}
	maxNestingValue, hasKebabMaxNesting := fm["max-nesting"]
	camelMaxNestingValue, hasCamelMaxNesting := fm["maxNesting"]
	if hasKebabMaxNesting && hasCamelMaxNesting {
		return nil, fmt.Errorf("bundle: invalid max-nesting in %s: max-nesting and maxNesting cannot both be set", path)
	}
	hasMaxNesting := hasKebabMaxNesting
	if !hasMaxNesting {
		maxNestingValue, hasMaxNesting = camelMaxNestingValue, hasCamelMaxNesting
	}
	if hasMaxNesting && maxNestingValue == nil {
		return nil, fmt.Errorf("bundle: invalid max-nesting in %s: value must be an integer", path)
	}
	if maxNesting, err := toIntPtr(maxNestingValue); err != nil {
		return nil, fmt.Errorf("bundle: invalid max-nesting in %s: %w", path, err)
	} else if maxNesting != nil {
		options = append(options, ir.WithSubagentMaxNestingPtr(maxNesting))
	}
	subagent := ir.NewSubagentWithOptions(name, description, body, model, tools, effort, disallowedTools, isolation, extra, source, options...)
	subagent.ModelSource = "claude"
	return subagent, nil
}
