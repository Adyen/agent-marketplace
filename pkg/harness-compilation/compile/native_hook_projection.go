package compile

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// NativeHookProjection compiles one hook into the target's declared native
// hook file fragment. Event, matcher, type, timeout and command translation
// come from the shared catalog-driven hookpolicy resolver; the entry encoding
// and envelope come from the target's declared HookProjection topology. The
// resulting fragment reconciles externally at private mode 0600 and carries
// no ownership marker. The function is pure: it performs no I/O and does not
// change what the default Compile path emits.
func NativeHookProjection(h *ir.Hook, dst Target, scope Scope, supportBase string) (*Result, error) {
	if h == nil {
		return nil, fmt.Errorf("native hook projection requires a hook resource")
	}
	definition, ok := targetmeta.Lookup(string(dst))
	if !ok {
		return nil, fmt.Errorf("target %q is not declared", dst)
	}
	if definition.HookProjection == nil {
		return nil, fmt.Errorf("target %q declares no native hook projection", dst)
	}
	machineScope := false
	switch scope {
	case "", ScopeProject:
	case ScopeUser:
		machineScope = true
	default:
		return nil, fmt.Errorf("invalid native hook scope %q", scope)
	}
	if err := validateNativeSupportBase(supportBase, machineScope); err != nil {
		return nil, err
	}
	artifacts, diags, err := GuardNativeCodexTranslation(h, dst, func(resource ir.Resource) ([]artifact.Artifact, []diag.Diagnostic, error) {
		hook, ok := resource.(*ir.Hook)
		if !ok {
			return nil, nil, fmt.Errorf("native hook projection requires a hook resource")
		}
		return projectNativeHook(hook, dst, definition.HookProjection, machineScope, supportBase)
	})
	if err != nil {
		return nil, err
	}
	return &Result{Artifacts: artifacts, Diagnostics: diags}, nil
}

func validateNativeSupportBase(supportBase string, machineScope bool) error {
	if supportBase == "" {
		return nil
	}
	if strings.ContainsAny(supportBase, "\\\x00") {
		return fmt.Errorf("invalid native hook support base %q", supportBase)
	}
	if machineScope {
		if !filepath.IsAbs(supportBase) || filepath.Clean(supportBase) != supportBase {
			return fmt.Errorf("machine-scope native hook support base %q must be a clean absolute path", supportBase)
		}
		return nil
	}
	if !fs.ValidPath(supportBase) || supportBase == "." {
		return fmt.Errorf("repository-scope native hook support base %q must be a clean relative slash path", supportBase)
	}
	return nil
}

func projectNativeHook(h *ir.Hook, dst Target, projection *targetmeta.NativeHookProjection, machineScope bool, supportBase string) ([]artifact.Artifact, []diag.Diagnostic, error) {
	resolveHook := h
	if dst == TargetWindsurf && h.HookSource == "" && h.Ext.Windsurf != nil && h.Ext.Windsurf.OriginalHookEvent != "" {
		clone := *h
		clone.HookSource = string(targetmeta.TargetWindsurf)
		clone.Event = h.Ext.Windsurf.OriginalHookEvent
		resolveHook = &clone
	}

	hpScope := hookpolicy.ScopeRepository
	if machineScope {
		hpScope = hookpolicy.ScopeMachine
	}
	if diags, refuse := nativeHookPrechecks(h, dst); refuse {
		return nil, diags, nil
	}
	resolved := hookpolicy.Resolve(resolveHook, targetmeta.Target(dst), hpScope, supportBase)
	if resolved.Failed() {
		return nil, resolved.Diagnostics, nil
	}

	rule, diags, emit := nativeHookRule(h, dst, resolved)
	diags = append(resolved.Diagnostics, diags...)
	if !emit {
		return nil, diags, nil
	}
	projected, err := nativeProjectionArtifact(projection, machineScope, resolved.Event, rule)
	if err != nil {
		return nil, nil, fmt.Errorf("target %q native hook entry: %w", dst, err)
	}
	return []artifact.Artifact{projected}, diags, nil
}

