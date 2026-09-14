package windsurf

import (
	"io/fs"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/rulefmt"
)

const windsurfWorkspaceRoot = ".windsurf/"

// LoadRule reads Windsurf's native trigger-based .md rule schema.
func LoadRule(src fs.FS, rulePath string) (*ir.Rule, error) {
	if strings.HasSuffix(rulePath, ".mdc") {
		rule, err := bundle.LoadRule(src, rulePath)
		if err != nil {
			return nil, err
		}
		rule.Source.File = strings.TrimPrefix(rule.Source.File, windsurfWorkspaceRoot)
		return rule, nil
	}
	return rulefmt.Load(src, rulePath, windsurfWorkspaceRoot, "windsurf")
}
