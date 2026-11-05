package harnesscompile

// Cross-target hook translation boundary tests. Every test exercises the public
// Compile/CompileBundle path and asserts the structured native artifact, not
// incidental scalar membership. Expected events, matchers, commands, and
// diagnostic codes are hand-authored fixture data; they are never derived
// from the shipped catalogs under test.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// hookEmission is one hook decoded from a compiled artifact. Where the native
// format groups handlers under a shared matcher (Factory, Devin, Gemini,
// Codex) the matcher is read from the group envelope; where the rule is flat
// (Claude, Cursor) it is read from the handler; Windsurf encodes the tool
// selection in the event name and emits no matcher member.
type hookEmission struct {
	event      string
	matcher    string
	hasMatcher bool
	command    string
	timeout    *int
}

// decodeHookArtifact parses the single JSON artifact a compiled hook produces
// for target and returns its native event plus the first handler. It fails
// the test when the artifact does not match the hand-authored shape for the
// target, so a changed envelope cannot silently satisfy these assertions.
func decodeHookArtifact(t *testing.T, target Target, raw []byte) hookEmission {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s artifact is not a JSON object: %v\n%s", target, err, raw)
	}

	singleKey := func(m map[string]any, context string) string {
		if len(m) != 1 {
			t.Fatalf("%s %s: want exactly one event key, got %v", target, context, m)
		}
		for k := range m {
			return k
		}
		return ""
	}
	// A single-emission artifact contains exactly one group per event and one
	// handler per group; extra emissions must surface, not hide behind index 0.
	singleElem := func(v any, context string) map[string]any {
		list, ok := v.([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("%s %s: want exactly one element, got %v", target, context, v)
		}
		entry, ok := list[0].(map[string]any)
		if !ok {
			t.Fatalf("%s %s: handler entry is not an object: %v", target, context, list[0])
		}
		return entry
	}

	var event string
	var group, handler map[string]any
	switch target {
	case TargetClaude, TargetCursor:
		hooks, ok := doc["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("%s: want a top-level hooks object, got %v", target, doc)
		}
		event = singleKey(hooks, "hooks map")
		handler = singleElem(hooks[event], "event handlers")
		group = handler
	case TargetFactory, TargetDevin:
		event = singleKey(doc, "top-level event map")
		group = singleElem(doc[event], "event groups")
		handler = singleElem(group["hooks"], "group handlers")
	case TargetGemini, TargetCodex:
		hooks, ok := doc["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("%s: want a top-level hooks object, got %v", target, doc)
		}
		event = singleKey(hooks, "hooks map")
		group = singleElem(hooks[event], "event groups")
		handler = singleElem(group["hooks"], "group handlers")
	case TargetWindsurf:
		hooks, ok := doc["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("windsurf: want a top-level hooks object, got %v", doc)
		}
		event = singleKey(hooks, "hooks map")
		handler = singleElem(hooks[event], "event handlers")
		group = handler
	default:
		t.Fatalf("no hand-authored hook shape for target %s", target)
	}

	out := hookEmission{event: event}
	if m, ok := group["matcher"].(string); ok {
		out.matcher = m
		out.hasMatcher = true
	}
	if v, ok := handler["command"].(string); ok {
		out.command = v
	}
	if v, ok := handler["timeout"].(float64); ok {
		timeout := int(v)
		out.timeout = &timeout
	}
	return out
}

// compileHook compiles one canonical hook for target and returns the single
// decoded emission, failing when compilation does not produce exactly one
// hook-bearing JSON artifact.
func compileHook(t *testing.T, target Target, hook *ir.Hook, opts ...Option) hookEmission {
	t.Helper()
	result, err := Compile(hook, target, opts...)
	if err != nil {
		t.Fatalf("Compile(hook, %s): %v", target, err)
	}
	if diag.HasErrors(result.Diagnostics) {
		t.Fatalf("unexpected error diagnostics on %s: %+v", target, result.Diagnostics)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("want exactly one artifact on %s, got %d (diags: %+v)", target, len(result.Artifacts), result.Diagnostics)
	}
	return decodeHookArtifact(t, target, result.Artifacts[0].Bytes)
}

