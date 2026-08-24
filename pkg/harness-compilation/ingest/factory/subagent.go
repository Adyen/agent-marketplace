package factory

import (
	"fmt"
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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
	if value, ok := fm["tools"]; ok {
		parsed, err := toStringSlice(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/factory: invalid tools in %s: %w", path, err)
		}
		tools, ok = normalizeFactoryTools(parsed)
		if !ok {
			return nil, nil, fmt.Errorf("ingest/factory: tools in %s cannot be represented in canonical tool vocabulary", path)
		}
	}
	var disallowedTools []string
	if value, ok := fm["disallowedTools"]; ok {
		parsed, err := toStringSlice(value)
		if err != nil {
			return nil, nil, fmt.Errorf("ingest/factory: invalid disallowedTools in %s: %w", path, err)
		}
		disallowedTools = parsed
	}

	canonicalModel := reverseFactoryModel(model)
	var diags []diag.Diagnostic
	if reasoningEffort != "" && canonicalModel != "" && canonicalModel != model {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeIngestFactoryReasoningEffortMappedToModel,
			Severity: diag.SeverityInfo,
			Target:   "factory",
			Resource: name,
			Message:  fmt.Sprintf("mapped Factory model %q to canonical model %q while preserving reasoningEffort=%q", model, canonicalModel, reasoningEffort),
			Hint:     "Compile back to Factory to restore the concrete Factory model identifier",
			Source:   diag.SourceRef{File: path, Line: 1},
		})
	}
	if canonicalModel == "" {
		canonicalModel = model
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
	return ir.NewSubagentWithOptions(name, description, body, canonicalModel, tools, reasoningEffort, disallowedTools, isolation, extraFields(fm, subagentKnownKeys), sourceRef(path), options...), diags, nil
}

func reverseFactoryModel(model string) string {
	switch model {
	case "claude-sonnet-4-5-20250929":
		return "sonnet"
	case "claude-opus-4-20250514":
		return "opus"
	case "claude-haiku-3-5-20241022":
		return "haiku"
	default:
		return model
	}
}

func normalizeFactoryTools(tools []string) ([]string, bool) {
	result := make([]string, 0, len(tools))
	for i := 0; i < len(tools); i++ {
		if i+1 < len(tools) && isEditCreatePair(tools[i], tools[i+1]) {
			result = append(result, "Write")
			i++
			continue
		}
		canonical, ok := mapping.ReverseTool(tools[i], mapping.TargetFactory)
		if !ok {
			return nil, false
		}
		result = append(result, canonical)
	}
	return result, true
}

func isEditCreatePair(a, b string) bool {
	return (a == "Edit" && b == "Create") || (a == "Create" && b == "Edit")
}
