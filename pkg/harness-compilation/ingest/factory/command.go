package factory

import (
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadCommand reads a Factory command markdown file.
func LoadCommand(src fs.FS, path string) (*ir.Command, error) {
	fm, body, err := parseMarkdownResource(src, path)
	if err != nil {
		return nil, err
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = baseNameWithoutExt(path)
	}
	if err := validateKebabName(name); err != nil {
		return nil, err
	}

	description, _ := fm["description"].(string)
	command := ir.NewCommand(name, description, body, extraFields(fm, commandKnownKeys), sourceRef(path))
	if _, ok := fm["allowed-tools"]; ok {
		command.ToolSource = "factory"
	}
	return command, nil
}
