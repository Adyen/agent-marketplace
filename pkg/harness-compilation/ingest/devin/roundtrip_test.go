package devin_test

// This file is the Devin semantic round-trip matrix required by work package D8
// ("semantic round-trip matrices cover skills, commands, flat agents, hooks, and
// MCP"). It lives in an external test package because it drives the whole
// pipeline through the root harnesscompile package, which imports the ingester
// under test.
//
// # What "semantic round trip" means here
//
// The plan defines the guarantee as IR stability, not byte stability: "Ingest
// canonical Devin input. Represent supported meaning in IR. Compile back to
// Devin deterministically. Re-ingest to equivalent IR." So the assertion is
// IR₁ == IR₂ under a per-kind equality relation, and the relation is written out
// below rather than left to reflect.DeepEqual on the whole struct, because two
// IR fields are deliberately excluded from every kind:
//
//   - ir.SourceRef. It records WHERE a resource was read from. The compiled
//     output lives at a different path than a workspace input (the compiler
//     emits relative to the Devin target root, so `.devin/skills/x/SKILL.md`
//     becomes `skills/x/SKILL.md`), and that is a fact about the layout, not
//     about the resource's meaning.
//   - ir.Ext. The target-specific sidecar bag is how OTHER harnesses carry
//     native fields they cannot express canonically. Devin uses none, and this
//     ingester never sets one; TestDevinIngestSetsNoTargetSidecar pins that
//     separately, which is stronger than folding it into every comparison.
//
// Everything else is compared exactly, INCLUDING nil-versus-empty for every
// slice and map, because on this target the difference between "absent" and
// "explicitly empty" is the difference between Devin's permissive default and
// the narrowest possible restriction.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

const roundTripPluginRoot = "/plugins/demo"

