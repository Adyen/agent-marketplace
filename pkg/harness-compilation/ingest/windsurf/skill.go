package windsurf

import (
	"io/fs"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func LoadSkill(src fs.FS, path string) (*ir.Skill, error) {
	skill, err := bundle.LoadSkill(src, path)
	if err != nil {
		return nil, err
	}
	skill.ModelSource = "windsurf"
	return skill, nil
}
