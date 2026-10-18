package opencode

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/opencodehook"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/toolpolicy"
)

var adapterToolName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func hook(h *ir.Hook, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	fail := func(prior ...diag.Diagnostic) ([]artifact.Artifact, []diag.Diagnostic, error) {
		if diag.HasCode(prior, diag.CodeHookOpenCodeUnsupported) {
			return nil, prior, nil
		}
		return nil, append(prior, diagnostic(diag.CodeHookOpenCodeUnsupported, diag.SeverityError, h.Name(), h.Source, "OpenCode supports command PreToolUse hooks with exact tool matchers and positive timeouts up to 600 seconds; hook omitted")), nil
	}
	unknownMetadata := len(h.Extra) > 0
	// Canonical payloads carry this reserved null serializer field.
	if value, present := h.Extra["Extra"]; present && value == nil && len(h.Extra) == 1 {
		unknownMetadata = false
	}
	if (h.Type != "" && h.Type != "command") || h.Command == "" || h.Prompt != "" || unknownMetadata {
		return fail()
	}
	overridden, matcher, ok := overriddenMatcher(h, opts.ToolOverride())
	if !ok {
		return fail()
	}
	resolveInput := h
	if overridden {
		// Event, deadline, and command still resolve through the catalogs;
		// only the consumer-mapped matcher bypasses catalog coverage.
		clone := *h
		clone.Matcher = ""
		resolveInput = &clone
	}
	scope := hookpolicy.ScopeRepository
	if opts.Scope() == compile.ScopeUser {
		scope = hookpolicy.ScopeMachine
	}
	resolved := hookpolicy.Resolve(resolveInput, target.TargetOpenCode, scope, opts.PluginRoot())
	if resolved.Failed() {
		return fail(resolved.Diagnostics...)
	}
	if resolved.Event != "PreToolUse" {
		return fail()
	}
	if !overridden {
		if matcher, ok = declarationMatcher(resolved.Matcher); !ok {
			return fail()
		}
	}
	timeout := opencodehook.DefaultTimeout
	if resolved.Timeout != nil {
		timeout = *resolved.Timeout
	}
	if timeout < 1 || timeout > 600 {
		return fail()
	}
	declaration := opencodehook.Declaration{Format: opencodehook.Format, Name: h.Name(), Event: resolved.Event, Matcher: matcher, Command: resolved.Command, Timeout: timeout}
	data, err := json.MarshalIndent(declaration, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	// The adapter is one fixed reviewed file shared by every accepted hook in
	// a bundle. It is emitted as a fragment that coalesces byte-identical
	// copies, so a multi-resource bundle publishes exactly one adapter
	// alongside all of its declarations instead of colliding on the shared
	// path. A single-resource compile still returns the adapter unchanged.
	return []artifact.Artifact{
		{Path: path.Join(opencodehook.DeclarationsDir, h.Name()+".json"), Bytes: append(data, '\n'), Mode: 0600},
		{Path: opencodehook.AdapterPath, Bytes: append([]byte(nil), opencodehook.Adapter...), Mode: 0644, Fragment: true, FragmentKey: opencodehook.AdapterFragmentKey, DedupeFragmentEntries: true},
	}, resolved.Diagnostics, nil
}

// declarationMatcher converts a catalog-rendered OpenCode matcher into the
// spelling the fixed adapter compares at runtime: the adapter canonicalizes
// native tool IDs before matching, so each native ID is written in its
// canonical form. Only the verified catalog tools are accepted. Task stays
// unsupported because nested subagent tool-execution coverage is not
// advertised, and MCP patterns are refused because the adapter compares exact
// names.
func declarationMatcher(rendered string) (string, bool) {
	if rendered == "" || rendered == "*" {
		return rendered, true
	}
	elements := strings.Split(rendered, "|")
	for i, element := range elements {
		if canonical, ok := opencodehook.CanonicalTools[element]; ok {
			element = canonical
		}
		if element == "Task" || !toolpolicy.IsToolOnSurface(target.TargetOpenCode, toolpolicy.SurfaceHookMatcher, element) {
			return "", false
		}
		elements[i] = element
	}
	return strings.Join(elements, "|"), true
}

// overriddenMatcher applies explicit tool overrides, the same mapping
// authority as restrictive subagent policies. It reports whether any matcher
// element was overridden; non-overridden elements of such a matcher still
// translate through the catalogs individually.
//
// An override value must be a plain tool name that the adapter does NOT
// canonicalize: a value the fixed runtime rewrites to another name would never
// compare equal, so such a mapping fails closed instead of installing a
// silently dead matcher. Task stays unsupported regardless of mapping.
func overriddenMatcher(h *ir.Hook, overrides map[string]string) (bool, string, bool) {
	if h.Matcher == "" || h.Matcher == "*" || len(overrides) == 0 {
		return false, "", true
	}
	elements := strings.Split(h.Matcher, "|")
	applies := false
	for _, element := range elements {
		if _, ok := overrides[element]; ok {
			applies = true
		}
	}
	if !applies {
		return false, "", true
	}
	source := target.Target(h.HookSource)
	srcSpec, srcOK := hookpolicy.MatcherSpecFor(source)
	dstSpec, dstOK := hookpolicy.MatcherSpecFor(target.TargetOpenCode)
	if !srcOK || !dstOK {
		return true, "", false
	}
	emitted := make([]string, 0, len(elements))
	for _, element := range elements {
		translated, err := toolpolicy.TranslateMatcher(source, target.TargetOpenCode, element, srcSpec, dstSpec)
		if err == nil && translated == "task" {
			return true, "", false
		}
		if value, ok := overrides[element]; ok {
			if _, canonicalized := opencodehook.CanonicalTools[value]; canonicalized || value == "Task" || !adapterToolName.MatchString(value) {
				return true, "", false
			}
			emitted = append(emitted, value)
			continue
		}
		if err != nil {
			return true, "", false
		}
		converted, ok := declarationMatcher(translated)
		if !ok || converted == "" || converted == "*" {
			return true, "", false
		}
		emitted = append(emitted, converted)
	}
	return true, strings.Join(emitted, "|"), true
}
