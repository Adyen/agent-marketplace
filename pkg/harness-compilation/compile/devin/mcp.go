package devin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Devin MCP vocabulary, taken verbatim from the 3000.3 evidence baseline
// (docs/devin-cli-harness-spec-3000.3.md, "MCP servers → Schema").
//
// The two documented server objects are disjoint. The stdio form carries
// command/args/env/disabled; the remote form carries
// url/transport/headers/oauthClientId/oauthClientSecret/oauthResource/disabled.
// `disabled` is the only member both forms share.
//
// Every one of these names is also a RESERVED extension key: see the canonical
// map in translateMcpServer.
const (
	// mcpServersKey is the only top-level member this target ever writes.
	// Evidence: "This target emits only the 3000.3+ dedicated-file layout"
	// with `{"mcpServers": {...}}` in both the stdio and HTTP examples. The
	// pre-3000.3 `config.json#mcpServers` layout is never emitted.
	mcpServersKey = "mcpServers"

	mcpKeyCommand           = "command"
	mcpKeyArgs              = "args"
	mcpKeyEnv               = "env"
	mcpKeyURL               = "url"
	mcpKeyTransport         = "transport"
	mcpKeyHeaders           = "headers"
	mcpKeyDisabled          = "disabled"
	mcpKeyOAuthClientID     = "oauthClientId"
	mcpKeyOAuthClientSecret = "oauthClientSecret"
	mcpKeyOAuthResource     = "oauthResource"
)

// mcpRemoteTransports is the closed set of remote transports Devin documents.
//
// Evidence: the documented HTTP form uses `"transport": "http"`, and "Remote
// servers default to Streamable HTTP and may fall back to SSE". Nothing else
// is documented, and an undocumented transport is never guessed at: see
// transportDiagnostics for why an unknown transport skips the entry instead of
// being dropped.
var mcpRemoteTransports = map[string]struct{}{
	"http": {},
	"sse":  {},
}

// mcpStdioOnlyKeys and mcpRemoteOnlyKeys are the modeled members that belong to
// exactly one documented server form. A field from the other form is dropped
// with mcp.devin.field-dropped rather than emitted, because Devin's two schemas
// are disjoint and the 3000.3 snapshot lists unknown-field handling as an
// unresolved probe item ("Comment handling, unknown-field handling,
// disabled-state round trip, and exact interpolation behavior require isolated
// 3000.3.27 probes").
//
// Privilege impact of every drop in these two lists is one-directional: env,
// headers, and the three OAuth members only ever ADD something the server
// receives (a variable, a header, a credential). Removing them can narrow what
// the server is given but can never widen it, and the removal is reported.
var (
	mcpStdioOnlyKeys  = []string{mcpKeyCommand, mcpKeyArgs, mcpKeyEnv}
	mcpRemoteOnlyKeys = []string{mcpKeyURL, mcpKeyTransport, mcpKeyHeaders, mcpKeyOAuthClientID, mcpKeyOAuthClientSecret, mcpKeyOAuthResource}
)

// mcpInterpolationToken matches one complete canonical interpolation token.
// The three alternatives are exactly the three source patterns declared in
// compile/mapping/mcp.go: a bare `${VAR}` (the canonical "inline" style),
// `${env:VAR}`, and `${file:PATH}`. Anything that is not a complete, well-formed
// token is left untouched rather than reinterpreted.
var mcpInterpolationToken = regexp.MustCompile(`\$\{(env:[A-Za-z_][A-Za-z0-9_]*|file:[^{}]*|[A-Za-z_][A-Za-z0-9_]*)\}`)

// mcpEntryIdentity documents the identity of a compiled Devin MCP entry.
//
// Plan, entry identities: "MCP identity is the server object key." That is the
// whole rule, and it is why this compiler does NOT set
// Artifact.DedupeFragmentEntries:
//
//   - Dedup only affects arrays of objects (see artifact.appendUniqueObjects).
//     Devin MCP entries are members of the `mcpServers` OBJECT, keyed by server
//     name, so there is no array to collapse.
//   - artifact.mergeMCPServers already implements the keyed rule: an identical
//     duplicate definition for the same key collapses, and a CONFLICTING
//     definition for the same key is rejected with an error rather than
//     silently resolved. Turning dedup on could not improve either outcome and
//     would only opt any nested extras array into a different merge semantic
//     than the sibling targets use.
const mcpEntryIdentity = "server-object-key"