// semantic is the comparison shape for one resource. One struct covers all five
// kinds so the matrix can diff any two resources field by field and NAME the
// fields that differ, which is what turns a loss into an explicit matrix entry
// instead of an opaque mismatch.
//
// Every field is a pointer, slice, or map where nil-versus-empty matters, and
// nothing is tagged omitempty for those cases: an omitted key and a key holding
// an empty list must not compare equal.
type semantic struct {
	Kind string `json:"kind"`
	Name string `json:"name"`

	// Skill, command, subagent
	Description  string         `json:"description"`
	Body         string         `json:"body"`
	ArgumentHint string         `json:"argumentHint,omitempty"`
	Model        string         `json:"model,omitempty"`
	Extra        map[string]any `json:"extra"`

	// Skill
	Triggers       *[]string `json:"triggers"`
	AllowedTools   *[]string `json:"allowedTools"`
	Permissions    *[]string `json:"permissions"`
	PermissionsSet bool      `json:"permissionsSet,omitempty"`
	Subagent       string    `json:"subagent,omitempty"`
	Agent          string    `json:"agent,omitempty"`
	SupportFiles   []string  `json:"supportFiles"`

	// Subagent
	Tools           *[]string `json:"tools"`
	Effort          string    `json:"effort,omitempty"`
	DisallowedTools *[]string `json:"disallowedTools"`
	Isolation       string    `json:"isolation,omitempty"`
	MaxNesting      *int      `json:"maxNesting"`

	// Hook: the behavior-identity tuple compile/devin documents as
	// (devin event, matcher, type, selected payload, timeout, emitted extras).
	// The IR carries both payload fields, so both are compared: a round trip
	// that moved a command into the prompt slot would change what Devin runs.
	Event   string `json:"event,omitempty"`
	Matcher string `json:"matcher,omitempty"`
	Type    string `json:"type,omitempty"`
	Command string `json:"command,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Timeout *int   `json:"timeout"`

	// MCP server
	Args              *[]string          `json:"args"`
	Env               *map[string]string `json:"env"`
	URL               string             `json:"url,omitempty"`
	Transport         string             `json:"transport,omitempty"`
	Headers           *map[string]string `json:"headers"`
	Disabled          bool               `json:"disabled,omitempty"`
	DisabledSet       bool               `json:"disabledSet,omitempty"`
	OAuthClientID     string             `json:"oauthClientId,omitempty"`
	OAuthClientSecret string             `json:"oauthClientSecret,omitempty"`
	OAuthResource     string             `json:"oauthResource,omitempty"`
}

// extras normalizes the preserved-extension bag.
//
// An absent bag and an empty bag are treated as EQUAL, unlike every restriction
// list in this struct. That is not laziness: `Extra` is not a policy, it is
// "the unknown fields that were present", and zero unknown fields is one state
// however a loader spells it. bundle.LoadHooks materializes an empty map while
// this ingester leaves nil, and treating that as a loss would report a
// difference no user can observe or act on.
func extras(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	return values
}

func semanticOf(t *testing.T, resource ir.Resource) semantic {
	t.Helper()
	switch typed := resource.(type) {
	case *ir.Skill:
		return semantic{
			Kind: "skill", Name: typed.Name(), Description: typed.Description, Body: typed.Body,
			ArgumentHint: typed.ArgumentHint, Model: typed.Model, Extra: extras(typed.Extra),
			Triggers: ptrSlice(typed.Triggers), AllowedTools: ptrSlice(typed.AllowedTools),
			Permissions: permissionSummary(typed), PermissionsSet: typed.PermissionsSet,
			Subagent: typed.Subagent, Agent: typed.Agent, SupportFiles: typed.SupportFiles,
		}
	case *ir.Command:
		return semantic{Kind: "command", Name: typed.Name(), Description: typed.Description, Body: typed.Body, Extra: extras(typed.Extra)}
	case *ir.Subagent:
		return semantic{
			Kind: "subagent", Name: typed.Name(), Description: typed.Description, Body: typed.Body,
			Model: typed.Model, Extra: extras(typed.Extra), Tools: ptrSlice(typed.Tools), Effort: typed.Effort,
			DisallowedTools: ptrSlice(typed.DisallowedTools), Isolation: typed.Isolation, MaxNesting: typed.MaxNesting,
		}
	case *ir.Hook:
		return semantic{
			Kind: "hook", Name: typed.Name(), Event: typed.Event, Matcher: typed.Matcher, Type: typed.Type,
			Command: typed.Command, Prompt: typed.Prompt, Timeout: typed.Timeout, Extra: extras(typed.Extra),
		}
	case *ir.McpServer:
		return semantic{
			Kind: "mcpserver", Name: typed.Name(), Command: typed.Command, Args: ptrSlice(typed.Args),
			Env: ptrMap(typed.Env), URL: typed.URL, Transport: typed.Transport, Headers: ptrMap(typed.Headers),
			Disabled: typed.Disabled, DisabledSet: typed.DisabledSet, OAuthClientID: typed.OAuthClientID,
			OAuthClientSecret: typed.OAuthClientSecret, OAuthResource: typed.OAuthResource, Extra: extras(typed.Extra),
		}
	default:
		t.Fatalf("unsupported resource type %T", resource)
		return semantic{}
	}
}

// ptrSlice preserves the nil-versus-empty distinction through JSON: a nil slice
// becomes a JSON null and an empty slice becomes `[]`, so "absent restriction"
// and "explicitly empty restriction" can never compare equal.
func ptrSlice(values []string) *[]string {
	if values == nil {
		return nil
	}
	copied := append([]string{}, values...)
	return &copied
}

func ptrMap(values map[string]string) *map[string]string {
	if values == nil {
		return nil
	}
	copied := map[string]string{}
	for key, value := range values {
		copied[key] = value
	}
	return &copied
}

// permissionSummary flattens the permission policy into a stable, comparable
// list. Sorting is deliberately absent: the ORDER of a permission list is part
// of the policy the plan forbids reordering.
func permissionSummary(skill *ir.Skill) *[]string {
	if !skill.PermissionsSet {
		return nil
	}
	summary := []string{}
	for _, group := range []struct {
		label  string
		values []string
	}{{"allow", skill.Permissions.Allow}, {"ask", skill.Permissions.Ask}, {"deny", skill.Permissions.Deny}} {
		if group.values == nil {
			continue
		}
		summary = append(summary, group.label+"=["+joinValues(group.values)+"]")
	}
	extraKeys := make([]string, 0, len(skill.Permissions.Extra))
	for key := range skill.Permissions.Extra {
		extraKeys = append(extraKeys, key)
	}
	slices.Sort(extraKeys)
	for _, key := range extraKeys {
		encoded, _ := json.Marshal(skill.Permissions.Extra[key])
		summary = append(summary, key+"="+string(encoded))
	}
	return &summary
}

func joinValues(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ","
		}
		out += value
	}
	return out
}

// devinRoundTripRoot is the canonical Devin workspace input for the identity
// matrix. It covers all five kinds and, per kind, the fields whose round trip
// the earlier work packages left open.
func devinRoundTripRoot() fstest.MapFS {
	return fstest.MapFS{
		// Skill: every typed field Devin represents, plus an extension field,
		// a support file, and a permission policy.
		".devin/skills/full-skill/SKILL.md": file(`---
name: full-skill
description: A fully populated Devin skill
triggers:
  - user
  - model
argument-hint: "<path>"
model: sonnet
allowed-tools:
  - read
  - edit
  - grep
  - glob
  - exec
permissions:
  allow:
    - Read
  ask:
    - Bash
  deny: []
subagent: explore
agent: reviewer
x-team: payments
---
Body of the full skill.
`),
		".devin/skills/full-skill/scripts/run.sh": file("#!/bin/sh\necho run\n"),

		// Skill: the explicit-empty forms. `triggers: []` is the shape D4
		// emits when every source trigger was unmappable, and it must stay an
		// explicit empty list rather than widening to Devin's default.
		".devin/skills/narrow-skill/SKILL.md": file("---\nname: narrow-skill\ndescription: Narrowest representable activation\ntriggers: []\nallowed-tools: []\n---\nBody\n"),

		// Command: the provenance marker plus the argument metadata and tool
		// restriction the command compiler reads from extras.
		".devin/skills/full-cmd/SKILL.md": file(`---
name: full-cmd
description: A converted command
x-agent-marketplace-kind: command
triggers:
  - user
argument-hint: "<issue>"
allowed-tools:
  - read
  - exec
x-origin: marketplace
---
Body of the command.
`),

		// Flat subagent: model, allowed-tools, and max-nesting, including the
		// explicit 0 the plan calls out as the strictest value.
		".devin/agents/full-agent.md": file("---\nname: full-agent\ndescription: A fully populated Devin subagent\nmodel: opus\nallowed-tools:\n  - read\n  - exec\nmax-nesting: 4\nx-owner: platform\n---\nBody of the agent.\n"),
		".devin/agents/leaf-agent.md": file("---\nname: leaf-agent\ndescription: No nested delegation at all\nmodel: haiku\nmax-nesting: 0\n---\nBody\n"),

		// Hooks: both types, a matcher on a tool event, an explicit zero
		// timeout, and a preserved extension member.
		".devin/hooks.v1.json": file(`{
  "PreToolUse": [
    {"type": "command", "command": "/plugins/demo/scripts/pre.sh", "matcher": "Read", "timeout": 30, "x-note": "kept"}
  ],
  "PostToolUse": [
    {"type": "command", "command": "/plugins/demo/scripts/post.sh", "matcher": "Edit", "timeout": 0}
  ],
  "SessionEnd": [
    {"type": "prompt", "prompt": "Summarize the session."}
  ]
}`),

		// MCP: both documented server forms, the OAuth trio, an explicit
		// disabled: false, and a preserved extension member.
		".devin/mcp_config.json": file(`{
  "mcpServers": {
    "remote-server": {
      "url": "https://example.invalid/mcp",
      "transport": "http",
      "headers": {"X-Client": "${env:CLIENT_ID}"},
      "oauthClientId": "client-id",
      "oauthClientSecret": "${env:OAUTH_CLIENT_SECRET}",
      "oauthResource": "https://example.invalid",
      "disabled": false,
      "x-note": "kept"
    },
    "stdio-server": {
      "command": "npx",
      "args": ["-y", "demo-mcp-server"],
      "env": {"TOKEN": "${env:TOKEN}"},
      "disabled": true
    }
  }
}`)}
}

func file(data string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(data)} }

// materializeNonOwningConversion is a test-only fixture for conversion tests
// whose isolated temporary destination has no owning consumer adapter. It
// first proves that generic Materialize refuses the complete result, then
// materializes ordinary artifacts and stages the externally reconciled files
// as a non-owning conversion publisher would. Convert has already merged
// fragments, so the aggregate bytes written here are the complete result.
func materializeNonOwningConversion(t *testing.T, result *hcc.Result, outDir string) {
	t.Helper()

	ordinary := &hcc.Result{Diagnostics: result.Diagnostics, Skipped: result.Skipped}
	var external []hcc.Artifact
	for _, candidate := range result.Artifacts {
		policy, err := candidate.Policy()
		if err != nil {
			t.Fatalf("artifact %q policy: %v", candidate.Path, err)
		}
		if policy.IsExternal() {
			external = append(external, candidate)
			continue
		}
		ordinary.Artifacts = append(ordinary.Artifacts, candidate)
	}
	if len(external) == 0 {
		if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
			t.Fatalf("Materialize ordinary conversion: %v", err)
		}
		return
	}

	if entries, err := os.ReadDir(outDir); err != nil {
		t.Fatalf("read empty conversion destination: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("conversion destination is not empty: %v", entries)
	}
	if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); !errors.Is(err, hcc.ErrExternalReconciliationRequired) {
		t.Fatalf("generic Materialize error = %v, want %v", err, hcc.ErrExternalReconciliationRequired)
	}
	if entries, err := os.ReadDir(outDir); err != nil {
		t.Fatalf("read destination after refused conversion: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("generic Materialize wrote %d entries before refusing conversion", len(entries))
	}

	if len(ordinary.Artifacts) > 0 {
		if _, err := hcc.Materialize(ordinary, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
			t.Fatalf("Materialize ordinary conversion artifacts: %v", err)
		}
	}
	for _, candidate := range external {
		if err := candidate.Validate(); err != nil {
			t.Fatalf("validate external conversion artifact %q: %v", candidate.Path, err)
		}
		path := filepath.Join(outDir, filepath.FromSlash(candidate.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create external conversion artifact directory %q: %v", filepath.Dir(path), err)
		}
		mode := candidate.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(path, candidate.Bytes, mode); err != nil {
			t.Fatalf("write external conversion artifact %q: %v", candidate.Path, err)
		}
	}
}

// TestDevinSemanticRoundTripMatrix is the identity half of the matrix: one
// canonical Devin workspace root, ingested, compiled back to Devin,
// materialized, and re-ingested, with per-kind semantic equality asserted for
// every resource.
//
// It is a matrix over KINDS rather than over harness pairs (the 6x6 harness
// matrix lives in pkg/harness-compilation/testdata/matrix): this is where the
// per-field, nil-versus-empty detail that a shared cross-harness fixture cannot
// express is pinned.
func TestDevinSemanticRoundTripMatrix(t *testing.T) {
	src := devinRoundTripRoot()

	first, err := hcc.IngestBundle(src, hcc.HarnessDevin)
	if err != nil {
		t.Fatalf("IngestBundle: %v", err)
	}
	for _, d := range first.Diagnostics {
		if d.Severity == hcc.SeverityError {
			t.Fatalf("ingest reported an error for canonical input: %s: %s", d.Code, d.Message)
		}
	}

	outDir := t.TempDir()
	result, err := hcc.Convert(src, hcc.HarnessDevin, hcc.HarnessDevin, hcc.WithPluginRoot(roundTripPluginRoot))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, d := range result.Diagnostics {
		if d.Severity == hcc.SeverityError {
			t.Fatalf("compile reported an error for canonical input: %s: %s", d.Code, d.Message)
		}
	}
	materializeNonOwningConversion(t, result, outDir)

	// The materialized output is the DIRECT root shape, because the Devin
	// compiler writes relative to the target root rather than under `.devin/`.
	// Autodetection must never call that shape Devin — it is exactly the
	// ambiguity the plan describes, since a bare `skills/` plus `agents/` is
	// equally a Claude plugin, and here autodetection does in fact answer
	// "claude". Only an explicit selection may read it as Devin, which is what
	// the re-ingest below does. Asserting both makes the matrix itself exercise
	// the user-root invariant.
	if detected, ok := hcc.Autodetect(os.DirFS(outDir)); ok && detected == hcc.HarnessDevin {
		t.Fatal("Autodetect recognized a direct Devin root as devin; the plan requires explicit selection for that shape")
	}
	second, err := hcc.IngestBundle(os.DirFS(outDir), hcc.HarnessDevin)
	if err != nil {
		t.Fatalf("re-ingest: %v", err)
	}

	before, beforeAliases := semanticIndex(t, first.Resources)
	after, _ := semanticIndex(t, second.Resources)

	kinds := []struct {
		kind    string
		marquee string
		// allowedLosses names the semantic fields that are NOT expected to
		// survive even an identity round trip on this target. Each one is
		// justified here and pinned by its own dedicated test, so the exclusion
		// cannot quietly grow.
		allowedLosses []string
	}{
		{kind: "skill", marquee: "full-skill",
			// Support-file NAMES are recovered by re-walking the compiled skill
			// directory, but the compile layer never copies the BYTES: it emits
			// skill.support-files-deferred and leaves materialization of
			// source-relative files to the owning layer. So the re-ingested
			// skill directory contains only SKILL.md and reports no support
			// files. This is a harness-independent property of the compile
			// layer, not a Devin mapping loss, and it is pinned by
			// TestDevinSupportFileBytesAreDeferredNotIngested.
			allowedLosses: []string{"supportFiles"}},
		{kind: "command", marquee: "full-cmd"},
		{kind: "subagent", marquee: "full-agent"},
		{kind: "hook", marquee: "PreToolUse-command"},
		{kind: "mcpserver", marquee: "stdio-server"},
	}
	for _, tt := range kinds {
		t.Run(tt.kind, func(t *testing.T) {
			if _, ok := beforeAliases[tt.marquee]; !ok {
				t.Fatalf("the %s fixture did not ingest; keys = %v", tt.kind, sortedKeys(beforeAliases))
			}
			compared := 0
			for key, want := range before {
				if want.Kind != tt.kind {
					continue
				}
				got, ok := after[key]
				if !ok {
					t.Errorf("%s %q did not survive the round trip; keys = %v", tt.kind, key, sortedKeys(after))
					continue
				}
				assertSemanticEqual(t, key, want, got, tt.allowedLosses)
				compared++
			}
			if compared == 0 {
				t.Fatalf("no %s resource was compared; the matrix cell would be vacuous", tt.kind)
			}
		})
	}

	// Non-vacuity: the fixture must still cover everything it claims to, and no
	// resource may appear or disappear across the round trip.
	if len(first.Resources) != len(second.Resources) {
		t.Fatalf("resource count changed: %d before, %d after (%v vs %v)",
			len(first.Resources), len(second.Resources), sortedKeys(before), sortedKeys(after))
	}
	// 2 skills + 1 command + 2 subagents + 3 hooks + 2 MCP servers.
	if len(first.Resources) != 10 {
		t.Fatalf("the round-trip fixture ingested %d resources, want 10", len(first.Resources))
	}
}

// TestDevinRoundTripIsDeterministic pins that a second identical run produces
// byte-identical artifacts. Without it, the semantic assertion above could pass
// on a compiler whose output depends on map iteration order.
func TestDevinRoundTripIsDeterministic(t *testing.T) {
	src := devinRoundTripRoot()
	var first map[string]string
	for run := 0; run < 5; run++ {
		result, err := hcc.Convert(src, hcc.HarnessDevin, hcc.HarnessDevin, hcc.WithPluginRoot(roundTripPluginRoot))
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		bytesByPath := map[string]string{}
		for _, artifact := range result.Artifacts {
			bytesByPath[artifact.Path] = string(artifact.Bytes)
		}
		if run == 0 {
			first = bytesByPath
			continue
		}
		if !reflect.DeepEqual(bytesByPath, first) {
			t.Fatalf("run %d produced different artifacts", run)
		}
	}
	if len(first) == 0 {
		t.Fatal("the round-trip fixture produced no artifacts")
	}
}

// devinLossMatrix enumerates every KNOWN IRREVERSIBLE case of
// `other harness -> Devin -> IR`. Each entry names the fields that do not
// survive, and the test asserts the observed loss set EXACTLY: a new loss fails,
// and a loss that quietly stops happening fails too, so no entry can rot into a
// vacuous pass.
var devinLossMatrix = []struct {
	name string
	// source is a Claude-layout bundle, so the "before" IR is the canonical one.
	source fstest.MapFS
	// key identifies the resource in both bundles.
	key string
	// losses are the semantic field names that differ after the round trip.
	losses []string
	// why documents the reason the loss is unavoidable on this target.
	why string
}{
	{
		name: "a stdio MCP server loses headers and the OAuth trio",
		// Recorded by D7: "A stdio MCP server carrying headers/oauth* loses
		// them on Devin, so Claude -> Devin -> IR is not fully reversible."
		// Devin's two documented server forms are DISJOINT, so there is
		// nowhere on a stdio entry to put a header or an OAuth credential.
		// The drop is reported per field with mcp.devin.field-dropped.
		source: fstest.MapFS{".mcp.json": file(`{"mcpServers":{"hybrid":{"command":"npx","args":["-y","x"],` +
			`"headers":{"X-Client":"literal"},"oauthClientId":"id","oauthClientSecret":"secret","oauthResource":"https://example.invalid"}}}`)},
		key:    "mcpserver/hybrid",
		losses: []string{"headers", "oauthClientId", "oauthClientSecret", "oauthResource"},
		why:    "Devin's stdio and remote server schemas are disjoint; a stdio entry has no header or OAuth member",
	},
	{
		name: "a hook command loses the plugin-root token",
		// Devin documents no plugin-root interpolation of its own (see
		// compile/mapping/envvars.go), so the compiler resolves the token at
		// compile time. The resolved absolute path is correct but no longer
		// portable, and the token cannot be recovered from it.
		source: fstest.MapFS{"hooks/hooks.json": file(`{"hooks":{"Stop":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/check.sh"}]}}`)},
		key:    "hook/Stop-command",
		losses: []string{"command"},
		why:    "Devin has no plugin-root token, so ${CLAUDE_PLUGIN_ROOT} is resolved to an absolute path at compile time",
	},
	{
		name:   "an MCP env value loses the plugin-root token",
		source: fstest.MapFS{".mcp.json": file(`{"mcpServers":{"demo":{"command":"npx","env":{"CONFIG":"${CLAUDE_PLUGIN_ROOT}/.env"}}}}`)},
		key:    "mcpserver/demo",
		losses: []string{"env"},
		why:    "same plugin-root resolution as hook commands, applied to env values",
	},
	{
		name: "unmappable skill triggers collapse to an explicit empty list",
		// D4 emits `triggers: []` when NO source trigger maps, because omitting
		// the key would widen activation to Devin's user+model default. The
		// original trigger names are gone; what survives is the narrowest
		// representable activation, and the ingester preserves exactly that.
		source: fstest.MapFS{"skills/demo-skill/SKILL.md": file("---\nname: demo-skill\ndescription: d\ntriggers:\n  - click\n  - mention\n---\nBody\n")},
		key:    "skill/demo-skill",
		losses: []string{"triggers"},
		why:    "Devin's activation vocabulary is user/model only; unmappable triggers are dropped with a warning and the narrowest representable value is emitted",
	},
	{
		name:   "a versioned model id is dropped rather than guessed",
		source: fstest.MapFS{"skills/demo-skill/SKILL.md": file("---\nname: demo-skill\ndescription: d\nmodel: claude-sonnet-4-5-20250929\n---\nBody\n")},
		key:    "skill/demo-skill",
		losses: []string{"model"},
		why:    "only stable Devin aliases are mapping targets; the plan forbids guessing a versioned model id",
	},
	{
		name:   "a subagent effort level is dropped",
		source: fstest.MapFS{"agents/demo-agent.md": file("---\nname: demo-agent\ndescription: d\nmodel: sonnet\neffort: high\n---\nBody\n")},
		key:    "subagent/demo-agent",
		losses: []string{"effort"},
		why:    "Devin has no subagent effort field; effort grants no capability, so it is dropped with a warning rather than failing the resource",
	},
	{
		name: "a command loses everything except name, description, body, and extras",
		// A Claude command has no other typed fields, so this entry exists to
		// prove the opposite of a loss: the command round trip is complete.
		source: fstest.MapFS{"commands/demo-cmd.md": file("---\nname: demo-cmd\ndescription: A demo command\n---\nRun it\n")},
		key:    "command/demo-cmd",
		losses: nil,
		why:    "commands round-trip completely through the x-agent-marketplace-kind provenance marker",
	},
	{
		name:   "a flat subagent round-trips model, allowed tools, and max nesting",
		source: fstest.MapFS{"agents/demo-agent.md": file("---\nname: demo-agent\ndescription: d\nmodel: sonnet\ntools:\n  - Read\n  - Bash\nmax-nesting: 2\n---\nBody\n")},
		key:    "subagent/demo-agent",
		losses: nil,
		why:    "max-nesting is a typed Subagent IR field that Devin represents natively",
	},
}

// TestDevinLossMatrix asserts each declared loss set exactly.
func TestDevinLossMatrix(t *testing.T) {
	for _, tt := range devinLossMatrix {
		t.Run(tt.name, func(t *testing.T) {
			before, err := hcc.IngestBundle(tt.source, hcc.HarnessClaude)
			if err != nil {
				t.Fatalf("IngestBundle(claude): %v", err)
			}
			result, err := hcc.Convert(tt.source, hcc.HarnessClaude, hcc.HarnessDevin, hcc.WithPluginRoot(roundTripPluginRoot))
			if err != nil {
				t.Fatalf("Convert(claude->devin): %v", err)
			}
			outDir := t.TempDir()
			materializeNonOwningConversion(t, result, outDir)
			after, err := hcc.IngestBundle(os.DirFS(outDir), hcc.HarnessDevin)
			if err != nil {
				t.Fatalf("IngestBundle(devin): %v", err)
			}

			beforeIndex, _ := semanticIndex(t, before.Resources)
			want, ok := beforeIndex[tt.key]
			if !ok {
				t.Fatalf("source resource %q not found; keys = %v", tt.key, sortedKeys(beforeIndex))
			}
			afterIndex, _ := semanticIndex(t, after.Resources)
			got, ok := afterIndex[tt.key]
			if !ok {
				t.Fatalf("round-tripped resource %q not found; keys = %v", tt.key, sortedKeys(afterIndex))
			}

			losses := semanticDiff(t, want, got)
			if !reflect.DeepEqual(losses, tt.losses) {
				t.Fatalf("observed loss set = %v, declared = %v (%s)", losses, tt.losses, tt.why)
			}
		})
	}
}

// TestDevinSupportFileBytesAreDeferredNotIngested pins the one allowed loss the
// identity matrix declares for skills, so the exclusion is never vacuous.
//
// The ingester DOES recover support-file names from the source layout; what the
// compile layer does not do is copy their bytes, which it reports with
// skill.support-files-deferred. Both halves are asserted here: the name is seen
// on the way in, the diagnostic is emitted, and the file is absent from the
// materialized output.
func TestDevinSupportFileBytesAreDeferredNotIngested(t *testing.T) {
	src := fstest.MapFS{
		".devin/skills/demo-skill/SKILL.md":       file("---\nname: demo-skill\ndescription: d\n---\nBody\n"),
		".devin/skills/demo-skill/scripts/run.sh": file("#!/bin/sh\n"),
	}

	before, err := hcc.IngestBundle(src, hcc.HarnessDevin)
	if err != nil {
		t.Fatalf("IngestBundle: %v", err)
	}
	skill, ok := before.Resources[0].(*ir.Skill)
	if !ok {
		t.Fatalf("resource type = %T, want *ir.Skill", before.Resources[0])
	}
	if !reflect.DeepEqual(skill.SupportFiles, []string{"scripts/run.sh"}) {
		t.Fatalf("SupportFiles = %v, want [scripts/run.sh]", skill.SupportFiles)
	}

	result, err := hcc.Convert(src, hcc.HarnessDevin, hcc.HarnessDevin, hcc.WithPluginRoot(roundTripPluginRoot))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	var deferred bool
	for _, d := range result.Diagnostics {
		if d.Code == "skill.support-files-deferred" {
			deferred = true
			if d.Severity != hcc.SeverityWarning {
				t.Errorf("skill.support-files-deferred severity = %v, want warning", d.Severity)
			}
			if d.Resource != "demo-skill" {
				t.Errorf("skill.support-files-deferred resource = %q, want demo-skill", d.Resource)
			}
		}
	}
	if !deferred {
		t.Fatal("no skill.support-files-deferred diagnostic; the allowed loss is undocumented at runtime")
	}

	outDir := t.TempDir()
	if _, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeOverwrite)); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if _, err := os.Stat(outDir + "/skills/demo-skill/scripts/run.sh"); err == nil {
		t.Fatal("the compile layer materialized a support file; the allowed loss no longer exists and must be removed from the matrix")
	}
}

