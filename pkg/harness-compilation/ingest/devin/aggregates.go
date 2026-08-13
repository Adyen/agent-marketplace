package devin

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/bundle"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/jsonc"
)

// hookRule is one entry of a Devin event array.
//
// The member set is exactly the tuple compile/devin emits — type, the payload
// its type selects, matcher, timeout — plus whatever unknown members Devin
// forwards to its hook runtime, which are preserved so two entries that differ
// only in an extra do not collapse into one behavior.
type hookRule struct {
	Type    string
	Command string
	Prompt  string
	Matcher string
	Timeout *int
	Extra   map[string]any
}

// hookRuleKeys are the members hookRule models. Everything else becomes Extra.
var hookRuleKeys = []string{"type", "command", "prompt", "matcher", "timeout", "hooks"}

// loadHooks reads every hook the explicit root declares.
//
// Two files are read, in this fixed order:
//
//  1. hooks.v1.json — the project standalone file, which "contains the hook
//     object directly rather than nesting it under a top-level `hooks` key"
//     (evidence baseline, "Hooks").
//  2. config.json#hooks — the user-scope location, "Hooks may also appear under
//     `hooks` in main config files".
//
// Reading both is NOT the implicit merge the plan forbids. That rule is about
// ROOTS ("project and user roots are never merged implicitly"); these are two
// canonical hook locations inside the ONE explicit root the caller named, and
// Devin itself collects hooks from every configuration source rather than
// letting one override another. A user root simply has no hooks.v1.json, and a
// project root usually has no hooks member, so in practice exactly one of the
// two contributes.
//
// A missing file is not an error. A present but unreadable one is: hook
// payloads are executable, so a file that cannot be parsed must never be
// silently treated as empty.
func loadHooks(src fs.FS, r root) ([]ir.Resource, []diag.Diagnostic) {
	var resources []ir.Resource
	var diags []diag.Diagnostic

	// One allocator across both files so two hooks with the same event and type
	// cannot end up with the same catalog key.
	names := newHookNamer()

	for _, source := range []struct {
		file   string
		nested bool
	}{
		{file: r.join(projectHooksFile), nested: false},
		{file: r.join(configFile), nested: true},
	} {
		events, err := readHookEvents(src, source.file, source.nested)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			diags = append(diags, ingest.LoadFailure(diag.CodeIngestDevinLoadHookFailed, "hooks", source.file, source.file, err))
			continue
		}
		hooks, hookDiags := hooksFromEvents(events, source.file, names)
		diags = append(diags, hookDiags...)
		resources = append(resources, hooks...)
	}
	return resources, diags
}

// readHookEvents decodes the event → rules object out of one file.
//
// nested selects the config.json shape, where the object hangs off a `hooks`
// member; hooks.v1.json carries it at the top level. A file that exists but has
// no hooks member decodes to an empty map, which is a legitimate "no hooks
// here" rather than a failure.
//
// jsonc.Unmarshal is the only decoder used: it ignores comments (Devin
// aggregates may carry them) and rejects duplicate object keys
// before decoding, so a repeated event key or a repeated `command` member
// cannot silently keep the last value and run a different program.
func readHookEvents(src fs.FS, file string, nested bool) (map[string][]hookRule, error) {
	data, err := fs.ReadFile(src, file)
	if err != nil {
		return nil, err
	}

	var top map[string]json.RawMessage
	if err := jsonc.Unmarshal(file, data, &top); err != nil {
		return nil, err
	}

	if nested {
		raw, ok := top[hooksKey]
		if !ok {
			return nil, nil
		}
		top = nil
		if err := json.Unmarshal(raw, &top); err != nil {
			return nil, fmt.Errorf("the %q member must be an object of event arrays: %w", hooksKey, err)
		}
	}

	events := make(map[string][]hookRule, len(top))
	for event, raw := range top {
		rules, err := decodeHookRules(raw)
		if err != nil {
			if nested {
				// Inside config.json#hooks every member IS an event, so a
				// member that is not an event array is a real error.
				return nil, fmt.Errorf("event %q: %w", event, err)
			}
			// hooks.v1.json is documented as Claude-Code-compatible, and
			// Claude-family hook files carry scalar metadata members such as
			// "claudeHooksImported" alongside the event arrays. A member that
			// is not an event array is one of those, not a broken hook.
			continue
		}
		if len(rules) > 0 {
			events[event] = rules
		}
	}
	return events, nil
}