// translateMcpServer compiles one MCP server into a Devin mcp_config.json
// fragment.
//
// Plan, resource mapping: "MCP server | `mcp_config.json` | Stdio or HTTP
// server merged into a shared file".
//
// SCOPE: unlike hooks, the Devin MCP file does NOT vary by scope. The evidence
// baseline's canonical-roots table gives `.devin/mcp_config.json` for project
// and `~/.config/devin/mcp_config.json` for user — the same relative path and
// the same `mcpServers` envelope under two different roots, which the caller
// selects. There is deliberately no compile.ScopeUser branch here; adding one
// would invent a layout the spec does not document. (The project-local personal
// files `.devin/mcp_config.local.json` and `.devin/config.local.json` are
// explicitly never materialized.)
//
// SEVERITY: every diagnostic this file emits costs at most one entry, and its
// severity follows that OUTCOME. A code that removes the whole keyed entry is
// an ERROR so sync exits non-zero (mcp.devin.transport-*,
// mcp.devin.interpolation-unrepresentable); a code that still emits the entry
// and only drops an inert sub-field or one value is a WARNING
// (mcp.devin.field-dropped, mcp.devin.plugin-root-unresolved, the shared
// mcp.interpolation-unsupported). The contract and its justification live with
// the mcp.devin.* constants in diag/codes.go. The slog level follows the same
// split — an entry-removing branch logs at Error, an entry-keeping one at Warn
// — so the structured log and the diagnostic never disagree, matching hook.go.
//
// CREDENTIAL HYGIENE: no diagnostic message, hint, or slog attribute in this
// file carries an env value, a header value, an OAuth client id/secret/resource,
// a command string, an argument, or a URL. Plan, literal credentials: "Secret
// values must never appear in structured logs, diagnostics, ownership state,
// journals, or backup metadata." Only FIELD NAMES, the resource name, and the
// source location are ever reported. TestMcpDiagnosticsNeverQuoteValues pins it
// for every diagnostic-producing branch.
func translateMcpServer(m *ir.McpServer, opts compile.Options) ([]artifact.Artifact, []diag.Diagnostic, error) {
	source := compile.DiagSource(m.Source)
	name := m.Name()

	// Plan, literal credentials: "Repo-scoped literal values in MCP environment
	// variables, HTTP headers, or OAuth client secrets are preserved but emit a
	// security warning." The shared helper inspects the RAW IR values (before
	// any interpolation rewrite, so `${TOKEN}` is still recognized as a
	// reference) and reports field names only. It runs before the skip checks:
	// a literal credential in the source is a fact about the configuration and
	// is worth reporting even when the entry itself turns out to be
	// unrepresentable.
	diags := compile.McpCredentialDiagnostics(m, mapping.TargetDevin, compile.WithMcpOptions(opts))

	stdio, remote := m.Command != "", m.URL != ""
	formDiags, ok := mcpFormDiagnostics(name, source, stdio, remote, m.Transport)
	diags = append(diags, formDiags...)
	if !ok {
		return nil, diags, nil
	}

	entry, entryDiags, ok := buildMcpEntry(m, stdio, opts)
	diags = append(diags, entryDiags...)
	if !ok {
		return nil, diags, nil
	}

	// Unknown-field precedence, plan: "Canonical modeled fields win over
	// conflicting values in preserved extras. The compiler emits a stable
	// warning naming the conflicting field without including sensitive
	// content."
	//
	// All ten documented member names are reserved UNCONDITIONALLY — the
	// strictest form, matching the hook compiler rather than the skill one.
	// Nothing here is ever consumed from Extra and re-emitted in mapped form,
	// so the conflict warning is always honest: an extension copy really was
	// dropped in favour of the compiler's own decision. The reason each key is
	// reserved even when its typed field is absent:
	//
	//   - oauthClientSecret is the sensitive value the plan singles out. Typing
	//     it in the IR is worthless if an untyped extension map can put a
	//     secret straight into the artifact behind the compiler's back, so the
	//     key can never be written from Extra. oauthClientId and oauthResource
	//     are reserved with it: together they select WHICH identity provider
	//     and WHICH protected resource the credential is presented to.
	//   - command and args ARE the executed process, and url is the endpoint
	//     the agent talks to. An extension copy would run or contact something
	//     that never passed the form and interpolation checks below.
	//   - env and headers carry credentials and are the two fields the literal-
	//     credential scan above covers. An extension copy would bypass that
	//     scan entirely.
	//   - transport selects the wire protocol and is validated against a closed
	//     documented vocabulary; an extension copy would bypass that check.
	//   - disabled is the activation switch. An extension copy could re-enable
	//     a server the source disabled — the clearest possible widening of the
	//     activation surface.
	//
	// Reserving them unconditionally is also what makes the form split above
	// safe: a field dropped as belonging to the other server form cannot be
	// smuggled back in through Extra under the same name.
	canonical := compile.PresentFields(map[string]bool{
		mcpKeyCommand:           true,
		mcpKeyArgs:              true,
		mcpKeyEnv:               true,
		mcpKeyURL:               true,
		mcpKeyTransport:         true,
		mcpKeyHeaders:           true,
		mcpKeyDisabled:          true,
		mcpKeyOAuthClientID:     true,
		mcpKeyOAuthClientSecret: true,
		mcpKeyOAuthResource:     true,
	})
	extraKeys, extraDiags := compile.ExtraKeys(m.Extra, canonical, mapping.TargetDevin, name, source)
	diags = append(diags, extraDiags...)
	for _, key := range extraKeys {
		entry[key] = m.Extra[key]
	}

	art, err := mcpArtifact(name, entry, opts)
	if err != nil {
		return nil, nil, err
	}
	slog.Debug("devin mcp server compiled", "target", mapping.TargetDevin, "resource", name,
		"form", mcpForm(stdio), "scope", opts.Scope(), "path", art.Path, "identity", mcpEntryIdentity)
	return []artifact.Artifact{art}, diags, nil
}

