package hookpolicy_test

// Resolver-level run of the independently authored conversion fixtures.
// The Compile-level runner in conversions_fixture_test.go exercises the
// same fixture file through the public boundary; this runner verifies the
// resolver directly.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/hookpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
	"gopkg.in/yaml.v3"
)

func TestResolverConversionFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/conversions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures conversionFixtures
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fixtures); err != nil {
		t.Fatal(err)
	}
	run := func(section string, cases []hookCase) {
		for _, c := range cases {
			t.Run(section+"/"+c.Name, func(t *testing.T) {
				runResolveCase(t, c)
			})
		}
	}
	run("matchers", fixtures.Matchers)
	run("events", fixtures.Events)
	run("env", fixtures.Env)
	run("timeouts", fixtures.Timeouts)
}

func runResolveCase(t *testing.T, c hookCase) {
	t.Helper()
	var options []ir.HookOption
	if c.Source != "" {
		options = append(options, ir.WithHookSource(c.Source))
	}
	options = append(options, ir.WithHookTimeoutPtr(c.Timeout))
	hook := ir.NewHookWithMatcherOptions("fixture", c.Event, c.Matcher, c.Command, "command", nil, ir.SourceRef{}, options...)
	res := hookpolicy.Resolve(hook, target.Target(c.Target), hookpolicy.ScopeRepository, "")
	if c.WantError != "" {
		if !hasResolveCode(res, c.WantError) {
			t.Fatalf("want error %q, diagnostics = %v", c.WantError, resolveCodes(res))
		}
		return
	}
	if res.Failed() {
		t.Fatalf("resolution suppressed: %+v", res.Diagnostics)
	}
	if c.WantEvent != "" && res.Event != c.WantEvent {
		t.Fatalf("event = %q, want %q", res.Event, c.WantEvent)
	}
	if res.Matcher != c.WantMatcher {
		t.Fatalf("matcher = %q, want %q", res.Matcher, c.WantMatcher)
	}
	if c.WantCommand != "" && res.Command != c.WantCommand {
		t.Fatalf("command = %q, want %q", res.Command, c.WantCommand)
	}
	if c.WantTimeout != nil {
		if res.Timeout == nil || *res.Timeout != *c.WantTimeout {
			got := "nil"
			if res.Timeout != nil {
				got = strconv.Itoa(*res.Timeout)
			}
			t.Fatalf("timeout = %s, want %d", got, *c.WantTimeout)
		}
	}
	if c.WantWarning != "" && !hasResolveCode(res, c.WantWarning) {
		t.Fatalf("want warning %q, diagnostics = %v", c.WantWarning, resolveCodes(res))
	}
}

func resolveCodes(res hookpolicy.Resolved) []string {
	var codes []string
	for _, d := range res.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

func hasResolveCode(res hookpolicy.Resolved, code string) bool {
	return diag.HasCode(res.Diagnostics, code)
}

// spec: hook-resources/unknown-handler-types-are-refused-on-claude-code
func TestResolveDeclaredNativeEventHandlerTypes(t *testing.T) {
	mcpHook := func(source, event string, timeout *int) *ir.Hook {
		return ir.NewHookWithMatcherOptions("fixture", event, "", "", "mcp_tool",
			map[string]any{"server": "docs", "tool": "lookup"}, ir.SourceRef{},
			ir.WithHookSource(source), ir.WithHookTimeoutPtr(timeout))
	}
	commandHook := func(source, event string, timeout *int) *ir.Hook {
		return ir.NewHookWithMatcherOptions("fixture", event, "", "echo fixture-native-type-control", "command",
			nil, ir.SourceRef{}, ir.WithHookSource(source), ir.WithHookTimeoutPtr(timeout))
	}
	three := 3
	cases := []struct {
		name        string
		hook        *ir.Hook
		target      target.Target
		wantError   string
		wantEvent   string
		wantTimeout *int
	}{
		{name: "claude Setup refuses mcp_tool", hook: mcpHook("claude", "Setup", nil), target: target.TargetClaude, wantError: "hook.type-unsupported"},
		{name: "codex SessionEnd refuses mcp_tool", hook: mcpHook("codex", "SessionEnd", nil), target: target.TargetCodex, wantError: "hook.codex.unsupported"},
		{name: "claude Setup command resolves", hook: commandHook("claude", "Setup", nil), target: target.TargetClaude, wantEvent: "Setup"},
		{name: "codex SessionEnd command resolves", hook: commandHook("codex", "SessionEnd", nil), target: target.TargetCodex, wantEvent: "SessionEnd"},
		{name: "codex SessionEnd command keeps timeout 3", hook: commandHook("codex", "SessionEnd", &three), target: target.TargetCodex, wantEvent: "SessionEnd", wantTimeout: &three},
		{name: "claude PreToolUse mcp_tool resolves", hook: mcpHook("claude", "PreToolUse", nil), target: target.TargetClaude, wantEvent: "PreToolUse"},
		{name: "codex PreToolUse mcp_tool resolves", hook: mcpHook("codex", "PreToolUse", nil), target: target.TargetCodex, wantEvent: "PreToolUse"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := hookpolicy.Resolve(c.hook, c.target, hookpolicy.ScopeRepository, "")
			if c.wantError != "" {
				if !res.Failed() || !hasResolveCode(res, c.wantError) {
					t.Fatalf("want error %q, diagnostics = %v", c.wantError, resolveCodes(res))
				}
				return
			}
			if res.Failed() {
				t.Fatalf("resolution suppressed: %+v", res.Diagnostics)
			}
			if res.Event != c.wantEvent {
				t.Fatalf("event = %q, want %q", res.Event, c.wantEvent)
			}
			if c.wantTimeout != nil {
				if res.Timeout == nil || *res.Timeout != *c.wantTimeout {
					t.Fatalf("timeout = %v, want %d", res.Timeout, *c.wantTimeout)
				}
			}
		})
	}
}

