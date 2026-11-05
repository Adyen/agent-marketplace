package target

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// ClaudeControlKeys returns the frontmatter keys Claude treats as execution
// controls for a resource kind.
func ClaudeControlKeys(kind ir.ResourceKind) []string {
	switch kind {
	case ir.KindSubagent:
		return []string{"permissionMode", "mcpServers", "hooks"}
	case ir.KindSkill, ir.KindCommand:
		return []string{"hooks"}
	default:
		return nil
	}
}

// IsClaudeControlKey reports whether key is one of the Claude execution
// controls for kind, in any letter case.
func IsClaudeControlKey(kind ir.ResourceKind, key string) bool {
	for _, control := range ClaudeControlKeys(kind) {
		if strings.EqualFold(key, control) {
			return true
		}
	}
	return false
}