// mcpForm names the documented server shape for structured logging. It is a
// fixed vocabulary term, never user content.
func mcpForm(stdio bool) string {
	if stdio {
		return "stdio"
	}
	return "remote"
}

// mcpFormDiagnostics decides whether the resource selects exactly one of
// Devin's two documented server forms, and whether an explicit remote transport
// is one Devin documents.
//
// All three failures skip the entry rather than emitting a repaired one,
// because each repair would make a connection decision the source never
// authorized:
//
//   - Both command and url: Devin's two schemas are disjoint and the snapshot
//     does not say which member wins. Picking one would either execute a local
//     process or contact a remote endpoint on a coin flip — and the entry's
//     credentials (env for stdio, headers/OAuth for remote) would travel with
//     whichever was picked.
//   - Neither: there is nothing to connect to. Inventing an empty command or
//     url is exactly the "never invent empty command/args/env" rule.
//   - An undocumented transport: dropping the field would leave Devin to apply
//     its documented default ("Remote servers default to Streamable HTTP"), so
//     the agent would speak a protocol the author did not select, to an
//     endpoint chosen for a different one. Skipping cannot connect to anything;
//     defaulting can.
//
// No value is named: the URL, command, and transport string all stay out of the
// message, the hint, and the log.
func mcpFormDiagnostics(name string, source diag.SourceRef, stdio, remote bool, transport string) ([]diag.Diagnostic, bool) {
	switch {
	case stdio && remote:
		slog.Error("devin mcp server declares both forms", "target", mapping.TargetDevin, "resource", name)
		return []diag.Diagnostic{mcpSkipDiagnostic(diag.CodeMcpDevinTransportAmbiguous, name, source,
			"the MCP server declares both a stdio command and a remote url, which Devin's two documented server forms cannot express together; the server entry was skipped",
			"Split the definition into one stdio server and one remote server, or remove the field that does not apply",
		)}, false
	case !stdio && !remote:
		slog.Error("devin mcp server declares no form", "target", mapping.TargetDevin, "resource", name)
		return []diag.Diagnostic{mcpSkipDiagnostic(diag.CodeMcpDevinTransportIncomplete, name, source,
			"the MCP server declares neither a stdio command nor a remote url, so Devin has nothing to launch or connect to; the server entry was skipped",
			"Set a command for a stdio server or a url for a remote server",
		)}, false
	}

	if remote && transport != "" {
		if _, documented := mcpRemoteTransports[transport]; !documented {
			slog.Error("devin mcp transport undocumented", "target", mapping.TargetDevin, "resource", name)
			return []diag.Diagnostic{mcpSkipDiagnostic(diag.CodeMcpDevinTransportUnsupported, name, source,
				"the MCP server requests a transport Devin does not document; the server entry was skipped",
				"Devin documents the http and sse remote transports; dropping the transport instead would silently fall back to Devin's Streamable HTTP default",
			)}, false
		}
	}
	return nil, true
}