func nativeProjectionArtifact(projection *targetmeta.NativeHookProjection, machineScope bool, event string, rule any) (artifact.Artifact, error) {
	entry, err := nativeProjectionEntry(projection.Shape, rule)
	if err != nil {
		return artifact.Artifact{}, err
	}
	eventMap, err := json.Marshal(map[string][]json.RawMessage{event: {entry}})
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("encode native hook event: %w", err)
	}
	file := projection.File(machineScope)
	document := make(map[string]json.RawMessage, len(file.Envelope)+1)
	for key, value := range file.Envelope {
		raw, err := json.Marshal(value)
		if err != nil {
			return artifact.Artifact{}, fmt.Errorf("encode native hook envelope: %w", err)
		}
		document[key] = raw
	}
	if file.Member != "" {
		if _, exists := document[file.Member]; exists {
			return artifact.Artifact{}, fmt.Errorf("native hook member %q conflicts with its envelope", file.Member)
		}
		document[file.Member] = eventMap
	} else {
		var events map[string]json.RawMessage
		if err := json.Unmarshal(eventMap, &events); err != nil {
			return artifact.Artifact{}, fmt.Errorf("decode native hook event map: %w", err)
		}
		for key, value := range events {
			if _, exists := document[key]; exists {
				return artifact.Artifact{}, fmt.Errorf("native hook event %q conflicts with its envelope", key)
			}
			document[key] = value
		}
	}
	data, err := json.Marshal(document)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("encode native hook artifact: %w", err)
	}
	return artifact.Artifact{
		Path:                           file.Path,
		Bytes:                          data,
		Mode:                           0o600,
		Fragment:                       true,
		FragmentKey:                    file.Path,
		Reconciliation:                 artifact.ReconciliationPolicy{Mode: artifact.ReconciliationExternal},
		ExternalReconciliationRequired: true,
	}, nil
}

func nativeHookPrechecks(h *ir.Hook, dst Target) ([]diag.Diagnostic, bool) {
	if dst != TargetCodex {
		return nil, false
	}
	source := DiagSource(h.Source)
	if h.Ext.Codex != nil && h.Ext.Codex.AdditionalContextLimit != nil && *h.Ext.Codex.AdditionalContextLimit < 0 {
		return []diag.Diagnostic{{Code: diag.CodeHookCodexUnsupported, Severity: diag.SeverityError, Target: string(dst), Resource: h.Name(), Message: "Codex hook has an invalid additional context limit; hook suppressed", Source: source}}, true
	}
	if h.HookSource == string(targetmeta.TargetCodex) {
		if !hookpolicy.IsDeclaredEvent(targetmeta.TargetCodex, h.Event) ||
			(h.Matcher != "" && hookpolicy.NativeEventMatcherKind(targetmeta.TargetCodex, h.Event) == hookpolicy.MatcherKindNone) {
			return []diag.Diagnostic{{Code: diag.CodeHookCodexUnsupported, Severity: diag.SeverityError, Target: string(dst), Resource: h.Name(), Message: "Codex does not execute this hook event or handler; hook suppressed", Source: source}}, true
		}
	}
	return nil, false
}

