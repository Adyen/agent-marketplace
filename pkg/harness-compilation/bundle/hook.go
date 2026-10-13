package bundle

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/jsonutil"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// LoadHooks reads a hooks.json file from src at the given path.
// It supports two JSON formats:
//
//  1. Claude Code workspace format (top-level event keys):
//     { "EventName": [ { "matcher": "...", "hooks": [ { "type": "command", "command": "..." } ] } ] }
//
//  2. Compilation IR format (nested under "hooks"):
//     { "hooks": { "EventName": [ { "type": "command", "command": "..." } ] } }
//
// Both formats are flattened into a flat []*ir.Hook (one per
// event+rule combination). Unsupported hook types (http, prompt,
// agent) produce a warning diagnostic attached to the returned Hook
// but do not cause an error.
func LoadHooks(src fs.FS, path string) ([]*ir.Hook, []*diag.Diagnostic, error) {
	return loadHooks(src, path, false)
}

func LoadFactoryHooksForNativeProjection(src fs.FS, path string) ([]*ir.Hook, []*diag.Diagnostic, error) {
	return loadHooks(src, path, true)
}

func loadHooks(src fs.FS, path string, factoryProjection bool) ([]*ir.Hook, []*diag.Diagnostic, error) {
	if err := validatePath(path); err != nil {
		return nil, nil, err
	}

	data, err := fs.ReadFile(src, path)
	if err != nil {
		return nil, nil, fmt.Errorf("bundle: read %s: %w", path, err)
	}

	// jsonc.Unmarshal is one pass that ignores comments and rejects duplicate
	// object keys, replacing the previous stripJSONComments-then-validate pair.
	// The contract is unchanged: a commented-out duplicate is not a duplicate,
	// and a real duplicate is rejected before any encoding/json decode.
	//
	// Parse the top-level object once so a malformed compilation-format
	// "hooks" field cannot be mistaken for a workspace-format metadata key.
	var rawWorkspace map[string]json.RawMessage
	if err := jsonc.Unmarshal(path, data, &rawWorkspace); err != nil {
		return nil, nil, fmt.Errorf("bundle: parse: %w", err)
	}
	wrapped := false
	if rawHooks, exists := rawWorkspace["hooks"]; exists {
		var hooks map[string]json.RawMessage
		if err := json.Unmarshal(rawHooks, &hooks); err != nil || hooks == nil {
			if err == nil {
				err = fmt.Errorf("value is null")
			}
			return nil, nil, fmt.Errorf("bundle: parse %s: top-level hooks must be an object of event arrays: %w", path, err)
		}
		rawWorkspace = hooks
		wrapped = true
	}

	// Fall back to the Claude Code workspace format (top-level event keys).
	// Use RawMessage to handle non-event keys (e.g. "claudeHooksImported": true)
	// that can't unmarshal into []claudeHookGroup.

	// Convert workspace format groups into the flat rule map.
	rulesByEvent := make(map[string][]hookRule)
	for event, raw := range rawWorkspace {
		var groups []claudeHookGroup
		if err := json.Unmarshal(raw, &groups); err != nil {
			if _, coded := diag.AsCodedError(err); coded {
				return nil, nil, err
			}
			if wrapped {
				return nil, nil, fmt.Errorf("bundle: parse %s: hook event %q must contain valid hook groups: %w", path, event, err)
			}
			// Not an event array; skip non-event keys like
			// "claudeHooksImported", "showHookOutput", etc.
			continue
		}
		if wrapped && groups == nil {
			return nil, nil, fmt.Errorf("bundle: parse %s: hook event %q must contain valid hook groups: value is null", path, event)
		}
		// Skip empty event arrays.
		if len(groups) == 0 {
			continue
		}
		var flatRules []hookRule
		for _, group := range groups {
			groupCommandRegex := ""
			hasGroupCommandRegex := false
			if factoryProjection && group.hooksPresent {
				if group.flatExecutionFieldsPresent {
					return nil, nil, fmt.Errorf("bundle: parse %s: Factory hook group cannot combine nested hooks with flat execution fields", path)
				}
				var err error
				groupCommandRegex, hasGroupCommandRegex, err = factoryNativeGroupCommandRegex(group.ExtraFields)
				if err != nil {
					return nil, nil, fmt.Errorf("bundle: parse %s: %w", path, err)
				}
			}
			if len(group.Hooks) > 0 {
				for _, h := range group.Hooks {
					if factoryProjection && h.matcherPresent {
						return nil, nil, fmt.Errorf("bundle: parse %s: Factory nested handler matcher must be declared on its group", path)
					}
					extraFields := h.ExtraFields
					if factoryProjection {
						var err error
						extraFields, err = factoryNativeHookExtraFields(h.ExtraFields, groupCommandRegex, hasGroupCommandRegex, true)
						if err != nil {
							return nil, nil, fmt.Errorf("bundle: parse %s: %w", path, err)
						}
					}
					r := hookRule{
						Type:        h.Type,
						Command:     h.Command,
						Prompt:      h.Prompt,
						Timeout:     h.Timeout,
						ExtraFields: extraFields,
					}
					if group.Matcher != "" {
						r.Matcher = group.Matcher
					}
					flatRules = append(flatRules, r)
				}
			} else if group.Type != "" || group.Command != "" {
				// Flat rule without nesting.
				extraFields := group.ExtraFields
				if factoryProjection {
					var err error
					extraFields, err = factoryNativeHookExtraFields(group.ExtraFields, "", false, false)
					if err != nil {
						return nil, nil, fmt.Errorf("bundle: parse %s: %w", path, err)
					}
				}
				r := hookRule{
					Type:        group.Type,
					Command:     group.Command,
					Prompt:      group.Prompt,
					Timeout:     group.Timeout,
					Matcher:     group.Matcher,
					ExtraFields: extraFields,
				}
				flatRules = append(flatRules, r)
			}
		}
		if len(flatRules) > 0 {
			rulesByEvent[event] = flatRules
		}
	}

	if len(rulesByEvent) == 0 {
		return nil, nil, nil
	}

	return flattenHookRules(rulesByEvent, path)
}