// buildMcpEntry renders the members of the documented server form the resource
// selected, and reports every modeled field that the other form owns.
//
// ok is false when a field the form cannot do without (command for stdio, url
// for remote) did not survive interpolation mapping: an entry with no command
// or no url is not a server, and emitting a truncated one would be inventing a
// value. That case goes through requiredValue and is an ERROR, because the
// outcome is a removed entry; every other interpolation loss goes through
// value, keeps the entry, and stays a warning.
func buildMcpEntry(m *ir.McpServer, stdio bool, opts compile.Options) (map[string]any, []diag.Diagnostic, bool) {
	source := compile.DiagSource(m.Source)
	name := m.Name()
	entry := make(map[string]any, 8)
	var diags []diag.Diagnostic

	interp := &mcpInterpolator{resource: name, source: source, opts: opts}

	if stdio {
		command, ok := interp.requiredValue(mcpKeyCommand, m.Command)
		if !ok {
			return nil, append(diags, interp.diags...), false
		}
		entry[mcpKeyCommand] = command
		if m.Args != nil {
			entry[mcpKeyArgs] = interp.slice(mcpKeyArgs, m.Args)
		}
		if m.Env != nil {
			entry[mcpKeyEnv] = interp.stringMap(mcpKeyEnv, m.Env)
		}
		diags = append(diags, droppedFieldDiagnostics(name, source, mcpRemoteOnlyKeys, map[string]bool{
			mcpKeyURL:               m.URL != "",
			mcpKeyTransport:         m.Transport != "",
			mcpKeyHeaders:           m.Headers != nil,
			mcpKeyOAuthClientID:     m.OAuthClientID != "",
			mcpKeyOAuthClientSecret: m.OAuthClientSecret != "",
			mcpKeyOAuthResource:     m.OAuthResource != "",
		})...)
	} else {
		url, ok := interp.requiredValue(mcpKeyURL, m.URL)
		if !ok {
			return nil, append(diags, interp.diags...), false
		}
		entry[mcpKeyURL] = url
		if m.Transport != "" {
			// Validated against the documented vocabulary in
			// mcpFormDiagnostics, and never interpolated: it is a protocol
			// identifier, not a value.
			entry[mcpKeyTransport] = m.Transport
		}
		if m.Headers != nil {
			entry[mcpKeyHeaders] = interp.stringMap(mcpKeyHeaders, m.Headers)
		}
		// The OAuth trio is typed in the IR (ir.McpServer.OAuthClientID,
		// OAuthClientSecret, OAuthResource) and emitted from those typed fields
		// only. Evidence: "The IR preserves explicit transport and typed OAuth
		// client ID, client secret, and resource rather than relying on runtime
		// fallback or target extras."
		for _, oauth := range []struct {
			key   string
			value string
		}{
			{mcpKeyOAuthClientID, m.OAuthClientID},
			{mcpKeyOAuthClientSecret, m.OAuthClientSecret},
			{mcpKeyOAuthResource, m.OAuthResource},
		} {
			if oauth.value == "" {
				continue
			}
			if mapped, ok := interp.value(oauth.key, oauth.value); ok {
				entry[oauth.key] = mapped
			}
		}
		diags = append(diags, droppedFieldDiagnostics(name, source, mcpStdioOnlyKeys, map[string]bool{
			mcpKeyArgs: m.Args != nil,
			mcpKeyEnv:  m.Env != nil,
			// command is impossible here: a non-empty command would have
			// selected the stdio form or the ambiguous skip.
		})...)
	}

	if m.DisabledSet || m.Disabled {
		// Emitted whenever the source expressed an opinion, INCLUDING an
		// explicit false. Plan/matrix: "disabled including false". Treating an
		// explicit `false` as absent would be harmless today, but treating an
		// explicit `true` as absent would enable a server the source disabled,
		// so the flag is always honored.
		entry[mcpKeyDisabled] = m.Disabled
	}

	return entry, append(diags, interp.diags...), true
}