func nativeHookRule(h *ir.Hook, dst Target, resolved hookpolicy.Resolved) (rule map[string]any, diags []diag.Diagnostic, emit bool) {
	name, source := h.Name(), DiagSource(h.Source)
	canonical := PresentFields(map[string]bool{
		"event":   true,
		"matcher": resolved.Matcher != "",
		"type":    true,
		"command": resolved.Command != "",
		"prompt":  h.Prompt != "",
		"timeout": resolved.Timeout != nil,
	})
	switch dst {
	case TargetClaude:
		if sidecar := h.Ext.Claude; sidecar != nil && sidecar.HookType != "" && sidecar.HookType != resolved.Type {
			return nil, []diag.Diagnostic{{Code: diag.CodeHookClaudeUnsupportedType, Severity: diag.SeverityError, Target: string(dst), Resource: name, Message: "A preserved Claude hook type contradicts the catalog-resolved type; hook suppressed", Hint: "Remove the preserved sidecar or align it with the declared hook type", Source: source}}, false
		}
		rule = map[string]any{"type": resolved.Type}
		if resolved.Command != "" {
			rule["command"] = resolved.Command
		}
		if resolved.Matcher != "" {
			rule["matcher"] = resolved.Matcher
		}
		if h.Prompt != "" {
			rule["prompt"] = h.Prompt
		}
		if resolved.Timeout != nil {
			rule["timeout"] = *resolved.Timeout
		}
	case TargetFactory:
		if h.Prompt != "" {
			return nil, []diag.Diagnostic{{Code: diag.CodeHookPromptUnsupported, Severity: diag.SeverityError, Target: string(dst), Resource: name, Message: "Factory cannot represent prompt hooks; hook artifact suppressed", Hint: "Use a command hook on Factory", Source: source}}, false
		}
		handler := map[string]any{"type": resolved.Type}
		if resolved.Command != "" {
			handler["command"] = resolved.Command
		}
		if resolved.Timeout != nil {
			handler["timeout"] = *resolved.Timeout
		}
		rule = map[string]any{"hooks": []any{handler}}
		if resolved.Matcher != "" {
			rule["matcher"] = resolved.Matcher
		}
		extraKeys, extraDiags := HookExtraKeys(h.Extra, canonical, dst, name, source)
		diags = append(diags, extraDiags...)
		for _, k := range extraKeys {
			if k == "commandRegex" {
				rule[k] = h.Extra[k]
				continue
			}
			handler[k] = h.Extra[k]
		}
		return rule, diags, true
	case TargetCursor:
		if h.Prompt != "" && resolved.Type != "prompt" {
			return nil, []diag.Diagnostic{{Code: diag.CodeHookPromptUnsupported, Severity: diag.SeverityError, Target: string(dst), Resource: name, Message: "Cursor cannot represent a command hook carrying a prompt payload; hook artifact suppressed", Hint: "Use a prompt hook or remove the prompt payload", Source: source}}, false
		}
		if sidecar := h.Ext.Cursor; sidecar != nil && sidecar.HookType != "" && sidecar.HookType != resolved.Type {
			return nil, []diag.Diagnostic{{Code: diag.CodeHookCursorUnsupportedType, Severity: diag.SeverityError, Target: string(dst), Resource: name, Message: "A preserved Cursor hook type contradicts the catalog-resolved type; hook suppressed", Hint: "Remove the preserved sidecar or align it with the declared hook type", Source: source}}, false
		}
		rule = map[string]any{"event": resolved.Event, "type": resolved.Type}
		if resolved.Type == "command" && resolved.Command != "" {
			rule["command"] = resolved.Command
		}
		if resolved.Type == "prompt" && h.Prompt != "" {
			rule["prompt"] = h.Prompt
		}
		if resolved.Matcher != "" {
			rule["matcher"] = resolved.Matcher
		}
		if resolved.Timeout != nil {
			rule["timeout"] = *resolved.Timeout
		}
	case TargetWindsurf:
		return windsurfNativeHookRule(h, resolved)
	case TargetDevin:
		payloadKey, payload, payloadDiags := devinHookPayload(h, name, source, resolved.Command)
		diags = append(diags, payloadDiags...)
		if payload == "" {
			return nil, diags, false
		}
		rule = map[string]any{"type": resolved.Type, payloadKey: payload}
		if resolved.Matcher != "" {
			rule["matcher"] = resolved.Matcher
		}
		if resolved.Timeout != nil {
			rule["timeout"] = *resolved.Timeout
		}
		canonical["hooks"] = struct{}{}
	case TargetCodex:
		return codexNativeHookRule(h, resolved)
	case TargetGemini:
		rule = map[string]any{"type": resolved.Type}
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
			if sidecar.HookTimeout != nil && (resolved.Timeout == nil || *sidecar.HookTimeout != *resolved.Timeout) {
				return nil, []diag.Diagnostic{{Code: diag.CodeHookTimeoutUnsupported, Severity: diag.SeverityError, Target: string(dst), Resource: name, Message: "A preserved Gemini hook timeout contradicts the catalog-resolved timeout; hook suppressed", Hint: "Remove the preserved sidecar or align it with the declared timeout", Source: source}}, false
			}
		}
		canonical = make(map[string]struct{}, len(rule)+1)
		for key := range rule {
			canonical[key] = struct{}{}
		}
		canonical["event"] = struct{}{}
		return geminiNativeHookRule(h, dst, name, source, rule, canonical, resolved.Matcher)
	default:
		return nil, []diag.Diagnostic{{Code: diag.CodeHookTypeUnsupported, Severity: diag.SeverityError, Target: string(dst), Resource: name, Message: "Target has no hook rule serialization; hook suppressed", Source: source}}, false
	}
	extraKeys, extraDiags := HookExtraKeys(h.Extra, canonical, dst, name, source)
	diags = append(diags, extraDiags...)
	for _, k := range extraKeys {
		rule[k] = h.Extra[k]
	}
	return rule, diags, true
}