func TestMachineSupportBaseIsQuotedAsLiteral(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh", "command", nil, ir.SourceRef{}, ir.WithHookSource("claude"))
	cases := []struct {
		name string
		base string
		want string
	}{
		{name: "safe path", base: "/tmp/hook-support/source/guard", want: "/tmp/hook-support/source/guard/scripts/check.sh"},
		{name: "shell metacharacters", base: "/tmp/hook support/it's/$HOME/`tick`", want: "'/tmp/hook support/it'\\''s/$HOME/`tick`'/scripts/check.sh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved := hookpolicy.Resolve(hook, target.TargetFactory, hookpolicy.ScopeMachine, tc.base)
			if resolved.Failed() {
				t.Fatalf("machine hook resolution failed: %+v", resolved.Diagnostics)
			}
			if resolved.Command != tc.want {
				t.Fatalf("command = %q, want %q", resolved.Command, tc.want)
			}
		})
	}
}

func assertFactoryCompileHook(t *testing.T, result *hcc.Result, wantCommand string) {
	t.Helper()
	if result == nil || result.HasErrors() {
		t.Fatalf("Factory Compile failed: %+v", result)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("Factory artifact count = %d, want 1", len(result.Artifacts))
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &document); err != nil {
		t.Fatalf("Factory hook artifact is invalid JSON: %v", err)
	}
	if len(document) != 1 {
		t.Fatalf("Factory hook artifact keys = %v, want bare PreToolUse map", document)
	}
	preToolUse, ok := document["PreToolUse"]
	if !ok {
		t.Fatalf("Factory hook artifact lacks PreToolUse: %v", document)
	}
	var groups []struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(preToolUse, &groups); err != nil {
		t.Fatalf("Factory PreToolUse groups are invalid: %v", err)
	}
	if len(groups) != 1 || groups[0].Matcher != "Execute" || len(groups[0].Hooks) != 1 ||
		groups[0].Hooks[0].Type != "command" || groups[0].Hooks[0].Command != wantCommand {
		t.Fatalf("Factory hook emission = %+v, want one Execute command %q", groups, wantCommand)
	}
}

