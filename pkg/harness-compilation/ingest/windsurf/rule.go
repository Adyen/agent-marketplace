package windsurf

import (
	"io/fs"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const windsurfWorkspaceRoot = ".windsurf/"

// LoadRule reads a Windsurf rule (.windsurf/rules/<name>.mdc) via the shared
// bundle loader, which auto-detects the .mdc frontmatter
// (globs/alwaysApply/description). The source path is normalized by stripping
// the ".windsurf/" workspace prefix so Source.File is consistent whether the
// rule was loaded from the workspace root or the user root (both become
// "rules/<name>.mdc"), matching Cursor's behavior.
func LoadRule(src fs.FS, path string) (*ir.Rule, error) {
	rule, err := bundle.LoadRule(src, path)
	if err != nil {
		return nil, err
	}
	rule.Source.File = strings.TrimPrefix(rule.Source.File, windsurfWorkspaceRoot)
	return rule, nil
}
