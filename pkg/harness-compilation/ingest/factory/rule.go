package factory

import (
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// LoadRule reads a Factory rule file. Factory rules (.factory/rules/) are plain
// Markdown convention files, but the shared bundle loader also tolerates .mdc
// frontmatter (globs/alwaysApply/description) — including the unquoted-glob
// dialect used by real Cursor/Windsurf rules — so cross-authored rules ingest
// cleanly.
func LoadRule(src fs.FS, path string) (*ir.Rule, error) {
	return bundle.LoadRule(src, path)
}