// spec: hook-resources/missing-repository-base-refuses-token-commands
func TestCompileRefusesTokenWhenCatalogLacksRepositoryBase(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions(
		"guard", "PreToolUse", "Bash", "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh", "command", nil,
		ir.SourceRef{}, ir.WithHookSource("claude"),
	)
	options := []hcc.Option{
		hcc.WithPluginRoot(".factory/hook-support/source/guard"),
		hcc.WithScope(hcc.ScopeProject),
	}

	beforeOverride, err := hcc.Compile(hook, hcc.TargetFactory, options...)
	if err != nil {
		t.Fatalf("default-catalog Factory Compile: %v", err)
	}
	assertFactoryCompileHook(t, beforeOverride, `"$FACTORY_PROJECT_DIR"/.factory/hook-support/source/guard/scripts/check.sh`)

	restore, err := hookpolicy.WithoutRepositoryBaseForTest(target.TargetFactory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)

	refused, err := hcc.Compile(hook, hcc.TargetFactory, options...)
	if err != nil {
		t.Fatalf("no-repository-base Factory Compile: %v", err)
	}
	if refused == nil || len(refused.Artifacts) != 0 || len(refused.Diagnostics) != 1 {
		t.Fatalf("no-base token Compile = %+v, want no artifacts and one diagnostic", refused)
	}
	diagnostic := refused.Diagnostics[0]
	if diagnostic.Code != diag.CodeHookSupportPathUnrepresentable || diagnostic.Severity != hcc.SeverityError {
		t.Fatalf("no-base token diagnostic = %+v", diagnostic)
	}

	literalHook := *hook
	literalHook.Command = "echo no-token"
	control, err := hcc.Compile(&literalHook, hcc.TargetFactory, options...)
	if err != nil {
		t.Fatalf("no-token Factory Compile: %v", err)
	}
	assertFactoryCompileHook(t, control, "echo no-token")
}