// decodeHookRules decodes one event array.
//
// Both documented shapes are accepted, because hooks.v1.json is
// "Claude-Code-compatible":
//
//   - the flat rule the Devin compiler writes, {type, command|prompt, matcher?,
//     timeout?}
//   - the Claude matcher group, {matcher, hooks: [rule, ...]}, whose matcher
//     applies to every inner rule
//
// A group's matcher is pushed onto each inner rule that does not carry its own,
// so the resulting IR hook always states its own activation condition. An inner
// rule that DOES carry a matcher keeps it: the more specific value wins, and
// silently replacing it could widen when the hook fires.
func decodeHookRules(raw json.RawMessage) ([]hookRule, error) {
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("value must be an array of hook rules: %w", err)
	}

	var rules []hookRule
	for i, entry := range entries {
		rule, err := decodeHookRule(entry)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i, err)
		}
		nested, hasNested := entry["hooks"]
		if !hasNested {
			rules = append(rules, rule)
			continue
		}
		var inner []map[string]json.RawMessage
		if err := json.Unmarshal(nested, &inner); err != nil {
			return nil, fmt.Errorf("rule %d: the %q member must be an array of hook rules: %w", i, "hooks", err)
		}
		_, hasTimeout := entry["timeout"]
		hasPayload := rule.Type != "" || rule.Command != "" || rule.Prompt != "" ||
			hasTimeout || len(rule.Extra) > 0
		if len(inner) == 0 {
			if !hasPayload {
				continue
			}
			rules = append(rules, rule)
			continue
		}
		if hasPayload {
			return nil, fmt.Errorf("rule %d: a matcher group must not also carry a rule payload", i)
		}
		for j, entry := range inner {
			innerRule, err := decodeHookRule(entry)
			if err != nil {
				return nil, fmt.Errorf("rule %d.%d: %w", i, j, err)
			}
			if innerRule.Matcher == "" {
				innerRule.Matcher = rule.Matcher
			}
			rules = append(rules, innerRule)
		}
	}
	return rules, nil
}

// decodeHookRule decodes the modeled members of one rule object and preserves
// the rest. Every member is decoded into its declared type rather than accepted
// as any JSON value: a `timeout` that is a string, or a `command` that is an
// array, is a malformed hook, and coercing it would change what runs.
func decodeHookRule(entry map[string]json.RawMessage) (hookRule, error) {
	var rule hookRule
	for _, member := range []struct {
		key    string
		target *string
	}{
		{"type", &rule.Type},
		{"command", &rule.Command},
		{"prompt", &rule.Prompt},
		{"matcher", &rule.Matcher},
	} {
		key, target := member.key, member.target
		raw, ok := entry[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, target); err != nil {
			// The VALUE is never quoted: command and prompt are payloads and
			// matcher is user content. Only the member name is reported.
			return hookRule{}, fmt.Errorf("the %q member must be a string", key)
		}
	}
	if raw, ok := entry["timeout"]; ok {
		if err := json.Unmarshal(raw, &rule.Timeout); err != nil {
			return hookRule{}, fmt.Errorf("the %q member must be an integer", "timeout")
		}
	}

	for key, raw := range entry {
		if slices.Contains(hookRuleKeys, key) {
			continue
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return hookRule{}, fmt.Errorf("the %q member is not valid JSON", key)
		}
		if rule.Extra == nil {
			rule.Extra = make(map[string]any, len(entry))
		}
		rule.Extra[key] = decoded
	}
	return rule, nil
}

