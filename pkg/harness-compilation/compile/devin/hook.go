package devin

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
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

	// Event, type, matcher, timeout, and command translation come from the
	// catalog-driven hookpolicy resolver (OpenSpec native-hook-integration,
	// task 3.4). An unrepresentable combination suppresses this ONE entry
	// with an error and never widens a restriction: a matcher on an event
	// the catalog declares without matchers is refused rather than dropped,
	// because emitting it would fire the hook for every occurrence of the
	// event — a strictly broader activation surface than the author selected.
	scope := hookpolicy.ScopeRepository
	if opts.Scope() == compile.ScopeUser {
		scope = hookpolicy.ScopeMachine
	}
	resolved := hookpolicy.Resolve(h, target.TargetDevin, scope, opts.PluginRoot())
	if resolved.Failed() {
		return nil, resolved.Diagnostics, nil
	}

	payloadKey, payload, diags := resolveHookPayload(h, name, source, resolved.Command)
	if payload == "" {
		return nil, diags, nil
	}

	rule := map[string]any{hookKeyType: resolved.Type, payloadKey: payload}
	if resolved.Matcher != "" {
		rule[hookKeyMatcher] = resolved.Matcher
	}
	if resolved.Timeout != nil {
		rule[hookKeyTimeout] = *resolved.Timeout
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
	diags = append(resolved.Diagnostics, diags...)

	art, err := hookArtifact(resolved.Event, rule, opts)
	if err != nil {
		return nil, nil, err
	}
	slog.Debug("devin hook compiled", "target", mapping.TargetDevin, "resource", name,
		"event", resolved.Event, "type", resolved.Type, "scope", opts.Scope(), "path", art.Path, "identity", hookBehaviorIdentity)
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
func resolveHookPayload(h *ir.Hook, name string, source diag.SourceRef, resolvedCommand string) (key, value string, diags []diag.Diagnostic) {
	var unusedKey, unused string
	switch h.Type {
	case hookTypeCommand:
		key, unusedKey, value, unused = hookKeyCommand, hookKeyPrompt, resolvedCommand, h.Prompt
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
		Reconciliation: artifact.ReconciliationPolicy{
			Mode: artifact.ReconciliationExternal,
		},
		ExternalReconciliationRequired: true,
	}
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