// droppedFieldDiagnostics reports each modeled field that belongs to the server
// form this resource did not select. present is keyed by field name so the
// report order follows the fixed `keys` order and is deterministic.
//
// Only the field NAME reaches the diagnostic and the log. env values, header
// values, and the OAuth secret are exactly the payloads the plan forbids
// quoting, and this is the branch most likely to be tempted into quoting them.
func droppedFieldDiagnostics(name string, source diag.SourceRef, keys []string, present map[string]bool) []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, key := range keys {
		if !present[key] {
			continue
		}
		slog.Warn("devin mcp field dropped", "target", mapping.TargetDevin, "resource", name, "field", key)
		diags = append(diags, mcpLossDiagnostic(diag.CodeMcpDevinFieldDropped, name, source,
			fmt.Sprintf("Devin's documented MCP server forms are disjoint, so the %q field does not apply to this server and was dropped", key),
			"Move the field to a server of the form that documents it: stdio servers document command, args, and env; remote servers document url, transport, headers, and the oauth fields",
		))
	}
	return diags
}

// mcpInterpolator rewrites canonical interpolation tokens into Devin syntax and
// accumulates the losses.
//
// It is a small struct rather than a set of free functions so that the
// per-field diagnostics stay deduplicated (one diagnostic per field, not one
// per map entry) and in a deterministic order without every caller threading a
// slice through.
type mcpInterpolator struct {
	resource string
	source   diag.SourceRef
	opts     compile.Options
	diags    []diag.Diagnostic
	// unsupported and unresolved record the FIELD NAMES already reported, so a
	// map with ten bad values still yields one diagnostic per field.
	unsupported map[string]bool
	unresolved  map[string]bool
}

// mapValue resolves the plugin-root token and then maps every interpolation
// token, WITHOUT deciding how the loss is reported. The two callers below
// differ only in that decision, which is the whole point of the split: the
// mapping is one behaviour, the severity follows the caller's outcome.
//
// field is a fixed member name used for reporting; the VALUE is never reported.
func (i *mcpInterpolator) mapValue(field, value string) (string, bool) {
	resolved, unresolvedRoot := resolvePluginRootToken(value, i.opts)
	if unresolvedRoot {
		i.reportUnresolvedPluginRoot(field)
	}
	return mapMcpInterpolation(resolved)
}

// value maps one scalar field value whose omission still leaves a usable entry
// — an argument, an env or header value, or one of the OAuth members. ok is
// false when the value carries an interpolation style Devin cannot represent;
// the caller then omits that field rather than writing an empty string, and
// the loss is a WARNING because the entry is still emitted.
func (i *mcpInterpolator) value(field, value string) (string, bool) {
	mapped, ok := i.mapValue(field, value)
	if !ok {
		i.reportUnsupportedInterpolation(field)
		return "", false
	}
	return mapped, true
}

// requiredValue maps the one field the selected server form cannot do without:
// command on a stdio server, url on a remote server. When it cannot be
// represented the caller drops the whole entry, so the loss is an ERROR and
// sync exits non-zero (bead agent-marketplace-5exj.23).
//
// It is called at most once per entry — the two fields select mutually
// exclusive forms and mcpFormDiagnostics has already refused a server that
// declares both — so unlike the warning path it needs no per-field dedup.
func (i *mcpInterpolator) requiredValue(field, value string) (string, bool) {
	mapped, ok := i.mapValue(field, value)
	if !ok {
		i.reportUnrepresentableRequiredValue(field)
		return "", false
	}
	return mapped, true
}

