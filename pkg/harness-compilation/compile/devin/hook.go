package devin

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Devin hook vocabulary. Evidence: docs/devin-cli-harness-spec-3000.3.md,
// "Hooks" — "Documented hook types are: command, prompt". The vocabulary is
// closed; anything else is skipped rather than guessed at.
const (
	hookTypeCommand = "command"
	hookTypePrompt  = "prompt"

	// Rule member names inside one hook entry. They double as the reserved
	// extension keys: none of them may ever reach an artifact from Extra.
	hookKeyType    = "type"
	hookKeyMatcher = "matcher"
	hookKeyCommand = "command"
	hookKeyPrompt  = "prompt"
	hookKeyTimeout = "timeout"
	// hookKeyEvent is not a member of the rule object — the event is the key
	// the rule array hangs off — but it is reserved for the same reason.
	hookKeyEvent = "event"

	// hooksKey is the member of a Devin main config file that carries hooks.
	// Plan, resource mapping: "Hook | Project: hooks.v1.json; user:
	// config.json#hooks".
	hooksKey = "hooks"
)

// hookMatcherEvents are the Devin events that documented matchers apply to.
//
// Evidence: docs/devin-cli-harness-spec-3000.3.md, "Hooks" — "Documented
// matchers apply to tool-related events". Only PreToolUse and PostToolUse are
// documented as tool events, so the set is deliberately narrow.
//
// The narrow set is the fail-closed choice. A matcher restricts WHEN a hook
// fires. If Devin ignores a matcher on an event it does not support matchers
// for, the hook fires for every occurrence of that event instead of the subset
// the author selected — a strictly broader activation surface than requested.
// Skipping the entry cannot widen anything; emitting it might. Whether
// PermissionRequest also accepts a matcher is a 3000.3.27 runtime probe item
// (D12); until a probe says so it stays out of this set.
var hookMatcherEvents = map[string]struct{}{
	"PreToolUse":  {},
	"PostToolUse": {},
}

// hookBehaviorIdentity documents the total identity that gives a compiled Devin
// hook its identity. The identity is realized by the materializer as the Devin
// event name plus the digest of the canonical JSON rule, so the rule object
// emitted by this compiler is the entire input to identity.
//
// Plan, entry identities: "Hook behavior identity is the event name plus the
// digest of the canonical hook value" and "Duplicate desired hook values
// collapse before patching."
//
// The identity this compiler produces is represented EXACTLY by the pair of the
// event key and the rule object that translateHook emits — nothing more,
// nothing less:
//
//   - The Devin event name, not the canonical IR event name, is the identity
//     component. Two IR events that map to the same Devin event produce the
//     same runtime behavior, so they must collapse.
//   - matcher, type, the payload the type selects, and timeout are the fields
//     the runtime reads. Each one changes when or how the hook runs.
//   - Preserved extras are included because Devin forwards unknown members to
//     its hook runtime; two entries that differ in an extra may behave
//     differently, so they must NOT collapse. "Relevant" in the plan means the
//     extras that survive into the artifact — a reserved extra that was
//     overridden by a canonical field never reaches the rule and so cannot
//     affect behavior.
//   - The IR hook NAME and its SourceRef are deliberately NOT part of the
//     identity. Neither is written to the artifact and neither changes what
//     Devin does, so two hooks that differ only in name are one behavior and
//     collapse into one entry. That is the case the plan's "One semantically
//     identical unmanaged hook may satisfy any number of selected marketplace
//     resources" rule depends on.
//   - The payload the type does NOT select is excluded, because it is dropped
//     (with a diagnostic) and therefore cannot influence behavior.
//
// The collapse itself happens during fragment merging: each hook emits one
// fragment carrying one rule, and artifact.MergeFragments concatenates the
// per-event arrays. Setting Artifact.DedupeFragmentEntries makes that
// concatenation drop an entry that is JSON-equal to one already present.
// JSON equality of the rule object is exactly identity equality, because the
// rule object contains every member that can influence runtime behavior and
// encoding/json emits object keys in sorted order. First occurrence wins, so
// the surviving order is the deterministic bundle resource order and is
// independent of how many duplicates arrived or in which order the identical
// copies appeared.
const hookBehaviorIdentity = "event+canonical-rule-digest"