// hooksFromEvents converts one decoded event object into IR hooks.
//
// Events are visited in sorted order so the resource order — and therefore the
// generated hook names — are independent of Go's map iteration order.
func hooksFromEvents(events map[string][]hookRule, file string, names *hookNamer) ([]ir.Resource, []diag.Diagnostic) {
	eventNames := make([]string, 0, len(events))
	for event := range events {
		eventNames = append(eventNames, event)
	}
	sort.Strings(eventNames)

	var resources []ir.Resource
	var diags []diag.Diagnostic
	for _, devinEvent := range eventNames {
		event, eventMatcher, ok := mapping.ReverseHookEvent(devinEvent, mapping.TargetDevin)
		if !ok {
			// The event NAME is a vocabulary term, not user content, so naming
			// it is safe and is the only actionable part of the report.
			slog.Warn("devin hook event unmappable", "source", ingest.HarnessDevin, "event", devinEvent, "file", file)
			event = devinEvent
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeIngestDevinHookEventUnmappable,
				Severity: diag.SeverityWarning,
				Resource: devinEvent,
				Message:  fmt.Sprintf("%q is not a documented Devin hook event; the original event name was preserved", devinEvent),
				Hint:     "Use a documented Devin event: PreToolUse, PostToolUse, PermissionRequest, UserPromptSubmit, Stop, SessionStart, SessionEnd, or PostCompaction",
				Source:   diag.SourceRef{File: file, Line: 0},
			})
		}

		for _, rule := range events[devinEvent] {
			hookType := rule.Type
			if hookType == "" {
				// Devin's own default for a typeless hook is not part of the
				// 3000.3 evidence, but every loader in this repository defaults
				// an absent type to "command" and compile/devin refuses to
				// compile an empty type at all. Defaulting here keeps the
				// canonical IR consistent with the sibling ingesters; the
				// refusal to GUESS lives on the compile side, where it decides
				// whether a payload is executed.
				hookType = "command"
			}
			// The rule's own matcher wins over any matcher the event name
			// implied. Devin never encodes a matcher in the event name (that is
			// a Windsurf shape), so eventMatcher is always "" today; keeping
			// the precedence explicit means a future Devin event that DOES
			// imply one cannot silently override the author's value.
			matcher := eventMatcher
			if rule.Matcher != "" {
				matcher = rule.Matcher
			}

			name := names.next(event, hookType)
			options := []ir.HookOption{ir.WithHookPrompt(rule.Prompt)}
			if rule.Timeout != nil {
				options = append(options, ir.WithHookTimeoutPtr(rule.Timeout))
			}
			source := ir.SourceRef{File: file, Line: 0}

			var hook *ir.Hook
			if matcher != "" {
				hook = ir.NewHookWithMatcherOptions(name, event, matcher, rule.Command, hookType, rule.Extra, source, options...)
			} else {
				hook = ir.NewHookWithOptions(name, event, rule.Command, hookType, rule.Extra, source, options...)
			}
			slog.Debug("devin hook ingested", "source", ingest.HarnessDevin, "resource", name, "event", event, "type", hookType, "file", file)
			resources = append(resources, hook)
		}
	}
	return resources, diags
}

// hookNamer allocates unique hook resource names.
//
// Hook names become catalog resource keys, so they must be unique across every
// hook in the bundle — including across the two files loadHooks reads. The
// scheme (<event>-<type>, then -2, -3, ...) matches bundle.LoadHooks so a hook
// keeps its identity when a bundle is converted between harnesses.
type hookNamer struct {
	counts map[string]int
	used   map[string]struct{}
}

func newHookNamer() *hookNamer {
	return &hookNamer{counts: map[string]int{}, used: map[string]struct{}{}}
}

func (n *hookNamer) next(event, hookType string) string {
	base := event + "-" + hookType
	seen := n.counts[base]
	name := base
	for {
		if seen > 0 {
			name = fmt.Sprintf("%s-%d", base, seen+1)
		}
		if _, taken := n.used[name]; !taken {
			break
		}
		seen++
	}
	n.counts[base] = seen + 1
	n.used[name] = struct{}{}
	return name
}

// loadMcpServers reads mcp_config.json.
//
// The 3000.3 dedicated-file layout is the ONLY location read. The pre-3000.3
// `config.json#mcpServers` block is deliberately not read even though Devin
// migrates it automatically: the compatibility floor is 3000.3 ("Supporting
// Devin versions older than 3000.3" is an explicit non-goal), this target emits
// only the dedicated file, and reading a location it will never write back to
// would silently move servers between files on the next sync. Its presence is
// reported so the omission is visible.
func loadMcpServers(src fs.FS, r root) ([]ir.Resource, []diag.Diagnostic) {
	diags := legacyMcpDiagnostics(src, r)

	file := r.join(mcpConfigFile)
	servers, err := bundle.LoadMcpServers(src, file)
	if err != nil {
		if isNotExist(err) {
			return nil, diags
		}
		return nil, append(diags, ingest.LoadFailure(diag.CodeIngestDevinLoadMcpServersFailed, "MCP servers", file, file, err))
	}

	resources := make([]ir.Resource, 0, len(servers))
	for _, server := range servers {
		if normalizeMcpInterpolation(server) {
			slog.Debug("devin mcp env interpolation normalized", "source", ingest.HarnessDevin, "resource", server.Name(), "file", file)
			diags = append(diags, diag.Diagnostic{
				Code:     diag.CodeIngestDevinMcpEnvNormalized,
				Severity: diag.SeverityInfo,
				Resource: server.Name(),
				Message:  "Devin ${env:VAR} interpolation was normalized to the canonical ${VAR} form",
				Hint:     "The canonical IR is harness-neutral; compiling back to Devin restores ${env:VAR}",
				Source:   diag.SourceRef{File: file, Line: 0},
			})
		}
		slog.Debug("devin mcp server ingested", "source", ingest.HarnessDevin, "resource", server.Name(), "file", file)
		resources = append(resources, server)
	}
	return resources, diags
}

