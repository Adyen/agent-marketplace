package sourcerevision

import (
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func legacyModelProjection(resource ir.Resource) ir.Resource {
	switch value := resource.(type) {
	case *ir.Skill:
		copy := *value
		if value.ModelSource == "devin" {
			copy.Model, _ = mapping.ReverseModel(value.Model, mapping.TargetDevin)
		}
		copy.ModelSource = ""
		return &copy
	case *ir.Subagent:
		copy := *value
		switch value.ModelSource {
		case "factory", "cursor", "devin":
			copy.Model, _ = mapping.ReverseModel(value.Model, mapping.Target(value.ModelSource))
		case "codex":
			var nativeEffort string
			copy.Effort, nativeEffort, _ = mapping.CodexReasoningEffort(value.Effort)
			copy.Model = ""
			var sidecar ext.Codex
			if value.Ext.Codex != nil {
				sidecar = *value.Ext.Codex
			}
			sidecar.Model, sidecar.ReasoningEffort = value.Model, nativeEffort
			if value.Ext.Codex != nil || sidecar.Model != "" || nativeEffort != "" {
				copy.Ext.Codex = &sidecar
			}
		}
		copy.ModelSource = ""
		return &copy
	default:
		return resource
	}
}