// translateHook compiles one hook into a scope-specific Devin aggregate
// fragment.
//
// Plan, resource mapping: "Hook | Project: `hooks.v1.json`; user:
// `config.json#hooks` | Command or prompt hook merged into a shared file".
//
// Every unrepresentable combination skips this ONE entry — never the bundle,
// never a sibling hook — and reports it as an ERROR. Plan, error behavior:
// "Within valid aggregate JSON, invalid or conflicting desired entries are
// skipped while other valid entries are patched atomically; sync still exits
// non-zero." The two losses that still EMIT the entry (an inert dropped payload,
// an unexpanded plugin-root token) stay warnings. See the severity contract on
// the hook.devin.* codes in diag/codes.go.
//
// No diagnostic, hint, or log attribute in this file carries a command string,
// prompt body, matcher value, timeout value, or extension value. Plan,
// observability: "Never log hook payloads". A hook's entire payload is a
// command line, which makes it the highest-risk kind in this target.
func translateHook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	source := compile.DiagSource(h.Source)
	name := h.Name()

	event, ok := mapping.HookEvent(h.Event, h.Matcher, mapping.TargetDevin)
	if !ok {
		// The canonical event name is a fixed vocabulary term, not user
		// content, so naming it is safe and makes the diagnostic actionable.
		slog.Error("devin hook event unmappable", "target", mapping.TargetDevin, "resource", name, "event", h.Event)
		return nil, []diag.Diagnostic{hookSkipDiagnostic(diag.CodeHookDevinEventUnmappable, name, source,
			fmt.Sprintf("Devin documents no %q hook event; the hook entry was skipped", h.Event),
			"Use a Devin-documented hook event: PreToolUse, PostToolUse, PermissionRequest, UserPromptSubmit, Stop, SessionStart, SessionEnd, or PostCompaction",
		)}, nil
	}

	if h.Type != hookTypeCommand && h.Type != hookTypePrompt {
		// An empty type is included here on purpose. Source loaders default an
		// absent type to "command", so an empty type only reaches this
		// compiler from hand-built IR, and Devin's documented default for a
		// typeless hook is not part of the 3000.3 evidence. Guessing "command"
		// would run a payload the author never declared executable.
		slog.Error("devin hook type unsupported", "target", mapping.TargetDevin, "resource", name, "type", h.Type)
		return nil, []diag.Diagnostic{hookSkipDiagnostic(diag.CodeHookDevinUnsupportedType, name, source,
			fmt.Sprintf("Devin does not support the %q hook type; the hook entry was skipped", h.Type),
			"Devin documents the command and prompt hook types only",
		)}, nil
	}

	if h.Matcher != "" {
		if _, tool := hookMatcherEvents[event]; !tool {
			slog.Error("devin hook matcher unsupported for event", "target", mapping.TargetDevin, "resource", name, "event", event)
			return nil, []diag.Diagnostic{hookSkipDiagnostic(diag.CodeHookDevinMatcherUnsupportedEvent, name, source,
				fmt.Sprintf("Devin documents hook matchers for tool events only, not for %q; the hook entry was skipped", event),
				"Remove the matcher, or move the hook to PreToolUse or PostToolUse; emitting the hook without its matcher would widen it to every occurrence of the event",
			)}, nil
		}
	}

	if h.Timeout != nil && *h.Timeout < 0 {
		// A negative timeout is undefined for Devin. It could plausibly be read
		// as "no limit", which would let a hook command run unbounded — a
		// broader execution surface than the source asked for. Fail closed.
		// An explicit 0 is preserved verbatim: it is the strictest value the
		// author can express, so passing it through can only narrow.
		slog.Error("devin hook timeout invalid", "target", mapping.TargetDevin, "resource", name)
		return nil, []diag.Diagnostic{hookSkipDiagnostic(diag.CodeHookDevinTimeoutInvalid, name, source,
			"hook timeout is negative, which Devin does not define; the hook entry was skipped",
			"Use a non-negative timeout, or remove the timeout so Devin applies its own default",
		)}, nil
	}

	payloadKey, payload, diags := resolveHookPayload(h, name, source, opts)
	if payload == "" {
		return nil, diags, nil
	}

	rule := map[string]any{hookKeyType: h.Type, payloadKey: payload}
	if h.Matcher != "" {
		rule[hookKeyMatcher] = h.Matcher
	}
	if h.Timeout != nil {
		rule[hookKeyTimeout] = *h.Timeout
	}

	// Unknown-field precedence, plan: "Canonical modeled fields win over
	// conflicting values in preserved extras. The compiler emits a stable
	// warning naming the conflicting field without including sensitive
	// content."
	//
	// Every hook field is reserved UNCONDITIONALLY, which is stricter than the
	// skill and subagent compilers. Presence is keyed on the reservation, not
	// on what survived mapping, because each of these keys is the hook's
	// behavior:
	//
	//   - command and prompt ARE the payload. An extension copy would inject an
	//     unvetted command line or prompt body into a hook that this compiler
	//     validated as carrying a different payload.
	//   - type selects which payload the runtime executes, so an extension copy
	//     could turn a validated prompt hook into a command hook.
	//   - matcher gates activation. An extension copy would bypass the tool-
	//     event check above and could widen or narrow when the hook fires.
	//   - timeout bounds execution. An extension copy could raise or remove a
	//     bound that was validated as non-negative.
	//   - event is not a member of the rule object at all; it is the key the
	//     array hangs off. An extension copy inside the rule would contradict
	//     that key and confuse both Devin and the D8 ingester.
	//
	// Nothing here is consumed from Extra and re-emitted in mapped form, so the
	// conflict warning is always honest: the canonical value really did win.
	canonical := compile.PresentFields(map[string]bool{
		hookKeyEvent:   true,
		hookKeyType:    true,
		hookKeyMatcher: true,
		hookKeyCommand: true,
		hookKeyPrompt:  true,
		hookKeyTimeout: true,
		hooksKey:       true,
	})
	extraKeys, extraDiags := compile.HookExtraKeys(h.Extra, canonical, mapping.TargetDevin, name, source)
	diags = append(diags, extraDiags...)
	for _, key := range extraKeys {
		rule[key] = h.Extra[key]
	}

	art, err := hookArtifact(event, rule, opts)
	if err != nil {
		return nil, nil, err
	}
	slog.Debug("devin hook compiled", "target", mapping.TargetDevin, "resource", name,
		"event", event, "type", h.Type, "scope", opts.Scope(), "path", art.Path, "identity", hookBehaviorIdentity)
	return []artifact.Artifact{art}, diags, nil
}