// refuseHook compiles one hook for target and asserts the hook is suppressed
// with want as an error-severity diagnostic and zero artifacts. When the hook
// carries a command, no diagnostic message or hint may quote it: hook
// diagnostics are value-free.
func refuseHook(t *testing.T, target Target, hook *ir.Hook, want string, opts ...Option) {
	t.Helper()
	result, err := Compile(hook, target, opts...)
	if err != nil {
		t.Fatalf("Compile(hook, %s): %v", target, err)
	}
	if len(result.Artifacts) != 0 {
		t.Fatalf("the hook must be suppressed on %s; artifacts = %d", target, len(result.Artifacts))
	}
	if !hasErrorCode(result.Diagnostics, want) {
		t.Fatalf("want error-severity %s on %s; diagnostics = %+v", want, target, result.Diagnostics)
	}
	if hook.Command == "" {
		return
	}
	for _, d := range result.Diagnostics {
		if strings.Contains(d.Message, hook.Command) || strings.Contains(d.Hint, hook.Command) {
			t.Fatalf("diagnostic must not quote the hook command on %s: %+v", target, d)
		}
	}
}

// hasErrorCode reports whether diagnostics carry code at error severity; a
// warning with the same code does not satisfy a refusal scenario.
func hasErrorCode(diagnostics []diag.Diagnostic, code string) bool {
	for _, d := range diagnostics {
		if d.Code == code && d.IsError() {
			return true
		}
	}
	return false
}

func commandHook(name, event, matcher, command string) *ir.Hook {
	return ir.NewHookWithMatcherOptions(name, event, matcher, command, "command", nil, ir.SourceRef{})
}

// spec: hook-resources/untranslatable-matchers-suppress-the-hook
func TestNativeSourceMatchersExactNoGuess(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		matcher   string
		target    Target
		want      string
		wantError string
	}{
		{name: "factory spelling is not decoded as Claude", source: "factory", matcher: "Bash", target: TargetCursor, wantError: "hook.matcher-untranslatable"},
		{name: "Factory Execute translates to Cursor Shell", source: "factory", matcher: "Execute", target: TargetCursor, want: "^Shell$"},
		{name: "empty source keeps canonical Claude spelling", matcher: "Bash", target: TargetFactory, want: "Execute"},
		{name: "Devin exec translates to Factory Execute", source: "devin", matcher: "exec", target: TargetFactory, want: "Execute"},
		{name: "Devin spelling is not decoded as Claude", source: "devin", matcher: "Bash", target: TargetFactory, wantError: "hook.matcher-untranslatable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hook := commandHook("guard", "PreToolUse", tc.matcher, "echo source-check")
			hook.HookSource = tc.source
			if tc.wantError != "" {
				refuseHook(t, tc.target, hook, tc.wantError)
				return
			}
			emission := compileHook(t, tc.target, hook)
			if !emission.hasMatcher || emission.matcher != tc.want {
				t.Fatalf("matcher = %q (present %v), want %q", emission.matcher, emission.hasMatcher, tc.want)
			}
		})
	}
}

