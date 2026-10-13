package compile

import (
	"slices"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
)

// SidecarToolsWithinCanonical reports whether a target sidecar tool list only
// restates or narrows the canonical tool list. Each sidecar entry must be a
// canonical entry, belong to that entry's target mapping (including caller
// overrides), or reverse-map to a canonical entry. An absent canonical list
// imposes no restriction, so any sidecar list narrows it.
func SidecarToolsWithinCanonical(canonical, sidecar []string, target Target, overrides map[string]string) bool {
	if sidecar == nil || canonical == nil {
		return true
	}
	allowed := make(map[string]struct{}, len(canonical))
	for _, tool := range canonical {
		allowed[tool] = struct{}{}
		if mapped, ok := mapping.ToolWithOverride(tool, target, overrides); ok {
			for _, native := range mapped {
				allowed[native] = struct{}{}
			}
		}
	}
	for _, tool := range sidecar {
		if _, ok := allowed[tool]; ok {
			continue
		}
		if reversed, ok := mapping.ReverseTool(tool, target); ok && slices.Contains(canonical, reversed) {
			continue
		}
		return false
	}
	return true
}