// flattenHookRules converts a map of event→rules into a flat hook slice.
func flattenHookRules(rulesByEvent map[string][]hookRule, path string) ([]*ir.Hook, []*diag.Diagnostic, error) {
	var hooks []*ir.Hook
	var diags []*diag.Diagnostic

	events := make([]string, 0, len(rulesByEvent))
	for event := range rulesByEvent {
		events = append(events, event)
	}
	sort.Strings(events)

	// Hook names become catalog resource keys, so they must be unique across
	// every generated hook: a suffixed name can otherwise collide with the
	// unsuffixed base name of a differently-typed hook for the same event.
	nameCounts := make(map[string]int)
	usedNames := make(map[string]struct{})
	for _, event := range events {
		rules := rulesByEvent[event]
		for _, rule := range rules {
			hookType := rule.Type
			if hookType == "" {
				hookType = "command" // default type
			}

			extra := make(map[string]any)
			for k, v := range rule.ExtraFields {
				extra[k] = v
			}

			source := ir.SourceRef{File: path, Line: 0}
			hookNameBase := event + "-" + hookType
			seen := nameCounts[hookNameBase]
			hookName := hookNameBase
			for {
				if seen > 0 {
					hookName = fmt.Sprintf("%s-%d", hookNameBase, seen+1)
				}
				if _, taken := usedNames[hookName]; !taken {
					break
				}
				seen++
			}
			nameCounts[hookNameBase] = seen + 1
			usedNames[hookName] = struct{}{}

			var h *ir.Hook
			options := []ir.HookOption{ir.WithHookPrompt(rule.Prompt)}
			if rule.Timeout != nil {
				options = append(options, ir.WithHookTimeoutPtr(rule.Timeout))
			}
			if rule.Matcher != "" {
				h = ir.NewHookWithMatcherOptions(hookName, event, rule.Matcher, rule.Command, hookType, extra, source, options...)
			} else {
				h = ir.NewHookWithOptions(hookName, event, rule.Command, hookType, extra, source, options...)
			}

			// Unsupported hook types get a warning diagnostic.
			if hookType != "command" {
				diags = append(diags, &diag.Diagnostic{
					Code:     diag.CodeHookClaudeUnsupportedType,
					Severity: diag.SeverityWarning,
					Resource: hookName,
					Message:  fmt.Sprintf("unsupported hook type %q for event %q; only command-type hooks are universally supported in v0", hookType, event),
					Hint:     "Convert the hook to a command type or remove it",
					Source:   diag.SourceRef{File: path, Line: 0},
				})
			}

			hooks = append(hooks, h)
		}
	}

	return hooks, diags, nil
}

