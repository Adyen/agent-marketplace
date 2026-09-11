package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const geminiSettingsPath = ".gemini/settings.json"

// translateHook translates a Hook IR resource into a .gemini/settings.json
// fragment containing a hooks section keyed by Gemini event names.
func translateHook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	// Map the Claude event name to the Gemini event name using the shared table.
	geminiEvent, ok := mapping.HookEvent(h.Event, h.Matcher, mapping.TargetGemini)
	if !ok {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookGeminiEventUnmappable, Severity: diag.SeverityWarning, Target: string(mapping.TargetGemini), Resource: h.Name(), Message: "Gemini has no equivalent hook event; hook artifact suppressed", Hint: "Use a documented Gemini hook event", Source: diag.SourceRef(h.Source)}}, nil
	}

	// Prompt hooks are natively representable; unsupported types are rejected
	// rather than fabricated as empty commands.
	if h.Type != "command" && h.Type != "prompt" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookGeminiUnsupportedType, Severity: diag.SeverityWarning, Target: string(mapping.TargetGemini), Resource: h.Name(), Message: "Gemini cannot represent this hook type; hook artifact suppressed", Hint: "Use a command or prompt hook on Gemini", Source: diag.SourceRef(h.Source)}}, nil
	}

	// Replace env tokens in the command string.
	mappedCommand := replaceEnvTokens(h.Command)

	// Build the hook entry without inventing command for prompt hooks.
	rule := map[string]any{"type": h.Type}
	group := make(map[string]any)
	if h.Matcher != "" {
		group["matcher"] = h.Matcher
	}
	if mappedCommand != "" {
		rule["command"] = mappedCommand
	}
	if h.Prompt != "" {
		rule["prompt"] = h.Prompt
	}
	if h.Timeout != nil {
		rule["timeout"] = *h.Timeout
	}
	if sidecar := h.Ext.Gemini; sidecar != nil {
		if sidecar.HookName != "" {
			rule["name"] = sidecar.HookName
		}
		if sidecar.HookTimeout != nil {
			rule["timeout"] = *sidecar.HookTimeout
		}
	}
	// The canonical set is exactly what this function already wrote into
	// `rule`, plus `event`, which is the enclosing map key rather than a
	// member. Deriving it from `rule` keeps the conflict report honest when a
	// sidecar contributes `name` or `timeout`.
	canonical := make(map[string]bool, len(rule)+1)
	for key := range rule {
		canonical[key] = true
	}
	canonical["event"] = true
	extraKeys, diags := compile.ExtraKeys(h.Extra, compile.PresentFields(canonical), mapping.TargetGemini, h.Name(), diag.SourceRef(h.Source))
	for _, k := range extraKeys {
		rule[k] = h.Extra[k]
	}
	group["hooks"] = []any{rule}
	if sidecar := h.Ext.Gemini; sidecar != nil {
		keys, groupDiags := compile.ExtraKeys(sidecar.HookGroupExtra, compile.PresentFields(map[string]bool{"hooks": true, "matcher": h.Matcher != ""}), mapping.TargetGemini, h.Name(), diag.SourceRef(h.Source))
		diags = append(diags, groupDiags...)
		for _, key := range keys {
			group[key] = sidecar.HookGroupExtra[key]
		}
	}

	// Wrap in the settings.json envelope.
	fragment := map[string]any{
		"hooks": map[string]any{
			geminiEvent: []any{group},
		},
	}

	fragBytes, err := json.MarshalIndent(fragment, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: marshal settings.json: %w", err)
	}

	art := artifact.Artifact{
		Path:        geminiSettingsPath,
		Bytes:       fragBytes,
		Fragment:    true,
		FragmentKey: geminiSettingsPath,
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""

	return []artifact.Artifact{art}, diags, nil
}
