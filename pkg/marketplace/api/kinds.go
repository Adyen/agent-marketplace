package api

import (
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// Marketplace-selectable resource kinds. Collections are virtual and never
// materialize; rules are intentionally out of scope for Marketplace selection.
const (
	KindSkill      = "skill"
	KindCommand    = "command"
	KindSubagent   = "subagent"
	KindHook       = "hook"
	KindMcpServer  = "mcpserver"
	KindCollection = "collection"
)

// SupportedResourceKinds is the ordered list of kinds exposed through the
// Marketplace API and consumer config.
var SupportedResourceKinds = []string{
	KindSkill,
	KindCommand,
	KindSubagent,
	KindHook,
	KindMcpServer,
	KindCollection,
}

// IsMarketplaceKind reports whether kind is a selectable Marketplace resource kind.
func IsMarketplaceKind(kind string) bool {
	return store.IsMarketplaceResourceKind(kind)
}

// ToIR converts a Marketplace kind string to the harness-compilation IR kind.
// Returns the zero value and false for unknown or collection kinds (collections
// are virtual and do not compile).
func ToIR(kind string) (ir.ResourceKind, bool) {
	switch kind {
	case KindSkill:
		return ir.KindSkill, true
	case KindCommand:
		return ir.KindCommand, true
	case KindSubagent:
		return ir.KindSubagent, true
	case KindHook:
		return ir.KindHook, true
	case KindMcpServer:
		return ir.KindMcpServer, true
	}
	return ir.ResourceKind(0), false
}

// FromIR converts a harness-compilation IR kind to a Marketplace kind string.
// Returns the empty string for unknown or rule kinds.
func FromIR(kind ir.ResourceKind) string {
	switch kind {
	case ir.KindSkill:
		return KindSkill
	case ir.KindCommand:
		return KindCommand
	case ir.KindSubagent:
		return KindSubagent
	case ir.KindHook:
		return KindHook
	case ir.KindMcpServer:
		return KindMcpServer
	case ir.KindCollection:
		return KindCollection
	}
	return ""
}