func windsurfNativeHookRule(h *ir.Hook, resolved hookpolicy.Resolved) (map[string]any, []diag.Diagnostic, bool) {
	name, source := h.Name(), DiagSource(h.Source)
	if h.Type == "prompt" || h.Prompt != "" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookPromptUnsupported, Severity: diag.SeverityError, Target: string(TargetWindsurf), Resource: name, Message: "Windsurf cannot represent prompt hooks; hook artifact suppressed", Hint: "Use a command hook on Windsurf", Source: source}}, false
	}
	if h.Type != "command" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookWindsurfUnsupportedType, Severity: diag.SeverityError, Target: string(TargetWindsurf), Resource: name, Message: "Windsurf cannot represent this hook type; hook artifact suppressed", Hint: "Use a command hook on Windsurf", Source: source}}, false
	}
	unsupported := func() []diag.Diagnostic {
		return []diag.Diagnostic{{Code: diag.CodeHookWindsurfNativeFieldsUnsupported, Severity: diag.SeverityError, Target: string(TargetWindsurf), Resource: name, Message: "Windsurf cannot preserve conflicting native hook fields; hook artifact suppressed", Hint: "Remove the conflicting native fields explicitly or use a target that represents them", Source: source}}
	}
	extra := maps.Clone(h.Extra)
	for _, field := range []struct {
		name string
		want string
	}{
		{name: "event", want: h.Event},
		{name: "type", want: h.Type},
		{name: "matcher", want: h.Matcher},
	} {
		if raw, exists := extra[field.name]; exists {
			value, ok := raw.(string)
			if !ok || value != field.want {
				return nil, unsupported(), false
			}
			delete(extra, field.name)
		}
	}
	if _, exists := extra["timeout"]; exists {
		return nil, unsupported(), false
	}
	if sidecar := h.Ext.Windsurf; sidecar != nil && sidecar.HookWorkingDir != "" {
		if raw, exists := extra["working_directory"]; exists {
			value, ok := raw.(string)
			if !ok || value != sidecar.HookWorkingDir {
				return nil, unsupported(), false
			}
			delete(extra, "working_directory")
		}
	}
	body := map[string]any{"command": resolved.Command}
	extraKeys, diags := HookExtraKeys(extra, CanonicalFields("command", "powershell", "show_output", "working_directory"), TargetWindsurf, name, source)
	for _, key := range extraKeys {
		body[key] = extra[key]
	}
	if sidecar := h.Ext.Windsurf; sidecar != nil {
		if sidecar.HookPowerShell != "" {
			body["powershell"] = sidecar.HookPowerShell
		}
		if sidecar.HookShowOutput != nil {
			body["show_output"] = *sidecar.HookShowOutput
		}
		if sidecar.HookWorkingDir != "" {
			body["working_directory"] = sidecar.HookWorkingDir
		}
	}
	return body, diags, true
}

func devinHookPayload(h *ir.Hook, name string, source diag.SourceRef, resolvedCommand string) (key, value string, diags []diag.Diagnostic) {
	var unusedKey, unused string
	if h.Type == "command" {
		key, unusedKey, value, unused = "command", "prompt", resolvedCommand, h.Prompt
	} else {
		key, value = "prompt", h.Prompt
		unusedKey, unused = "command", h.Command
	}
	if value == "" {
		return key, "", append(diags, diag.Diagnostic{
			Code:     diag.CodeHookDevinPayloadMissing,
			Severity: diag.SeverityError,
			Target:   string(TargetDevin),
			Resource: name,
			Message:  fmt.Sprintf("the %q hook carries no %s payload; the hook entry was skipped", h.Type, key),
			Hint:     "Set the payload the hook type selects: command hooks need a command and prompt hooks need a prompt",
			Source:   source,
		})
	}
	if unused != "" {
		diags = append(diags, diag.Diagnostic{
			Code:     diag.CodeHookDevinPayloadDropped,
			Severity: diag.SeverityWarning,
			Target:   string(TargetDevin),
			Resource: name,
			Message:  fmt.Sprintf("the %q hook also carries a %s payload, which Devin does not run for this hook type; that payload was dropped", h.Type, unusedKey),
			Hint:     "Split the hook into one command hook and one prompt hook if both payloads are required",
			Source:   source,
		})
	}
	return key, value, diags
}

