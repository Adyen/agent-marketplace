package windsurf

import (
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func LoadSkill(src fs.FS, path string) (*ir.Skill, error) {
	return bundle.LoadSkill(src, path)
}