// spec: hook-resources/machine-scope-commands-use-absolute-support-paths
func TestMachineSupportBaseQuotesShellMetacharacters(t *testing.T) {
	hook := commandHook("guard", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh")
	emission := compileHook(t, TargetFactory, hook, WithPluginRoot("/tmp/hook support/it's/$HOME/`tick`"), WithScope(ScopeUser))
	want := "'/tmp/hook support/it'\\''s/$HOME/`tick`'/scripts/check.sh"
	if emission.command != want {
		t.Fatalf("command = %q, want literal support path %q", emission.command, want)
	}
}

// spec: hook-resources/unmappable-events-are-errors-on-every-target
func TestCatalogCrossUnmappableEventsAreErrorsOnEveryTarget(t *testing.T) {
	// Hand-authored per-target event-unmappable codes; a fabricated event is
	// unmappable on all seven targets, while a valid sibling hook in the same
	// bundle must still be emitted.
	wantCode := map[Target]string{
		TargetClaude:   "hook.event-unmappable",
		TargetFactory:  "hook.event-unmappable",
		TargetCursor:   "hook.event-unmappable",
		TargetGemini:   "hook.gemini.event-unmappable",
		TargetWindsurf: "hook.windsurf.event-unmappable",
		TargetDevin:    "hook.devin.event-unmappable",
		TargetCodex:    "hook.codex.unsupported",
	}
	siblingEvent := map[Target]string{
		TargetClaude:   "PreToolUse",
		TargetFactory:  "PreToolUse",
		TargetCursor:   "preToolUse",
		TargetGemini:   "BeforeTool",
		TargetWindsurf: "pre_run_command",
		TargetDevin:    "PreToolUse",
		TargetCodex:    "PreToolUse",
	}
	const siblingCommand = "echo sibling-ok-7c2f"
	const badCommand = "echo bad-hook-9d31"
	bundle := fstest.MapFS{
		"hooks/hooks.json": &fstest.MapFile{Data: []byte(`{
			"hooks": {
				"PreToolUse": [
					{"matcher": "Bash", "type": "command", "command": "` + siblingCommand + `"}
				],
				"NotARealEvent": [
					{"type": "command", "command": "` + badCommand + `"}
				]
			}
		}`)},
	}
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetGemini, TargetWindsurf, TargetDevin, TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			result, err := CompileBundle(bundle, target)
			if err != nil {
				t.Fatalf("CompileBundle(hooks, %s): %v", target, err)
			}
			if !hasErrorCode(result.Diagnostics, wantCode[target]) {
				t.Fatalf("want error %q on %s; diagnostics = %+v", wantCode[target], target, result.Diagnostics)
			}
			var sibling hookEmission
			emissions := 0
			for _, a := range result.Artifacts {
				if bytes.Contains(a.Bytes, []byte(badCommand)) {
					t.Fatalf("the unmappable hook's command must not appear in any artifact on %s:\n%s", target, a.Bytes)
				}
				emission := decodeHookArtifact(t, target, a.Bytes)
				emissions++
				if emission.command == siblingCommand {
					sibling = emission
				}
			}
			if emissions != 1 {
				t.Fatalf("the valid sibling must be the only hook emission on %s; emissions = %d", target, emissions)
			}
			if sibling.event != siblingEvent[target] {
				t.Fatalf("sibling event = %q, want %q on %s", sibling.event, siblingEvent[target], target)
			}
		})
	}
}

// spec: hook-resources/write-and-edit-translate-to-each-target-s-file-change-tools
func TestCatalogCrossWriteAndEditTranslateToFileChangeTools(t *testing.T) {
	cases := []struct {
		target      Target
		wantEvent   string
		wantMatcher string
		hasMatcher  bool
	}{
		{TargetFactory, "PostToolUse", "^(Create|Edit|ApplyPatch)$", true},
		{TargetCursor, "postToolUse", "^Write$", true},
		{TargetCodex, "PostToolUse", "^apply_patch$", true},
		// Windsurf encodes the file-change tool set in the event name and
		// emits no matcher member.
		{TargetWindsurf, "post_write_code", "", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.target), func(t *testing.T) {
			emission := compileHook(t, tc.target, commandHook("guard", "PostToolUse", "Write|Edit", "echo lint-4be1"))
			if emission.event != tc.wantEvent {
				t.Fatalf("event = %q, want %q", emission.event, tc.wantEvent)
			}
			if emission.hasMatcher != tc.hasMatcher || emission.matcher != tc.wantMatcher {
				t.Fatalf("matcher = %q (present %v), want %q (present %v)", emission.matcher, emission.hasMatcher, tc.wantMatcher, tc.hasMatcher)
			}
			if emission.command != "echo lint-4be1" {
				t.Fatalf("command = %q, want the unchanged hook command", emission.command)
			}
		})
	}
}

