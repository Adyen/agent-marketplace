package factory

import (
	"fmt"
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

// LoadSubagent reads a Factory droid markdown file.
func LoadSubagent(src fs.FS, path string) (*ir.Subagent, []diag.Diagnostic, error) {
	fm, body, err := parseMarkdownResource(src, path)
	if err != nil {
		return nil, nil, err
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = baseNameWithoutExt(path)
	}
	if err := validateKebabName(name); err != nil {
		return nil, nil, err
	}

	description, _ := fm["description"].(string)
	model, _ := fm["model"].(string)
	reasoningEffort, _ := fm["reasoningEffort"].(string)
	var isolation string
	if value, ok := fm["isolation"]; ok {
		var valid bool
		isolation, valid = value.(string)
		if !valid {
			return nil, nil, fmt.Errorf("ingest/factory: invalid isolation in %s: value must be a string", path)
		}
	}
	var tools []string
	toolsCategory := ""
	if value, ok := fm["tools"].(string); ok && value == "read-only" {
		toolsCategory = value
	} else if value, ok := fm["tools"]; ok {
		parsed, err := toStringSlice(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/factory: invalid tools in %s: %w", path, err)
		}
		tools = parsed
	}
	var disallowedTools []string
	if value, ok := fm["disallowedTools"]; ok {
		parsed, err := toStringSlice(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/factory: invalid disallowedTools in %s: %w", path, err)
		}
		disallowedTools = parsed
	}

	options := []ir.SubagentOption{}
	maxNestingValue, hasMaxNesting := fm["max-nesting"]
	if !hasMaxNesting {
		maxNestingValue, hasMaxNesting = fm["maxNesting"]
	}
	if hasMaxNesting && maxNestingValue == nil {
		return nil, nil, fmt.Errorf("ingest/factory: invalid max-nesting in %s: value must be an integer", path)
	}
	if maxNesting, err := toIntPtr(maxNestingValue); err != nil {
		return nil, nil, fmt.Errorf("ingest/factory: invalid max-nesting in %s: %w", path, err)
	} else if maxNesting != nil {
		options = append(options, ir.WithSubagentMaxNestingPtr(maxNesting))
	}
	subagent := ir.NewSubagentWithOptions(name, description, body, model, tools, reasoningEffort, disallowedTools, isolation, extraFields(fm, subagentKnownKeys), sourceRef(path), options...)
	subagent.ModelSource = "factory"
	if tools != nil {
		subagent.ToolSource = "factory"
	}
	if toolsCategory != "" {
		if subagent.Ext.Factory == nil {
			subagent.Ext.Factory = &extpkg.Factory{}
		}
		subagent.Ext.Factory.ToolsCategory = toolsCategory
	}
	return subagent, nil, nil
}