// slice maps every element of an argument list. An element that cannot be
// represented is DROPPED from the list rather than replaced with an empty
// string: an empty argument is a real, different argument and would change the
// command line.
func (i *mcpInterpolator) slice(field string, values []string) []string {
	mapped := make([]string, 0, len(values))
	for _, value := range values {
		if element, ok := i.value(field, value); ok {
			mapped = append(mapped, element)
		}
	}
	return mapped
}

// stringMap maps every value of an env or header map. A key whose value cannot
// be represented is omitted entirely; writing an empty value would hand the
// server a defined-but-blank variable or header, which is a different (and
// possibly authentication-bypassing) input than the one the author wrote.
//
// Keys are visited in sorted order so a partially droppable map produces the
// same result on every run. The keys themselves are variable and header NAMES
// and are never logged.
func (i *mcpInterpolator) stringMap(field string, values map[string]string) map[string]string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	mapped := make(map[string]string, len(values))
	for _, key := range keys {
		if value, ok := i.value(field, values[key]); ok {
			mapped[key] = value
		}
	}
	return mapped
}

func (i *mcpInterpolator) reportUnsupportedInterpolation(field string) {
	if i.unsupported[field] {
		return
	}
	if i.unsupported == nil {
		i.unsupported = make(map[string]bool, 2)
	}
	i.unsupported[field] = true
	slog.Warn("devin mcp interpolation unsupported", "target", mapping.TargetDevin, "resource", i.resource, "field", field)
	i.diags = append(i.diags, mcpLossDiagnostic(diag.CodeMcpInterpolationUnsupported, i.resource, i.source,
		fmt.Sprintf("Devin has no equivalent for the file interpolation style used in the %q field; the value was omitted rather than written empty", field),
		"Devin documents ${env:VAR} interpolation only; move the file contents into an environment variable",
	))
}

// reportUnrepresentableRequiredValue reports the entry-removing form of the
// same loss. It is a separate CODE rather than a second severity for
// mcp.interpolation-unsupported so that a catalogued code keeps meaning one
// thing: the shared code always means "one value was omitted from an entry
// that was still written", and this one always means "the entry is gone".
func (i *mcpInterpolator) reportUnrepresentableRequiredValue(field string) {
	slog.Error("devin mcp required value unrepresentable", "target", mapping.TargetDevin, "resource", i.resource, "field", field)
	i.diags = append(i.diags, mcpSkipDiagnostic(diag.CodeMcpDevinInterpolationUnrepresentable, i.resource, i.source,
		fmt.Sprintf("Devin has no equivalent for the file interpolation style used in the %q field, which the server's documented form cannot do without; the server entry was skipped", field),
		"Devin documents ${env:VAR} interpolation only; move the value into an environment variable, or use a literal command or url",
	))
}

func (i *mcpInterpolator) reportUnresolvedPluginRoot(field string) {
	if i.unresolved[field] {
		return
	}
	if i.unresolved == nil {
		i.unresolved = make(map[string]bool, 2)
	}
	i.unresolved[field] = true
	slog.Warn("devin mcp plugin root unresolved", "target", mapping.TargetDevin, "resource", i.resource, "field", field)
	i.diags = append(i.diags, mcpLossDiagnostic(diag.CodeMcpDevinPluginRootUnresolved, i.resource, i.source,
		fmt.Sprintf("the %q field references the plugin root, but Devin has no plugin-root token and no plugin root was configured; the token was left unexpanded", field),
		"Compile with a plugin root so the value resolves, or use an absolute path",
	))
}