func factoryNativeGroupCommandRegex(extra map[string]any) (string, bool, error) {
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var commandRegex string
	found := false
	for _, key := range keys {
		if !strings.EqualFold(key, "commandRegex") {
			return "", false, fmt.Errorf("factory hook group field %q is not representable", key)
		}
		if key != "commandRegex" || found {
			return "", false, fmt.Errorf("factory hook group has a case-colliding commandRegex alias %q", key)
		}
		value, ok := extra[key].(string)
		if !ok {
			return "", false, fmt.Errorf("factory hook group commandRegex must be a string")
		}
		commandRegex = value
		found = true
	}
	return commandRegex, found, nil
}

func factoryNativeHookExtraFields(source map[string]any, groupCommandRegex string, hasGroupCommandRegex, nestedHandler bool) (map[string]any, error) {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := []string{"type", "command", "prompt", "timeout", "matcher", "commandRegex"}
	for _, key := range keys {
		for _, field := range fields {
			if !strings.EqualFold(key, field) {
				continue
			}
			if key != field {
				return nil, fmt.Errorf("factory hook extra field %q aliases typed field %q", key, field)
			}
			if field == "commandRegex" {
				if nestedHandler {
					if hasGroupCommandRegex {
						return nil, fmt.Errorf("factory hook handler commandRegex conflicts with group commandRegex")
					}
					return nil, fmt.Errorf("factory nested handler commandRegex requires the group-level field")
				}
				if _, ok := source[key].(string); !ok {
					return nil, fmt.Errorf("factory flat commandRegex must be a string")
				}
			}
		}
	}
	extra := make(map[string]any, len(source)+1)
	for key, value := range source {
		extra[key] = value
	}
	if hasGroupCommandRegex {
		extra["commandRegex"] = groupCommandRegex
	}
	return extra, nil
}

// claudeHookGroup represents a single entry in the Claude Code workspace
// hooks.json format: an optional matcher plus an array of hook handlers.
type claudeHookGroup struct {
	Matcher                    string         `json:"matcher,omitempty"`
	Hooks                      []hookRule     `json:"hooks"`
	Type                       string         `json:"type,omitempty"`    // flat rule fallback
	Command                    string         `json:"command,omitempty"` // flat rule fallback
	Prompt                     string         `json:"prompt,omitempty"`
	Timeout                    *int           `json:"timeout,omitempty"`
	ExtraFields                map[string]any `json:"-"`
	hooksPresent               bool           `json:"-"`
	flatExecutionFieldsPresent bool           `json:"-"`
}

