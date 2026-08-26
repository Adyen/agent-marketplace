package artifact

import (
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// ExtractDescription returns the author-written one-line description declared
// by a resource, normalized and bounded by store.BoundDescription.
//
// ir.Resource only exposes Kind and Name, so the concrete kinds are matched
// here in the single place that needs them, mirroring ExtractCapabilities.
//
// Hooks and MCP servers have no description field in the IR and deliberately
// return the empty string. A synthesized stand-in would be actively harmful
// here: an MCP server's nearest equivalent field is its executable Command,
// and this value is published through the API and rendered in a browser.
func ExtractDescription(r ir.Resource) string {
	if r == nil {
		return ""
	}

	// A typed-nil resource pointer reaches this function as a non-nil
	// interface, so each case guards its receiver before reading a field.
	var description string
	switch res := r.(type) {
	case *ir.Skill:
		if res != nil {
			description = res.Description
		}
	case *ir.Command:
		if res != nil {
			description = res.Description
		}
	case *ir.Rule:
		if res != nil {
			description = res.Description
		}
	case *ir.Subagent:
		if res != nil {
			description = res.Description
		}
	case *ir.Collection:
		if res != nil {
			description = res.Description
		}
	}

	return store.BoundDescription(description)
}
