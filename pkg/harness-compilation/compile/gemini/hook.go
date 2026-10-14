package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const geminiSettingsPath = ".gemini/settings.json"

// translateHook translates a Hook IR resource into a .gemini/settings.json
// fragment containing a hooks section keyed by Gemini event names. Event,
// matcher, timeout, handler type, and command come from the catalog-driven
// hookpolicy resolver (OpenSpec native-hook-integration, task 3.2).
func translateHook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	scope := hookpolicy.ScopeRepository
	if opts.Scope() == compile.ScopeUser {
		scope = hookpolicy.ScopeMachine
	}
	resolved := hookpolicy.Resolve(h, mapping.TargetGemini, scope, opts.PluginRoot())
	if resolved.Failed() {
		return nil, resolved.Diagnostics, nil
	}

	// Build the hook entry without inventing command for prompt hooks.
	rule := map[string]any{"type": resolved.Type}
	group := make(map[string]any)
	if resolved.Matcher != "" {
		group["matcher"] = resolved.Matcher
	}
	if resolved.Command != "" {
		rule["command"] = resolved.Command
	}
	if h.Prompt != "" {
		rule["prompt"] = h.Prompt
	}
	if resolved.Timeout != nil {
		rule["timeout"] = *resolved.Timeout
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
	extraKeys, diags := compile.HookExtraKeys(h.Extra, compile.PresentFields(canonical), mapping.TargetGemini, h.Name(), diag.SourceRef(h.Source))
	for _, k := range extraKeys {
		rule[k] = h.Extra[k]
	}
	group["hooks"] = []any{rule}
	if sidecar := h.Ext.Gemini; sidecar != nil {
		keys, groupDiags := compile.HookExtraKeys(sidecar.HookGroupExtra, compile.PresentFields(map[string]bool{"hooks": true, "matcher": resolved.Matcher != ""}), mapping.TargetGemini, h.Name(), diag.SourceRef(h.Source))
		diags = append(diags, groupDiags...)
		for _, key := range keys {
			group[key] = sidecar.HookGroupExtra[key]
		}
	}

	// Wrap in the settings.json envelope.
	fragment := map[string]any{
		"hooks": map[string]any{
			resolved.Event: []any{group},
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

	return []artifact.Artifact{art}, append(resolved.Diagnostics, diags...), nil
}
