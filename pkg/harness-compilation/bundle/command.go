package bundle

import (
	"fmt"
	"io/fs"
	"path"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadCommand reads a command .md file from src at the given path.
// The file must have YAML frontmatter with at least a name field.
func LoadCommand(src fs.FS, path string) (*ir.Command, error) {
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
		// Derive name from filename (without extension).
		name = baseNameWithoutExt(path)
	}
	if err := validateKebabName(name); err != nil {
		return nil, err
	}

	description, _ := fm["description"].(string)
	extra := extraFields(fm, commandKnownKeys)
	source := ir.SourceRef{File: path, Line: 1}

	return ir.NewCommand(name, description, body, extra, source), nil
}

// baseNameWithoutExt returns the base filename without the .md extension.
func baseNameWithoutExt(p string) string {
	base := path.Base(p)
	if len(base) > 3 && base[len(base)-3:] == ".md" {
		return base[:len(base)-3]
	}
	return base
}
