package compile

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
)

// SidecarToolsWithinCanonical reports whether a target sidecar tool list only
// restates or narrows the canonical subagent tool list. Each sidecar entry
// must be a canonical entry or a target tool the canonical list covers when
// translated from source on the subagent tool surface (including caller
// overrides), or fall under a trailing-wildcard canonical entry. An absent
// canonical list imposes no restriction, so any sidecar list narrows it.
func SidecarToolsWithinCanonical(source Target, canonical, sidecar []string, target Target, overrides map[string]string) bool {
	if sidecar == nil || canonical == nil {
		return true
	}
	allowed := make(map[string]struct{}, len(canonical))
	for _, tool := range canonical {
		allowed[tool] = struct{}{}
	}
	if emitted, _, err := toolpolicy.TranslateToolList(source, target, toolpolicy.SurfaceSubagentTools, canonical, overrides); err == nil {
		for _, tool := range emitted {
			allowed[tool] = struct{}{}
		}
	}
	for _, tool := range sidecar {
		if _, ok := allowed[tool]; !ok && !coveredByWildcard(allowed, tool) {
			return false
		}
	}
	return true
}

// coveredByWildcard reports whether tool is a proven subset of an allowed
// trailing-wildcard pattern such as `mcp__github__*`.
func coveredByWildcard(allowed map[string]struct{}, tool string) bool {
	for pattern := range allowed {
		if prefix, ok := strings.CutSuffix(pattern, "*"); ok && strings.HasPrefix(tool, prefix) {
			return true
		}
	}
	return false
}