// spec: hook-resources/partial-tool-coverage-suppresses-the-hook
func TestCatalogCrossPartialToolCoverageSuppressesTheHook(t *testing.T) {
	// Cursor's Write tool also covers edits and Factory's ApplyPatch also
	// covers edits, so an Edit-only hook would silently widen on both.
	for _, target := range []Target{TargetCursor, TargetFactory} {
		t.Run(string(target), func(t *testing.T) {
			refuseHook(t, target, commandHook("guard", "PreToolUse", "Edit", "echo edit-check"), "hook.matcher-untranslatable")
		})
	}
}

// spec: hook-resources/untranslatable-matchers-suppress-the-hook
func TestCatalogCrossUntranslatableMatchersSuppressTheHook(t *testing.T) {
	cases := []struct {
		name    string
		target  Target
		matcher string
	}{
		{"a regex matcher is not a tool name", TargetFactory, "Bash.*"},
		{"a tool the target does not cover", TargetCursor, "Glob"},
		{"a server-scoped MCP pattern", TargetCursor, "mcp__github__.*"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := commandHook("guard", "PreToolUse", tc.matcher, "echo mcp-check-5aa2")
			refuseHook(t, tc.target, hook, "hook.matcher-untranslatable")
		})
	}
}

// spec: hook-resources/prompt-hooks-are-refused-where-undeclared
func TestCatalogCrossPromptHooksRefusedWhereUndeclared(t *testing.T) {
	// Stop is a representable event on every one of these targets, so each
	// refusal is driven by the undeclared prompt handler, not the event.
	for _, target := range []Target{TargetFactory, TargetGemini, TargetWindsurf, TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			hook := ir.NewHookWithMatcherOptions("guard", "Stop", "", "", "prompt", nil, ir.SourceRef{}, ir.WithHookPrompt("summarize-8f0d"))
			refuseHook(t, target, hook, "hook.prompt.unsupported")
		})
	}
}