// resolveHookPayload returns the rule member the hook type selects and its
// value, reporting the payload losses on the way.
//
// The hook type is authoritative: a command hook emits `command` and a prompt
// hook emits `prompt`. Devin executes exactly one of them, so emitting both
// would either be ignored or run something the type did not declare.
//
// An empty selected payload returns "" and the caller skips the entry: an entry
// with a type and no payload is not a hook, and inventing one is forbidden.
func resolveHookPayload(h *ir.Hook, name string, source diag.SourceRef, opts compile.Options) (key, value string, diags []diag.Diagnostic) {
	var unusedKey, unused string
	switch h.Type {
	case hookTypeCommand:
		key, unusedKey, unused = hookKeyCommand, hookKeyPrompt, h.Prompt
		var pluginDiags []diag.Diagnostic
		value, pluginDiags = resolvePluginRoot(h.Command, name, source, opts)
		diags = append(diags, pluginDiags...)
	default:
		// Prompt hooks carry text the model reads; they are never executed, so
		// the plugin-root token is not resolved in them. Substituting a
		// filesystem path into prose would change the prompt's meaning without
		// making anything work.
		key, value = hookKeyPrompt, h.Prompt
		unusedKey, unused = hookKeyCommand, h.Command
	}

	if value == "" {
		slog.Error("devin hook payload missing", "target", mapping.TargetDevin, "resource", name, "type", h.Type, "field", key)
		return key, "", append(diags, hookSkipDiagnostic(diag.CodeHookDevinPayloadMissing, name, source,
			fmt.Sprintf("the %q hook carries no %s payload; the hook entry was skipped", h.Type, key),
			"Set the payload the hook type selects: command hooks need a command and prompt hooks need a prompt",
		))
	}

	if unused != "" {
		// Only the field NAME is reported. The dropped value is a command line
		// or a prompt body and must never appear in a diagnostic or a log.
		slog.Warn("devin hook payload dropped", "target", mapping.TargetDevin, "resource", name, "type", h.Type, "field", unusedKey)
		diags = append(diags, hookLossDiagnostic(diag.CodeHookDevinPayloadDropped, name, source,
			fmt.Sprintf("the %q hook also carries a %s payload, which Devin does not run for this hook type; that payload was dropped", h.Type, unusedKey),
			"Split the hook into one command hook and one prompt hook if both payloads are required",
		))
	}
	return key, value, diags
}