// spec: hook-resources/no-plugin-only-variable-or-cache-path-is-emitted
// spec: hook-resources/unknown-variables-are-left-unchanged
func TestCompileRefusesForeignPluginVariables(t *testing.T) {
	targets := []hcc.Target{
		hcc.TargetClaude, hcc.TargetFactory, hcc.TargetCursor, hcc.TargetGemini,
		hcc.TargetWindsurf, hcc.TargetDevin, hcc.TargetCodex,
	}
	scopes := []struct {
		name  string
		scope hcc.Scope
	}{
		{name: "repository", scope: hcc.ScopeProject},
		{name: "machine", scope: hcc.ScopeUser},
	}
	supportBase := func(tgt hcc.Target, scope hcc.Scope) string {
		if scope == hcc.ScopeUser {
			return "/tmp/hook-support/source/guard"
		}
		return "." + string(tgt) + "/hook-support/source/guard"
	}
	sources := []struct {
		name   string
		source string
	}{
		{name: "default", source: ""},
		{name: "claude", source: "claude"},
	}
	tokens := []struct {
		name  string
		token string
	}{
		{name: "bare", token: "$DROID_PLUGIN_ROOT"},
		{name: "braced", token: "${DROID_PLUGIN_ROOT}"},
		{name: "quoted", token: `"${DROID_PLUGIN_ROOT}"`},
	}
	for _, src := range sources {
		for _, tgt := range targets {
			for _, sc := range scopes {
				for _, tok := range tokens {
					t.Run(src.name+"/"+string(tgt)+"/"+sc.name+"/"+tok.name, func(t *testing.T) {
						command := "echo private_foreign_token_probe; " + tok.token + "/scripts/check.sh"
						var options []ir.HookOption
						if src.source != "" {
							options = append(options, ir.WithHookSource(src.source))
						}
						hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", command, "command", nil, ir.SourceRef{}, options...)
						result, err := hcc.Compile(hook, tgt,
							hcc.WithPluginRoot(supportBase(tgt, sc.scope)),
							hcc.WithScope(sc.scope))
						if err != nil {
							t.Fatalf("Compile: %v", err)
						}
						if result == nil || !result.HasErrors() {
							t.Fatalf("Compile = %+v, want an error diagnostic", result)
						}
						if len(result.Artifacts) != 0 {
							t.Fatalf("a refused hook must emit zero artifacts, got %d", len(result.Artifacts))
						}
						if len(result.Diagnostics) != 1 {
							t.Fatalf("diagnostics = %+v, want exactly one", result.Diagnostics)
						}
						diagnostic := result.Diagnostics[0]
						if diagnostic.Code != diag.CodeHookEnvUnrepresentable || diagnostic.Severity != hcc.SeverityError {
							t.Fatalf("diagnostic = %+v, want error %q", diagnostic, diag.CodeHookEnvUnrepresentable)
						}
						for _, leaked := range []string{"DROID_PLUGIN_ROOT", "private_foreign_token_probe", command} {
							if strings.Contains(diagnostic.Message, leaked) || strings.Contains(diagnostic.Hint, leaked) {
								t.Fatalf("diagnostic leaks a payload value %q: %+v", leaked, diagnostic)
							}
						}
					})
				}
			}
		}
	}

	factoryHook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Execute",
		`"${DROID_PLUGIN_ROOT}"/scripts/check.sh`, "command", nil, ir.SourceRef{}, ir.WithHookSource("factory"))
	declaredProject, err := hcc.Compile(factoryHook, hcc.TargetFactory,
		hcc.WithPluginRoot(".factory/hook-support/source/guard"), hcc.WithScope(hcc.ScopeProject))
	if err != nil {
		t.Fatalf("declared-token Factory Compile: %v", err)
	}
	assertFactoryCompileHook(t, declaredProject, `"$FACTORY_PROJECT_DIR"/.factory/hook-support/source/guard/scripts/check.sh`)
	declaredMachine, err := hcc.Compile(factoryHook, hcc.TargetFactory,
		hcc.WithPluginRoot("/tmp/hook-support/source/guard"), hcc.WithScope(hcc.ScopeUser))
	if err != nil {
		t.Fatalf("declared-token Factory Compile: %v", err)
	}
	assertFactoryCompileHook(t, declaredMachine, "/tmp/hook-support/source/guard/scripts/check.sh")

	unknownCommand := "echo $HOME ${AM_PRIVATE_CUSTOM_VAR}"
	encoded, err := json.Marshal(unknownCommand)
	if err != nil {
		t.Fatal(err)
	}
	for _, tgt := range targets {
		for _, sc := range scopes {
			t.Run("unknown/"+string(tgt)+"/"+sc.name, func(t *testing.T) {
				hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", unknownCommand, "command", nil,
					ir.SourceRef{}, ir.WithHookSource("claude"))
				result, err := hcc.Compile(hook, tgt,
					hcc.WithPluginRoot(supportBase(tgt, sc.scope)),
					hcc.WithScope(sc.scope))
				if err != nil {
					t.Fatalf("Compile: %v", err)
				}
				if result == nil || result.HasErrors() {
					t.Fatalf("Compile = %+v, want no error diagnostics", result)
				}
				if len(result.Artifacts) != 1 {
					t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
				}
				if !strings.Contains(string(result.Artifacts[0].Bytes), string(encoded)) {
					t.Fatalf("artifact = %s, want the command emitted verbatim", result.Artifacts[0].Bytes)
				}
			})
		}
	}

	restore, err := hookpolicy.WithoutRepositoryBaseForTest(target.TargetFactory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	overridden, err := hcc.Compile(
		ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash",
			"echo private_foreign_token_probe; ${DROID_PLUGIN_ROOT}/scripts/check.sh", "command", nil,
			ir.SourceRef{}, ir.WithHookSource("claude")),
		hcc.TargetFactory,
		hcc.WithPluginRoot(".factory/hook-support/source/guard"), hcc.WithScope(hcc.ScopeProject))
	if err != nil {
		t.Fatalf("overridden Factory Compile: %v", err)
	}
	if overridden == nil || len(overridden.Artifacts) != 0 || len(overridden.Diagnostics) != 1 {
		t.Fatalf("overridden Compile = %+v, want no artifacts and one diagnostic", overridden)
	}
	if diagnostic := overridden.Diagnostics[0]; diagnostic.Code != diag.CodeHookEnvUnrepresentable ||
		diagnostic.Severity != hcc.SeverityError {
		t.Fatalf("overridden diagnostic = %+v, want error %q", diagnostic, diag.CodeHookEnvUnrepresentable)
	}
}