// UnmarshalJSON implements custom unmarshalling to capture extra fields.
func (g *claudeHookGroup) UnmarshalJSON(data []byte) error {
	*g = claudeHookGroup{}
	if err := jsonutil.ValidateFieldAliases("", data, g); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if v, ok := raw["matcher"]; ok {
		if err := json.Unmarshal(v, &g.Matcher); err != nil {
			return fmt.Errorf("hook group matcher: %w", err)
		}
		delete(raw, "matcher")
	}
	if v, ok := raw["hooks"]; ok {
		g.hooksPresent = true
		if err := json.Unmarshal(v, &g.Hooks); err != nil {
			return fmt.Errorf("hook group hooks: %w", err)
		}
		if g.Hooks == nil {
			return fmt.Errorf("hook group hooks must be an array")
		}
		delete(raw, "hooks")
	}
	if v, ok := raw["type"]; ok {
		g.flatExecutionFieldsPresent = true
		if err := json.Unmarshal(v, &g.Type); err != nil {
			return fmt.Errorf("hook group type: %w", err)
		}
		delete(raw, "type")
	}
	if v, ok := raw["command"]; ok {
		g.flatExecutionFieldsPresent = true
		if err := json.Unmarshal(v, &g.Command); err != nil {
			return fmt.Errorf("hook group command: %w", err)
		}
		delete(raw, "command")
	}
	if v, ok := raw["prompt"]; ok {
		g.flatExecutionFieldsPresent = true
		if err := json.Unmarshal(v, &g.Prompt); err != nil {
			return fmt.Errorf("hook group prompt: %w", err)
		}
		delete(raw, "prompt")
	}
	if v, ok := raw["timeout"]; ok {
		g.flatExecutionFieldsPresent = true
		if err := json.Unmarshal(v, &g.Timeout); err != nil {
			return fmt.Errorf("hook group timeout: %w", err)
		}
		delete(raw, "timeout")
	}

	if len(raw) > 0 {
		g.ExtraFields = make(map[string]any, len(raw))
		for k, v := range raw {
			var val any
			if err := json.Unmarshal(v, &val); err != nil {
				g.ExtraFields[k] = string(v)
			} else {
				g.ExtraFields[k] = val
			}
		}
	}

	return nil
}

// hookRule represents a single hook rule in the hooks.json structure.
type hookRule struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Prompt  string `json:"prompt,omitempty"`
	Timeout *int   `json:"timeout,omitempty"`
	Matcher string `json:"matcher,omitempty"`
	// ExtraFields captures any additional JSON fields via the
	// remaining key-value pairs after standard deserialization.
	ExtraFields    map[string]any `json:"-"`
	matcherPresent bool           `json:"-"`
}

// UnmarshalJSON implements custom unmarshalling to capture extra fields.
func (r *hookRule) UnmarshalJSON(data []byte) error {
	*r = hookRule{}
	if err := jsonutil.ValidateFieldAliases("", data, r); err != nil {
		return err
	}
	// First, unmarshal known fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if v, ok := raw["type"]; ok {
		if err := json.Unmarshal(v, &r.Type); err != nil {
			return fmt.Errorf("hook rule type: %w", err)
		}
		delete(raw, "type")
	}
	if v, ok := raw["command"]; ok {
		if err := json.Unmarshal(v, &r.Command); err != nil {
			return fmt.Errorf("hook rule command: %w", err)
		}
		delete(raw, "command")
	}
	if v, ok := raw["prompt"]; ok {
		if err := json.Unmarshal(v, &r.Prompt); err != nil {
			return fmt.Errorf("hook rule prompt: %w", err)
		}
		delete(raw, "prompt")
	}
	if v, ok := raw["timeout"]; ok {
		if err := json.Unmarshal(v, &r.Timeout); err != nil {
			return fmt.Errorf("hook rule timeout: %w", err)
		}
		delete(raw, "timeout")
	}
	if v, ok := raw["matcher"]; ok {
		r.matcherPresent = true
		if err := json.Unmarshal(v, &r.Matcher); err != nil {
			return fmt.Errorf("hook rule matcher: %w", err)
		}
		delete(raw, "matcher")
	}

	// Collect remaining fields.
	if len(raw) > 0 {
		r.ExtraFields = make(map[string]any, len(raw))
		for k, v := range raw {
			var val any
			if err := json.Unmarshal(v, &val); err != nil {
				// Store raw string if we can't decode.
				r.ExtraFields[k] = string(v)
			} else {
				r.ExtraFields[k] = val
			}
		}
	}

	return nil
}