// TestDevinIngestSetsNoTargetSidecar pins the assumption the semantic relation
// above depends on: this ingester never populates ir.Ext, so excluding the
// sidecar bag from the comparison cannot hide a loss.
func TestDevinIngestSetsNoTargetSidecar(t *testing.T) {
	bundle, err := hcc.IngestBundle(devinRoundTripRoot(), hcc.HarnessDevin)
	if err != nil {
		t.Fatalf("IngestBundle: %v", err)
	}
	if len(bundle.Resources) == 0 {
		t.Fatal("fixture ingested nothing")
	}
	for _, resource := range bundle.Resources {
		var empty bool
		switch typed := resource.(type) {
		case *ir.Skill:
			empty = typed.Ext.Gemini == nil && typed.Ext.Windsurf == nil
		case *ir.Command:
			empty = typed.Ext.Gemini == nil && typed.Ext.Windsurf == nil
		case *ir.Subagent:
			empty = typed.Ext.Gemini == nil && typed.Ext.Windsurf == nil
		case *ir.Hook:
			empty = typed.Ext.Gemini == nil && typed.Ext.Windsurf == nil
		case *ir.McpServer:
			empty = typed.Ext.Gemini == nil && typed.Ext.Windsurf == nil
		default:
			t.Fatalf("unexpected resource type %T", resource)
		}
		if !empty {
			t.Errorf("%s %q carries a target sidecar; the Devin ingester must not set one", resource.Kind(), resource.Name())
		}
	}
}

