package sourcerevision

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// legacyHookToolProjection replays the v1-v4 (pre-provenance) ingester view of
// hook and tool fields for the catalog-fallback digest path. The historical
// ingesters canonicalized native hook events, implied matchers, command env
// tokens, and tool names at ingestion and never recorded HookSource or
// ToolSource, so frozen digests are computed over the projected resource. The
// projection is digest-only: it clones rather than mutates the ingested
// resource.
func legacyHookToolProjection(resource ir.Resource) ir.Resource {
	switch value := resource.(type) {
	case *ir.Hook:
		return legacyHookProjection(value)
	case *ir.Subagent:
		if value.ToolSource == "" {
			return resource
		}
		copy := *value
		copy.Tools = legacyToolNames(value.ToolSource, value.Tools)
		copy.ToolSource = ""
		return &copy
	case *ir.Skill:
		if value.ToolSource == "" {
			return resource
		}
		copy := *value
		copy.AllowedTools = legacyToolNames(value.ToolSource, value.AllowedTools)
		copy.ToolSource = ""
		return &copy
	case *ir.Command:
		if value.ToolSource == "" {
			return resource
		}
		copy := *value
		copy.ToolSource = ""
		return &copy
	default:
		return resource
	}
}

// legacyToolNames replays BASE's ReverseTool canonicalization for the one
// source whose tool vocabulary differed from the canonical names: Devin's
// closed allowed-tools set. Every other source (cursor, factory, gemini,
// claude) kept authored names verbatim — BASE either reversed identity cells
// or preserved unknown names, which is what the historical digests contain.
func legacyToolNames(source string, tools []string) []string {
	if source != string(target.TargetDevin) || len(tools) == 0 {
		return tools
	}
	out := make([]string, len(tools))
	for i, name := range tools {
		switch name {
		case "read":
			out[i] = "Read"
		case "edit":
			out[i] = "Edit"
		case "exec":
			out[i] = "Bash"
		case "glob":
			out[i] = "Glob"
		case "grep":
			out[i] = "Grep"
		default:
			out[i] = name
		}
	}
	return out
}

// legacyHookProjection returns the digest clone of h as the v1-v4 ingesters
// would have produced it.
func legacyHookProjection(h *ir.Hook) *ir.Hook {
	if h.HookSource == "" && len(h.GroupExtraKeys) == 0 {
		return h
	}
	cp := *h
	cp.HookSource = ""
	if h.Extra != nil || len(h.GroupExtraKeys) > 0 {
		// BASE bundle loaders never promoted group-level fields into the
		// handler's Extra, so subtract exactly the promoted keys. The map
		// stays non-nil: the historical loader always allocated it.
		extra := make(map[string]any, len(h.Extra))
		for key, value := range h.Extra {
			if h.GroupExtraKeys[key] {
				continue
			}
			extra[key] = value
		}
		cp.Extra = extra
	}

	source := target.Target(h.HookSource)
	canonical, matcher, known := hookpolicy.LegacyEvent(source, h.Event)

	switch source {
	case target.TargetFactory:
		cp.Command = strings.ReplaceAll(cp.Command, "${DROID_PLUGIN_ROOT}", "${CLAUDE_PLUGIN_ROOT}")
	case target.TargetCursor:
		cp.Command = strings.ReplaceAll(cp.Command, "${CURSOR_PLUGIN_ROOT}", "${CLAUDE_PLUGIN_ROOT}")
	case target.TargetGemini:
		replacer := strings.NewReplacer(
			"${extensionPath}", "${CLAUDE_PLUGIN_ROOT}",
			"${GEMINI_PROJECT_DIR}", "${CLAUDE_PLUGIN_ROOT}",
			"${CLAUDE_PROJECT_DIR}", "${CLAUDE_PLUGIN_ROOT}",
		)
		cp.Command = replacer.Replace(cp.Command)
		if known {
			cp.Event = canonical
		} else {
			// BASE recorded the unmappable Gemini event under gemini_event.
			if cp.Extra == nil {
				cp.Extra = map[string]any{}
			}
			cp.Extra["gemini_event"] = h.Event
		}
	case target.TargetWindsurf:
		var sidecar *ext.Windsurf
		if cp.Ext.Windsurf != nil {
			clone := *cp.Ext.Windsurf
			sidecar = &clone
		}
		if known {
			cp.Event = canonical
			cp.Matcher = matcher
		} else {
			// BASE kept the native event as the canonical placeholder and
			// preserved it in the Windsurf sidecar.
			if sidecar == nil {
				sidecar = &ext.Windsurf{}
			}
			sidecar.OriginalHookEvent = h.Event
		}
		cp.Ext.Windsurf = sidecar
	case target.TargetCodex:
		if known {
			cp.Event = canonical
		}
		// BASE capped SessionEnd execution synchronously at three seconds.
		if cp.Ext.Codex != nil {
			clone := *cp.Ext.Codex
			cp.Ext.Codex = &clone
		}
		if cp.Event == "SessionEnd" {
			if cp.Ext.Codex != nil && cp.Ext.Codex.HookAsync != nil && *cp.Ext.Codex.HookAsync {
				cp.Ext.Codex.HookAsync = nil
			}
			if cp.Timeout != nil && *cp.Timeout > 3 {
				timeout := 3
				cp.Timeout = &timeout
			}
		}
	default:
		// Devin and the Claude-lineage sources keep the authored event and
		// matcher verbatim; only the provenance marker is projected away.
		if known {
			cp.Event = canonical
		}
	}
	return &cp
}