// devinEnvToken matches one complete Devin `${env:VAR}` interpolation token.
//
// The VAR grammar is deliberately the same one compile/devin's
// mcpInterpolationToken accepts, so the two directions cannot disagree about
// what a token is. Anything that is not a complete, well-formed token is left
// untouched rather than reinterpreted.
var devinEnvToken = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// normalizeMcpInterpolation rewrites Devin's documented `${env:VAR}` style into
// the canonical inline `${VAR}` style, in place, and reports whether anything
// changed.
//
// Why the IR must not keep the Devin spelling: the canonical interpolation
// style declared in compile/mapping/mcp.go is inline `${VAR}`, and every
// target's compiler maps FROM that. Leaving `${env:VAR}` in the IR would make
// Devin → Claude emit a literal `${env:VAR}` that Claude does not interpolate,
// so a value that resolved on Devin would silently become a literal string
// containing the variable name. Devin → Devin is unaffected either way, because
// `${env:VAR}` maps to itself, which is exactly why this bug would not show up
// in an identity round trip and is normalized here instead.
//
// Every field that can carry an interpolated value is covered: command, args,
// env values, url, header values, and the three OAuth members. None of the
// values is logged or returned.
func normalizeMcpInterpolation(server *ir.McpServer) bool {
	changed := false
	normalize := func(value string) string {
		if !strings.Contains(value, "${env:") {
			return value
		}
		replaced := devinEnvToken.ReplaceAllString(value, "${$1}")
		if replaced != value {
			changed = true
		}
		return replaced
	}

	server.Command = normalize(server.Command)
	for i, arg := range server.Args {
		server.Args[i] = normalize(arg)
	}
	for key, value := range server.Env {
		server.Env[key] = normalize(value)
	}
	server.URL = normalize(server.URL)
	for key, value := range server.Headers {
		server.Headers[key] = normalize(value)
	}
	server.OAuthClientID = normalize(server.OAuthClientID)
	server.OAuthClientSecret = normalize(server.OAuthClientSecret)
	server.OAuthResource = normalize(server.OAuthResource)
	return changed
}

// legacyMcpDiagnostics reports a pre-3000.3 config.json#mcpServers block.
//
// Only the PRESENCE of the member is inspected. The block is never decoded, so
// no command, argument, env value, header, or OAuth secret inside it is ever
// read into memory, let alone reported.
func legacyMcpDiagnostics(src fs.FS, r root) []diag.Diagnostic {
	file := r.join(configFile)
	data, err := fs.ReadFile(src, file)
	if err != nil {
		return nil
	}
	var top map[string]json.RawMessage
	if err := jsonc.Unmarshal(file, data, &top); err != nil {
		// A malformed config.json is already reported by loadHooks, which reads
		// the same file for the same reason. Reporting it twice would double
		// every diagnostic for one broken file.
		return nil
	}
	if _, present := top[legacyMcpServersKey]; !present {
		return nil
	}
	slog.Warn("devin legacy mcp location ignored", "source", ingest.HarnessDevin, "file", file)
	return []diag.Diagnostic{{
		Code:     diag.CodeIngestDevinLegacyMcpLocationIgnored,
		Severity: diag.SeverityWarning,
		Resource: "bundle",
		Message:  fmt.Sprintf("%q declares the pre-3000.3 %q block, which this target does not read; those servers were not ingested", file, legacyMcpServersKey),
		Hint:     "Devin 3000.3 migrated MCP configuration into mcp_config.json; move the servers there so they are ingested",
		Source:   diag.SourceRef{File: file, Line: 0},
	}}
}