// spec: hook-resources/single-quoted-plugin-root-literals-refuse-safely
func TestCompileRefusesSingleQuotedPluginRoot(t *testing.T) {
	machineBase := "/tmp/hook home/quote'/$literal/\"double\"/`tick`/guard"
	machineDouble := "echo \"/tmp/hook home/quote'/\\$literal/\\\"double\\\"/\\`tick\\`/guard/scripts/run.sh\""
	machineBare := "echo '/tmp/hook home/quote'\\''/$literal/\"double\"/`tick`/guard'/scripts/run.sh"
	targets := []struct {
		name, member, event string
		target              hcc.Target
		grouped             bool
		repoBase            string
		repoDouble          string
		repoBare            string
	}{
		{
			name:       "factory",
			target:     hcc.TargetFactory,
			event:      "PreToolUse",
			grouped:    true,
			repoBase:   ".factory/hook-support/source/guard",
			repoDouble: `echo "$FACTORY_PROJECT_DIR/.factory/hook-support/source/guard/scripts/run.sh"`,
			repoBare:   `echo "$FACTORY_PROJECT_DIR"/.factory/hook-support/source/guard/scripts/run.sh`,
		},
		{
			name:       "codex",
			target:     hcc.TargetCodex,
			member:     "hooks",
			event:      "PreToolUse",
			grouped:    true,
			repoBase:   ".codex/hook-support/source/guard",
			repoDouble: `echo "$(git rev-parse --show-toplevel)/.codex/hook-support/source/guard/scripts/run.sh"`,
			repoBare:   `echo "$(git rev-parse --show-toplevel)"/.codex/hook-support/source/guard/scripts/run.sh`,
		},
		{
			name:       "windsurf",
			target:     hcc.TargetWindsurf,
			member:     "hooks",
			event:      "pre_run_command",
			repoBase:   "hook-support/source/guard",
			repoDouble: `echo "hook-support/source/guard/scripts/run.sh"`,
			repoBare:   "echo hook-support/source/guard/scripts/run.sh",
		},
	}
	commandFromResult := func(t *testing.T, result *hcc.Result, member, event string, grouped bool) string {
		t.Helper()
		if result == nil {
			t.Fatal("Compile returned nil result without an error")
			return ""
		}
		if len(result.Artifacts) != 1 {
			t.Fatalf("native artifact count = %d, want 1", len(result.Artifacts))
		}
		var document map[string]json.RawMessage
		if err := json.Unmarshal(result.Artifacts[0].Bytes, &document); err != nil {
			t.Fatalf("decode native artifact: %v", err)
		}
		if member != "" {
			if err := json.Unmarshal(document[member], &document); err != nil {
				t.Fatalf("decode native %s member: %v", member, err)
			}
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(document[event], &entries); err != nil || len(entries) != 1 {
			t.Fatalf("native %s entries = %d, %v; want one", event, len(entries), err)
		}
		handler := entries[0]
		if grouped {
			var group struct {
				Hooks []json.RawMessage `json:"hooks"`
			}
			if err := json.Unmarshal(entries[0], &group); err != nil || len(group.Hooks) != 1 {
				t.Fatalf("native %s handlers = %d, %v; want one", event, len(group.Hooks), err)
			}
			handler = group.Hooks[0]
		}
		var command struct {
			Value string `json:"command"`
		}
		if err := json.Unmarshal(handler, &command); err != nil {
			t.Fatalf("decode native command: %v", err)
		}
		return command.Value
	}
	checkSyntax := func(t *testing.T, command string) {
		t.Helper()
		syntax := exec.Command("sh", "-n")
		syntax.Stdin = strings.NewReader(command)
		if output, err := syntax.CombinedOutput(); err != nil {
			t.Errorf("actual projected command is not valid shell: %v: %s; command=%q", err, output, command)
		}
	}
	compile := func(t *testing.T, command string, target hcc.Target, scope hcc.Scope, base string) (*hcc.Result, *hcc.Result, error) {
		t.Helper()
		hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", command, "command", nil,
			ir.SourceRef{}, ir.WithHookSource("claude"))
		withFlag, err := hcc.Compile(hook, target,
			hcc.WithNativeHookOutput(), hcc.WithPluginRoot(base), hcc.WithScope(scope))
		if err != nil {
			return nil, nil, err
		}
		pure, err := hcc.CompileNativeHookProjection(hook, target, scope, base)
		return withFlag, pure, err
	}
	compareProjectionResults := func(t *testing.T, withFlag, pure *hcc.Result) {
		t.Helper()
		if withFlag == nil || pure == nil || len(withFlag.Artifacts) != len(pure.Artifacts) || len(withFlag.Diagnostics) != len(pure.Diagnostics) {
			t.Fatalf("native flag and pure projection result shapes differ: flag=%+v pure=%+v", withFlag, pure)
		}
		for i := range withFlag.Diagnostics {
			if withFlag.Diagnostics[i] != pure.Diagnostics[i] {
				t.Errorf("native flag diagnostics = %+v, pure diagnostics = %+v", withFlag.Diagnostics, pure.Diagnostics)
				break
			}
		}
		for i := range withFlag.Artifacts {
			left, right := withFlag.Artifacts[i], pure.Artifacts[i]
			if left.Path != right.Path || left.Mode != right.Mode || !bytes.Equal(left.Bytes, right.Bytes) {
				t.Errorf("native flag artifact differs from pure projection: flag=%+v pure=%+v", left, right)
			}
		}
	}
	checkError := func(t *testing.T, result *hcc.Result, member, event string, grouped bool) {
		t.Helper()
		if result == nil {
			t.Fatal("Compile returned nil result without an error")
			return
		}
		if !result.HasErrors() || !diag.HasCode(result.Diagnostics, diag.CodeHookEnvUnrepresentable) {
			t.Errorf("single-quoted plugin root diagnostics = %+v, want %s error", result.Diagnostics, diag.CodeHookEnvUnrepresentable)
		}
		if len(result.Artifacts) != 0 {
			t.Errorf("single-quoted plugin root emitted %d artifacts, want none", len(result.Artifacts))
			checkSyntax(t, commandFromResult(t, result, member, event, grouped))
		}
	}

	for _, tc := range targets {
		for _, scopeCase := range []struct {
			name  string
			scope hcc.Scope
		}{
			{name: "repository", scope: hcc.ScopeProject},
			{name: "machine", scope: hcc.ScopeUser},
		} {
			base := tc.repoBase
			wantDouble, wantBare := tc.repoDouble, tc.repoBare
			if scopeCase.scope == hcc.ScopeUser {
				base = machineBase
				wantDouble, wantBare = machineDouble, machineBare
			}
			t.Run(tc.name+"/"+scopeCase.name+"/single-quoted", func(t *testing.T) {
				result, pure, err := compile(t, `echo '${CLAUDE_PLUGIN_ROOT}/scripts/run.sh'`, tc.target, scopeCase.scope, base)
				if err != nil {
					t.Fatalf("public Compile: %v", err)
				}
				compareProjectionResults(t, result, pure)
				checkError(t, result, tc.member, tc.event, tc.grouped)
			})
			t.Run(tc.name+"/"+scopeCase.name+"/double-quoted", func(t *testing.T) {
				result, pure, err := compile(t, `echo "${CLAUDE_PLUGIN_ROOT}/scripts/run.sh"`, tc.target, scopeCase.scope, base)
				if err != nil {
					t.Fatalf("public Compile: %v", err)
				}
				compareProjectionResults(t, result, pure)
				if result == nil || result.HasErrors() {
					t.Fatalf("double-quoted plugin root diagnostics = %+v", result)
				}
				got := commandFromResult(t, result, tc.member, tc.event, tc.grouped)
				if got != wantDouble {
					t.Errorf("projected command = %q, want %q", got, wantDouble)
				}
				checkSyntax(t, got)
			})
			t.Run(tc.name+"/"+scopeCase.name+"/unquoted", func(t *testing.T) {
				result, pure, err := compile(t, `echo ${CLAUDE_PLUGIN_ROOT}/scripts/run.sh`, tc.target, scopeCase.scope, base)
				if err != nil {
					t.Fatalf("public Compile: %v", err)
				}
				compareProjectionResults(t, result, pure)
				if result == nil || result.HasErrors() {
					t.Fatalf("unquoted plugin root diagnostics = %+v", result)
				}
				got := commandFromResult(t, result, tc.member, tc.event, tc.grouped)
				if got != wantBare {
					t.Errorf("projected command = %q, want %q", got, wantBare)
				}
				checkSyntax(t, got)
			})
		}
	}

	t.Run("same-harness project variable remains authored inside single quotes", func(t *testing.T) {
		command := `echo '$CLAUDE_PROJECT_DIR/ordinary'`
		hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", command, "command", nil,
			ir.SourceRef{}, ir.WithHookSource("claude"))
		result, err := hcc.Compile(hook, hcc.TargetClaude,
			hcc.WithNativeHookOutput(), hcc.WithScope(hcc.ScopeProject))
		if err != nil {
			t.Fatalf("public same-harness Compile: %v", err)
		}
		pure, err := hcc.CompileNativeHookProjection(hook, hcc.TargetClaude, hcc.ScopeProject, "")
		if err != nil {
			t.Fatalf("public same-harness pure projection: %v", err)
		}
		compareProjectionResults(t, result, pure)
		if result == nil || result.HasErrors() {
			t.Fatalf("same-harness project-variable diagnostics = %+v", result)
		}
		got := commandFromResult(t, result, "hooks", "PreToolUse", true)
		if got != command {
			t.Errorf("same-harness command = %q, want authored command %q", got, command)
		}
		checkSyntax(t, got)
	})
}