// --- comparison helpers ---

// semanticIndex returns the prefixed index (one entry per resource) and a
// bare-name alias index used only for marquee lookups.
func semanticIndex(t *testing.T, resources []ir.Resource) (map[string]semantic, map[string]semantic) {
	t.Helper()
	index := make(map[string]semantic, len(resources))
	aliases := make(map[string]semantic, len(resources))
	for _, resource := range resources {
		value := semanticOf(t, resource)
		// Hook and MCP names are unique within a bundle, and the kind prefix
		// keeps a skill and a command with the same name distinguishable.
		key := value.Kind + "/" + value.Name
		if _, duplicate := index[key]; duplicate {
			t.Fatalf("duplicate resource key %q", key)
		}
		index[key] = value
		// The marquee lookups in the round-trip matrix address resources by
		// bare name, which is unambiguous in that fixture.
		if _, duplicate := aliases[value.Name]; duplicate {
			t.Fatalf("ambiguous marquee alias %q", value.Name)
		}
		aliases[value.Name] = value
	}
	return index, aliases
}

func sortedKeys(index map[string]semantic) []string {
	keys := make([]string, 0, len(index))
	for key := range index {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func assertSemanticEqual(t *testing.T, key string, want, got semantic, allowedLosses []string) {
	t.Helper()
	diffs := semanticDiff(t, want, got)
	diffs = slices.DeleteFunc(diffs, func(field string) bool { return slices.Contains(allowedLosses, field) })
	if len(diffs) > 0 {
		wantJSON := mustIndent(t, want)
		gotJSON := mustIndent(t, got)
		t.Errorf("%s did not round-trip semantically; differing fields = %v\n--- before ---\n%s\n--- after ---\n%s",
			key, diffs, wantJSON, gotJSON)
	}
}

func mustIndent(t *testing.T, value semantic) string {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal semantic: %v", err)
	}
	return string(data)
}

// semanticDiff returns the sorted names of the semantic fields that differ.
// Naming the fields is what lets the loss matrix declare an exact loss set
// instead of "these two structs are not equal".
func semanticDiff(t *testing.T, want, got semantic) []string {
	t.Helper()
	wantMap := semanticMap(t, want)
	gotMap := semanticMap(t, got)
	fields := map[string]struct{}{}
	for field := range wantMap {
		fields[field] = struct{}{}
	}
	for field := range gotMap {
		fields[field] = struct{}{}
	}
	var diffs []string
	for field := range fields {
		if !reflect.DeepEqual(wantMap[field], gotMap[field]) {
			diffs = append(diffs, field)
		}
	}
	slices.Sort(diffs)
	return diffs
}

func semanticMap(t *testing.T, value semantic) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal semantic: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal semantic: %v", err)
	}
	return decoded
}