func codexNativeHookRule(h *ir.Hook, resolved hookpolicy.Resolved) (map[string]any, []diag.Diagnostic, bool) {
	name, source := h.Name(), DiagSource(h.Source)
	var diags []diag.Diagnostic
	sessionEndAsync := resolved.Event == "SessionEnd" && h.Ext.Codex != nil && h.Ext.Codex.HookAsync != nil && *h.Ext.Codex.HookAsync
	if resolved.Command == "" {
		return nil, []diag.Diagnostic{{Code: diag.CodeHookCodexUnsupported, Severity: diag.SeverityError, Target: string(TargetCodex), Resource: name, Message: "Codex does not execute this hook event or handler; hook suppressed", Source: source}}, false
	}
	handler := map[string]any{"type": h.Type, "command": resolved.Command}
	if resolved.Timeout != nil {
		handler["timeout"] = *resolved.Timeout
	}
	if h.Ext.Codex != nil {
		if h.Ext.Codex.HookAsync != nil && !sessionEndAsync {
			handler["async"] = *h.Ext.Codex.HookAsync
		}
		if h.Ext.Codex.HookStatusMessage != nil {
			handler["statusMessage"] = *h.Ext.Codex.HookStatusMessage
		}
		if h.Ext.Codex.CommandWindows != nil {
			handler["commandWindows"] = *h.Ext.Codex.CommandWindows
		}
		if h.Ext.Codex.AdditionalContextLimit != nil {
			handler["additionalContextLimit"] = *h.Ext.Codex.AdditionalContextLimit
		}
	}
	rule := map[string]any{"hooks": []any{handler}}
	if resolved.Matcher != "" {
		rule["matcher"] = resolved.Matcher
	}
	if sessionEndAsync {
		diags = append(diags, diag.Diagnostic{Code: diag.CodeCodexFieldDropped, Severity: diag.SeverityWarning, Target: string(TargetCodex), Resource: name, Message: "SessionEnd always runs synchronously; async was omitted", Source: source})
	}
	if h.Prompt != "" || len(h.Extra) > 0 {
		diags = append(diags, diag.Diagnostic{Code: diag.CodeCodexFieldDropped, Severity: diag.SeverityWarning, Target: string(TargetCodex), Resource: name, Message: "Codex hook output omitted fields without a documented command-handler equivalent", Source: source})
	}
	return rule, diags, true
}

func geminiNativeHookRule(h *ir.Hook, dst Target, name string, source diag.SourceRef, rule map[string]any, canonical map[string]struct{}, matcher string) (map[string]any, []diag.Diagnostic, bool) {
	extraKeys, diags := HookExtraKeys(h.Extra, canonical, dst, name, source)
	for _, k := range extraKeys {
		rule[k] = h.Extra[k]
	}
	group := map[string]any{"hooks": []any{rule}}
	if matcher != "" {
		group["matcher"] = matcher
	}
	if sidecar := h.Ext.Gemini; sidecar != nil {
		keys, groupDiags := HookExtraKeys(sidecar.HookGroupExtra, PresentFields(map[string]bool{"hooks": true, "matcher": matcher != ""}), dst, name, source)
		diags = append(diags, groupDiags...)
		for _, key := range keys {
			group[key] = sidecar.HookGroupExtra[key]
		}
	}
	return group, diags, true
}

func nativeProjectionEntry(shape targetmeta.HookShape, rule any) (json.RawMessage, error) {
	raw, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("encode native hook rule: %w", err)
	}
	if err := validateNativeHookFields(raw); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("native hook rule must be an object")
	}
	switch shape {
	case targetmeta.HookShapeMatcherGroups:
		if hooks, ok := fields["hooks"]; ok {
			var handlers []json.RawMessage
			if err := json.Unmarshal(hooks, &handlers); err != nil || len(handlers) == 0 {
				return nil, fmt.Errorf("native hook matcher group has no handlers")
			}
			return json.Marshal(fields)
		}
		matcher, hasMatcher := fields["matcher"]
		delete(fields, "matcher")
		delete(fields, "event")
		handler, err := json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("encode native hook handler: %w", err)
		}
		handlers, err := json.Marshal([]json.RawMessage{handler})
		if err != nil {
			return nil, fmt.Errorf("encode native hook handlers: %w", err)
		}
		group := map[string]json.RawMessage{"hooks": handlers}
		if hasMatcher {
			group["matcher"] = matcher
		}
		return json.Marshal(group)
	case targetmeta.HookShapeEventHandlers:
		delete(fields, "event")
		return json.Marshal(fields)
	default:
		return nil, fmt.Errorf("unknown native hook shape %q", shape)
	}
}

func validateNativeHookFields(raw json.RawMessage) error {
	pending := []json.RawMessage{raw}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		var object map[string]json.RawMessage
		if err := json.Unmarshal(current, &object); err == nil && object != nil {
			for key, value := range object {
				if strings.EqualFold(key, "_managed_by") {
					return fmt.Errorf("native hook entry contains reserved ownership metadata")
				}
				pending = append(pending, value)
			}
			continue
		}
		var values []json.RawMessage
		if err := json.Unmarshal(current, &values); err == nil {
			pending = append(pending, values...)
		}
	}
	return nil
}