// spec: hook-resources/project-directory-references-follow-the-target
func TestCatalogCrossProjectDirectoryReferencesFollowTheTarget(t *testing.T) {
	cases := []struct {
		target Target
		want   string
	}{
		{TargetFactory, `"$FACTORY_PROJECT_DIR"/bin/check`},
		{TargetCodex, `"$(git rev-parse --show-toplevel)"/bin/check`},
		{TargetWindsurf, `bin/check`},
	}
	for _, tc := range cases {
		t.Run(string(tc.target), func(t *testing.T) {
			emission := compileHook(t, tc.target, commandHook("guard", "PreToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/bin/check`))
			if emission.command != tc.want {
				t.Fatalf("command = %q, want %q", emission.command, tc.want)
			}
		})
	}
}

// spec: hook-resources/whole-word-project-directory-references-keep-the-repository-directory
func TestCatalogCrossWholeWordProjectDirectoryReferencesKeepTheRepositoryDirectory(t *testing.T) {
	for _, tc := range []struct{ name, command, want string }{
		{"bare", `cd $CLAUDE_PROJECT_DIR && pwd -P`, `cd . && pwd -P`},
		{"braced", `cd ${CLAUDE_PROJECT_DIR} && pwd -P`, `cd . && pwd -P`},
		{"quoted", `cd "$CLAUDE_PROJECT_DIR" && pwd -P`, `cd "." && pwd -P`},
		{"quoted trailing slash", `cd "$CLAUDE_PROJECT_DIR"/ && pwd -P`, `cd . && pwd -P`},
		{"slash inside quotes", `cd "$CLAUDE_PROJECT_DIR/"`, `cd "."`},
		{"unquoted trailing slash", `ls $CLAUDE_PROJECT_DIR/`, `ls .`},
		{"subshell", `(cd $CLAUDE_PROJECT_DIR) && pwd -P`, `(cd .) && pwd -P`},
		{"option value", `tool --root=$CLAUDE_PROJECT_DIR; pwd -P`, `tool --root=.; pwd -P`},
		{"alone", `$CLAUDE_PROJECT_DIR`, `.`},
		{"path component control", `"$CLAUDE_PROJECT_DIR"/bin/check`, `bin/check`},
		{"quoted path component control", `"$CLAUDE_PROJECT_DIR/bin/check"`, `"bin/check"`},
		{"unquoted path component control", `$CLAUDE_PROJECT_DIR/bin/check`, `bin/check`},
		{"repeated slash stays relative", `"$CLAUDE_PROJECT_DIR"//bin/check`, `./bin/check`},
		{"unquoted repeated slash stays relative", `$CLAUDE_PROJECT_DIR//bin/check`, `./bin/check`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emission := compileHook(t, TargetWindsurf, commandHook("guard", "PreToolUse", "Bash", tc.command))
			if emission.command != tc.want {
				t.Fatalf("command = %q, want %q", emission.command, tc.want)
			}
		})
	}
}

// spec: hook-resources/joined-project-directory-references-refuse-the-hook
func TestCatalogCrossJoinedProjectDirectoryReferencesRefuseTheHook(t *testing.T) {
	for _, command := range []string{
		`cp notes ${CLAUDE_PROJECT_DIR}-backup`,
		`cp notes $CLAUDE_PROJECT_DIR.bak`,
		`echo "$CLAUDE_PROJECT_DIR is the root"`,
		`ls "$CLAUDE_PROJECT_DIR"suffix`,
	} {
		t.Run(command, func(t *testing.T) {
			refuseHook(t, TargetWindsurf, commandHook("guard", "PreToolUse", "Bash", command), "hook.env-unrepresentable")
			if got := compileHook(t, TargetFactory, commandHook("guard", "PreToolUse", "Bash", command)).command; !strings.Contains(got, "FACTORY_PROJECT_DIR") {
				t.Fatalf("Factory keeps a variable repository base, so the joined form must still translate: %q", got)
			}
		})
	}
}

// spec: hook-resources/targets-that-accept-the-claude-spelling-keep-it
func TestCatalogCrossTargetsAcceptingTheClaudeSpellingKeepIt(t *testing.T) {
	for _, target := range []Target{TargetCursor, TargetGemini} {
		t.Run(string(target), func(t *testing.T) {
			emission := compileHook(t, target, commandHook("guard", "PreToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/bin/check`))
			if emission.command != `"$CLAUDE_PROJECT_DIR"/bin/check` {
				t.Fatalf("command = %q, want the Claude spelling kept verbatim", emission.command)
			}
		})
	}
}

// spec: hook-resources/variables-without-a-target-equivalent-refuse-the-hook
func TestCatalogCrossVariablesWithoutTargetEquivalentRefuseTheHook(t *testing.T) {
	hook := commandHook("guard", "SessionStart", "", `echo seed >> "$CLAUDE_ENV_FILE"`)
	refuseHook(t, TargetFactory, hook, "hook.env-unrepresentable")
}

// spec: hook-resources/unmappable-events-are-errors-on-every-target
// spec: hook-resources/claude-code-refuses-unknown-events
func TestCatalogNativeSameHarnessUndeclaredEventsRefuse(t *testing.T) {
	const command = "echo native-catalog-control"
	cases := []struct {
		target Target
		event  string
		code   string
	}{
		{TargetClaude, "PreToolUse", "hook.event-unmappable"},
		{TargetFactory, "PreToolUse", "hook.event-unmappable"},
		{TargetCursor, "preToolUse", "hook.event-unmappable"},
		{TargetGemini, "BeforeTool", "hook.gemini.event-unmappable"},
		{TargetWindsurf, "pre_run_command", "hook.windsurf.event-unmappable"},
		{TargetDevin, "PreToolUse", "hook.devin.event-unmappable"},
		{TargetCodex, "PreToolUse", "hook.codex.unsupported"},
	}
	for _, tc := range cases {
		t.Run(string(tc.target), func(t *testing.T) {
			hook := commandHook("guard", tc.event, "", command)
			hook.HookSource = string(tc.target)
			emission := compileHook(t, tc.target, hook)
			if emission.event != tc.event {
				t.Fatalf("event = %q, want the authored native event %q", emission.event, tc.event)
			}
			if emission.command != command {
				t.Fatalf("command = %q, want %q", emission.command, command)
			}
			bad := commandHook("guard", "UnknownNativeEventForProbe", "", command)
			bad.HookSource = string(tc.target)
			refuseHook(t, tc.target, bad, tc.code)
		})
	}
}

// spec: hook-resources/claude-code-refuses-unknown-events
func TestCatalogNativeDeclaredNativeOnlyEventsEmit(t *testing.T) {
	const command = "echo native-catalog-control"
	cases := []struct {
		target Target
		event  string
	}{
		{TargetClaude, "Setup"},
		{TargetCodex, "SessionEnd"},
	}
	for _, tc := range cases {
		t.Run(string(tc.target)+"/"+tc.event, func(t *testing.T) {
			hook := commandHook("guard", tc.event, "", command)
			hook.HookSource = string(tc.target)
			emission := compileHook(t, tc.target, hook)
			if emission.event != tc.event {
				t.Fatalf("event = %q, want the authored native event %q", emission.event, tc.event)
			}
			if emission.command != command {
				t.Fatalf("command = %q, want %q", emission.command, command)
			}
		})
	}
}

// spec: hook-resources/unknown-handler-types-are-refused-on-claude-code
func TestCatalogNativeDeclaredEventHandlerTypesEnforced(t *testing.T) {
	const command = "echo native-catalog-type-control"
	cases := []struct {
		target Target
		event  string
		code   string
	}{
		{TargetClaude, "Setup", "hook.type-unsupported"},
		{TargetCodex, "SessionEnd", "hook.codex.unsupported"},
	}
	for _, tc := range cases {
		t.Run(string(tc.target)+"/"+tc.event, func(t *testing.T) {
			mcp := ir.NewHookWithMatcherOptions("guard", tc.event, "", "", "mcp_tool",
				map[string]any{"server": "docs", "tool": "lookup"}, ir.SourceRef{},
				ir.WithHookSource(string(tc.target)))
			refuseHook(t, tc.target, mcp, tc.code)
			positive := commandHook("guard", tc.event, "", command)
			positive.HookSource = string(tc.target)
			emission := compileHook(t, tc.target, positive)
			if emission.event != tc.event {
				t.Fatalf("event = %q, want %q", emission.event, tc.event)
			}
			if emission.command != command {
				t.Fatalf("command = %q, want %q", emission.command, command)
			}
		})
	}
}

// spec: hook-resources/unknown-variables-are-left-unchanged
func TestCatalogCrossUnknownVariablesAreLeftUnchanged(t *testing.T) {
	for _, target := range []Target{TargetClaude, TargetFactory, TargetCursor, TargetGemini, TargetWindsurf, TargetDevin, TargetCodex} {
		t.Run(string(target), func(t *testing.T) {
			emission := compileHook(t, target, commandHook("guard", "PreToolUse", "Bash", `"$HOME"/bin/check`))
			if emission.command != `"$HOME"/bin/check` {
				t.Fatalf("$HOME must be emitted unchanged on %s; command = %q", target, emission.command)
			}
		})
	}
}