// mapMcpInterpolation rewrites every complete canonical interpolation token in
// value into the Devin cell of the mapping table.
//
// It routes through mapping.McpInterpolationSupported for each of the three
// declared source styles, so the Devin column stays the single source of truth:
//
//	inline `${VAR}` -> `${env:VAR}`
//	`${env:VAR}`    -> `${env:VAR}`
//	`${file:PATH}`  -> unmappable (ok=false)
//
// ok is false as soon as one token is unmappable. The caller omits the whole
// value: a partially rewritten value would silently drop the part the author
// used to inject a secret.
//
// `${CLAUDE_PLUGIN_ROOT}` is deliberately excluded from the inline rule. By the
// time this runs, resolvePluginRootToken has either expanded it or left it in
// place with a diagnostic; rewriting the leftover to `${env:CLAUDE_PLUGIN_ROOT}`
// would turn a reported failure into a silent lookup of an environment variable
// Devin never sets.
func mapMcpInterpolation(value string) (string, bool) {
	if !strings.Contains(value, "${") {
		return value, true
	}
	ok := true
	mapped := mcpInterpolationToken.ReplaceAllStringFunc(value, func(token string) string {
		inner := token[2 : len(token)-1]
		switch {
		case strings.HasPrefix(inner, "file:"):
			pattern, supported := mapping.McpInterpolationSupported("${file:PATH}", mapping.TargetDevin)
			if !supported {
				ok = false
				return token
			}
			return strings.Replace(pattern, "PATH", strings.TrimPrefix(inner, "file:"), 1)
		case strings.HasPrefix(inner, "env:"):
			pattern, supported := mapping.McpInterpolationSupported("${env:VAR}", mapping.TargetDevin)
			if !supported {
				ok = false
				return token
			}
			return strings.Replace(pattern, "VAR", strings.TrimPrefix(inner, "env:"), 1)
		case inner == pluginRootName:
			return token
		default:
			pattern, supported := mapping.McpInterpolationSupported("inline", mapping.TargetDevin)
			if !supported {
				ok = false
				return token
			}
			return strings.Replace(pattern, "VAR", inner, 1)
		}
	})
	if !ok {
		return "", false
	}
	return mapped, true
}

// mcpArtifact wraps one server entry in the shared mcp_config.json envelope.
//
// The path is identical for both scopes; see the SCOPE note on
// translateMcpServer. The artifact is a fragment so every selected MCP server
// contributes one keyed entry to one shared file, and it deliberately does not
// set DedupeFragmentEntries (see mcpEntryIdentity).
func mcpArtifact(name string, entry map[string]any, opts compile.Options) (artifact.Artifact, error) {
	document := map[string]any{mcpServersKey: map[string]any{name: entry}}
	bytes, err := json.Marshal(document)
	if err != nil {
		// The error deliberately omits the document: it holds env values,
		// headers, and the OAuth client secret.
		return artifact.Artifact{}, fmt.Errorf("devin: marshal mcp fragment for %s: %w", mcpConfig, err)
	}

	art := artifact.Artifact{
		Path:                           mcpConfig,
		Bytes:                          bytes,
		Fragment:                       true,
		FragmentKey:                    mcpConfig,
		ExternalReconciliationRequired: true,
	}
	return art, nil
}

// mcpSkipDiagnostic builds the diagnostic for a server definition that removes
// the whole keyed entry. It is an ERROR so the run exits non-zero: the plan
// requires that "invalid or conflicting desired entries are skipped ... sync
// still exits non-zero". The contract and its justification live with the
// mcp.devin.* constants in diag/codes.go.
func mcpSkipDiagnostic(code, resource string, source diag.SourceRef, message, hint string) diag.Diagnostic {
	return mcpDiagnostic(diag.SeverityError, code, resource, source, message, hint)
}

// mcpLossDiagnostic builds the diagnostic for a loss that still EMITS the
// server entry — an inapplicable modeled field was dropped, or a plugin-root
// token was left unexpanded inside a value. It stays a WARNING: the entry Devin
// receives is otherwise exactly the one the source authorized, and suppressing
// it would remove a working server over an inert field.
func mcpLossDiagnostic(code, resource string, source diag.SourceRef, message, hint string) diag.Diagnostic {
	return mcpDiagnostic(diag.SeverityWarning, code, resource, source, message, hint)
}

func mcpDiagnostic(severity diag.Severity, code, resource string, source diag.SourceRef, message, hint string) diag.Diagnostic {
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
