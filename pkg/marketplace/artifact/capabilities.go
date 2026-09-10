package artifact

import (
	"fmt"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// ExtractCapabilities returns the well-known capability strings declared by a
// resource. Capabilities are sorted and de-duplicated.
func ExtractCapabilities(r ir.Resource) []string {
	if r == nil {
		return nil
	}

	var caps []string
	switch res := r.(type) {
	case *ir.Skill:
		caps = append(caps, "skill")
		if len(res.AllowedTools) > 0 {
			caps = append(caps, "tool:allowed")
			for _, t := range res.AllowedTools {
				caps = append(caps, fmt.Sprintf("tool:%s", t))
			}
		}
		if res.PermissionsSet || len(res.Permissions.Allow) > 0 || len(res.Permissions.Ask) > 0 || len(res.Permissions.Deny) > 0 {
			caps = append(caps, "permission:policy")
			for _, t := range res.Permissions.Allow {
				caps = append(caps, fmt.Sprintf("permission:allow:%s", t))
			}
			for _, t := range res.Permissions.Ask {
				caps = append(caps, fmt.Sprintf("permission:ask:%s", t))
			}
			for _, t := range res.Permissions.Deny {
				caps = append(caps, fmt.Sprintf("permission:deny:%s", t))
			}
		}
		if res.Subagent != "" {
			caps = append(caps, fmt.Sprintf("subagent:%s", res.Subagent))
		}
		if res.Agent != "" {
			caps = append(caps, fmt.Sprintf("agent:%s", res.Agent))
		}
		for _, t := range res.Triggers {
			caps = append(caps, fmt.Sprintf("trigger:%s", t))
		}
	case *ir.Command:
		caps = append(caps, "command")
	case *ir.Subagent:
		caps = append(caps, "subagent")
		for _, t := range res.Tools {
			caps = append(caps, fmt.Sprintf("tool:%s", t))
		}
		if res.Isolation != "" {
			caps = append(caps, fmt.Sprintf("isolation:%s", res.Isolation))
		}
		if res.Model != "" {
			caps = append(caps, fmt.Sprintf("model:%s", res.Model))
		}
	case *ir.Hook:
		caps = append(caps, "hook")
		if res.Event != "" {
			caps = append(caps, fmt.Sprintf("hook:event:%s", res.Event))
		}
		if res.Type != "" {
			caps = append(caps, fmt.Sprintf("hook:type:%s", res.Type))
		}
	case *ir.McpServer:
		caps = append(caps, "mcp")
		if res.URL != "" {
			caps = append(caps, "mcp:remote")
		}
		if res.Command != "" {
			caps = append(caps, "mcp:stdio")
		}
		if res.Transport != "" {
			caps = append(caps, fmt.Sprintf("mcp:transport:%s", res.Transport))
		}
		if len(res.Env) > 0 {
			caps = append(caps, "mcp:env")
		}
	case *ir.Collection:
		caps = append(caps, "collection")
		for _, m := range res.Members {
			caps = append(caps, fmt.Sprintf("collection:member:%s", m.Kind.String()))
		}
	}

	return uniqueSorted(caps)
}

func uniqueSorted(a []string) []string {
	if len(a) == 0 {
		return nil
	}
	sort.Strings(a)
	out := a[:1]
	for i := 1; i < len(a); i++ {
		if a[i] != out[len(out)-1] {
			out = append(out, a[i])
		}
	}
	return out
}