// resolvePluginRoot expands the canonical plugin-root token in a hook command
// and reports the hook-scoped loss when no plugin root is configured.
//
// The expansion rule itself lives in resolvePluginRootToken (fields.go), shared
// with the MCP compiler, so the two kinds cannot drift on whether an
// unconfigured plugin root may be expanded to an empty string. Only the
// diagnostic code differs, so consumer UIs can attribute the loss to the right
// resource kind.
//
// The command string is never logged or quoted: only whether the token was
// present and whether a plugin root was configured.
func resolvePluginRoot(command, name string, source diag.SourceRef, opts compile.Options) (string, []diag.Diagnostic) {
	resolved, unresolved := resolvePluginRootToken(command, opts)
	if !unresolved {
		return resolved, nil
	}
	slog.Warn("devin hook plugin root unresolved", "target", mapping.TargetDevin, "resource", name)
	return resolved, []diag.Diagnostic{hookLossDiagnostic(diag.CodeHookDevinPluginRootUnresolved, name, source,
		"the hook command references the plugin root, but Devin has no plugin-root token and no plugin root was configured; the token was left unexpanded",
		"Compile with a plugin root so the command resolves, or use an absolute command path",
	)}
}

// hookArtifact wraps one rule in the scope-specific aggregate envelope.
//
// Project scope writes hooks.v1.json, which "contains the hook object directly
// rather than nesting it under a top-level `hooks` key"
// (docs/devin-cli-harness-spec-3000.3.md, "Hooks"). User scope writes the
// `hooks` member of config.json, because "a user-level standalone hooks.v1.json
// is not part of the documented contract".
//
// The artifact is a fragment so every selected hook contributes one entry to
// one shared file, and it opts into entry deduplication so duplicate behavior
// tuples collapse (see hookBehaviorIdentity).
func hookArtifact(event string, rule map[string]any, opts compile.Options) (artifact.Artifact, error) {
	group := map[string]any{hooksKey: []any{rule}}
	if matcher, ok := rule[hookKeyMatcher]; ok {
		group[hookKeyMatcher] = matcher
		delete(rule, hookKeyMatcher)
	}
	events := map[string]any{event: []any{group}}

	path := projectHooks
	document := events
	if opts.Scope() == compile.ScopeUser {
		path = userConfig
		document = map[string]any{hooksKey: events}
	}

	bytes, err := json.Marshal(document)
	if err != nil {
		// The error is wrapped without the document: it would contain the
		// command payload.
		return artifact.Artifact{}, fmt.Errorf("devin: marshal hook fragment for %s: %w", path, err)
	}

	art := artifact.Artifact{
		Path:                  path,
		Bytes:                 bytes,
		Fragment:              true,
		FragmentKey:           path,
		DedupeFragmentEntries: true,
	}
	art.ManagedByField, art.ManagedByValue = opts.ManagedBy()
	art.MergeWithExisting = art.ManagedByField != ""
	return art, nil
}

// hookSkipDiagnostic builds the diagnostic for a hook combination that removes
// the entry. It is an ERROR: the plan requires that "invalid or conflicting
// desired entries are skipped ... sync still exits non-zero", and only an error
// severity reaches internal/pipeline's non-zero exit. The contract and its
// justification live with the hook.devin.* constants in diag/codes.go.
func hookSkipDiagnostic(code, resource string, source diag.SourceRef, message, hint string) diag.Diagnostic {
	return hookDiagnostic(diag.SeverityError, code, resource, source, message, hint)
}

// hookLossDiagnostic builds the diagnostic for a hook loss that still EMITS the
// entry — an inert sub-field was dropped or a token was left unexpanded. It
// stays a WARNING: escalating it would suppress an entry that is otherwise
// exactly what the source asked for, which contradicts the per-resource
// suppression contract.
func hookLossDiagnostic(code, resource string, source diag.SourceRef, message, hint string) diag.Diagnostic {
	return hookDiagnostic(diag.SeverityWarning, code, resource, source, message, hint)
}

func hookDiagnostic(severity diag.Severity, code, resource string, source diag.SourceRef, message, hint string) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     code,
		Severity: severity,
		Target:   string(mapping.TargetDevin),
		Resource: resource,
		Message:  message,
		Hint:     hint,
		Source:   source,
	}
}
