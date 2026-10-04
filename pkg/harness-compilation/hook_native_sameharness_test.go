package harnesscompile

// Same-harness plugin-token tests (OpenSpec change native-hook-integration):
// every plugin_env name, primary or compat alias, routes through the
// installed support-directory reference. Expected commands and diagnostic
// codes are hand-authored fixture data.

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// nativeCommandHook builds a command hook carrying sourceHarness's native
// spelling so same-harness resolution applies.
func nativeCommandHook(sourceHarness, name, event, matcher, command string) *ir.Hook {
	return ir.NewHookWithMatcherOptions(name, event, matcher, command, "command", nil, ir.SourceRef{}, ir.WithHookSource(sourceHarness))
}

// spec: hook-resources/repository-scope-commands-use-the-project-directory-variable
// spec: hook-resources/codex-and-windsurf-repository-commands-use-their-documented-base
// spec: hook-resources/machine-scope-commands-use-absolute-support-paths
// spec: hook-resources/no-plugin-only-variable-or-cache-path-is-emitted
func TestNativeSameHarnessPluginReferences(t *testing.T) {
	repository := []struct {
		name      string
		target    Target
		source    string
		matcher   string
		reference string
		want      string
	}{
		{"factory plugin-root alias", TargetFactory, "factory", "Execute", "${CLAUDE_PLUGIN_ROOT}", `"$FACTORY_PROJECT_DIR"/hook-support/source/guard/scripts/check.sh`},
		{"factory plugin-root primary", TargetFactory, "factory", "Execute", "${DROID_PLUGIN_ROOT}", `"$FACTORY_PROJECT_DIR"/hook-support/source/guard/scripts/check.sh`},
		{"codex plugin-root alias", TargetCodex, "codex", "Bash", "${CLAUDE_PLUGIN_ROOT}", `"$(git rev-parse --show-toplevel)"/hook-support/source/guard/scripts/check.sh`},
		{"codex plugin-root primary", TargetCodex, "codex", "Bash", "${PLUGIN_ROOT}", `"$(git rev-parse --show-toplevel)"/hook-support/source/guard/scripts/check.sh`},
	}
	for _, tc := range repository {
		t.Run("repository "+tc.name, func(t *testing.T) {
			hook := nativeCommandHook(tc.source, "guard", "PreToolUse", tc.matcher, tc.reference+"/scripts/check.sh")
			emission := compileHook(t, tc.target, hook, WithPluginRoot("hook-support/source/guard"), WithScope(ScopeProject))
			if emission.event != "PreToolUse" {
				t.Fatalf("event = %q, want PreToolUse", emission.event)
			}
			if emission.matcher != tc.matcher {
				t.Fatalf("matcher = %q, want %q", emission.matcher, tc.matcher)
			}
			if emission.command != tc.want {
				t.Fatalf("command = %q, want %q", emission.command, tc.want)
			}
		})
	}

	machine := []struct {
		name      string
		target    Target
		source    string
		matcher   string
		reference string
	}{
		{"factory plugin-root alias", TargetFactory, "factory", "Execute", "${CLAUDE_PLUGIN_ROOT}"},
		{"factory plugin-root primary", TargetFactory, "factory", "Execute", "${DROID_PLUGIN_ROOT}"},
		{"codex plugin-root alias", TargetCodex, "codex", "Bash", "${CLAUDE_PLUGIN_ROOT}"},
		{"codex plugin-root primary", TargetCodex, "codex", "Bash", "${PLUGIN_ROOT}"},
	}
	for _, tc := range machine {
		t.Run("machine "+tc.name, func(t *testing.T) {
			hook := nativeCommandHook(tc.source, "guard", "PreToolUse", tc.matcher, tc.reference+"/scripts/check.sh")
			emission := compileHook(t, tc.target, hook, WithPluginRoot("/tmp/hook-support/source/guard"), WithScope(ScopeUser))
			if emission.event != "PreToolUse" {
				t.Fatalf("event = %q, want PreToolUse", emission.event)
			}
			if emission.matcher != tc.matcher {
				t.Fatalf("matcher = %q, want %q", emission.matcher, tc.matcher)
			}
			if emission.command != "/tmp/hook-support/source/guard/scripts/check.sh" {
				t.Fatalf("command = %q, want %q", emission.command, "/tmp/hook-support/source/guard/scripts/check.sh")
			}
		})
	}

	// Non-plugin same-harness references keep their authored spelling.
	factoryControl := nativeCommandHook("factory", "guard", "PreToolUse", "Execute", `"$FACTORY_PROJECT_DIR"/bin/check`)
	emission := compileHook(t, TargetFactory, factoryControl, WithScope(ScopeProject))
	if emission.command != `"$FACTORY_PROJECT_DIR"/bin/check` {
		t.Fatalf("non-plugin reference must be kept verbatim on factory; command = %q", emission.command)
	}
	codexControl := nativeCommandHook("codex", "guard", "PreToolUse", "Bash", `"$HOME"/bin/check`)
	emission = compileHook(t, TargetCodex, codexControl, WithScope(ScopeProject))
	if emission.command != `"$HOME"/bin/check` {
		t.Fatalf("non-plugin reference must be kept verbatim on codex; command = %q", emission.command)
	}
}

// spec: hook-resources/no-plugin-only-variable-or-cache-path-is-emitted
func TestNativeSameHarnessPluginDataRefusal(t *testing.T) {
	for _, reference := range []string{"${PLUGIN_DATA}", "${CLAUDE_PLUGIN_DATA}"} {
		t.Run(reference, func(t *testing.T) {
			command := reference + "/payload.bin"
			hook := nativeCommandHook("codex", "guard", "PreToolUse", "Bash", command)
			refuseHook(t, TargetCodex, hook, "hook.env-unrepresentable", WithPluginRoot("hook-support/source/guard"), WithScope(ScopeProject))
		})
	}
}

// TestNativeSameHarnessPluginMissingSupportBase is an additional failure
// control: it exercises a missing opaque plugin-root support base, not the
// absent catalog repository_base of
// hook-resources/missing-repository-base-refuses-token-commands, so it carries
// no spec binding.
func TestNativeSameHarnessPluginMissingSupportBase(t *testing.T) {
	cases := []struct {
		name      string
		target    Target
		source    string
		matcher   string
		reference string
	}{
		{"factory plugin-root alias", TargetFactory, "factory", "Execute", "${CLAUDE_PLUGIN_ROOT}"},
		{"factory plugin-root primary", TargetFactory, "factory", "Execute", "${DROID_PLUGIN_ROOT}"},
		{"codex plugin-root alias", TargetCodex, "codex", "Bash", "${CLAUDE_PLUGIN_ROOT}"},
		{"codex plugin-root primary", TargetCodex, "codex", "Bash", "${PLUGIN_ROOT}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			command := tc.reference + "/scripts/check.sh"
			hook := nativeCommandHook(tc.source, "guard", "PreToolUse", tc.matcher, command)
			result, err := Compile(hook, tc.target, WithScope(ScopeProject))
			if err != nil {
				t.Fatalf("Compile(hook, %s): %v", tc.target, err)
			}
			if len(result.Artifacts) != 0 {
				t.Fatalf("the hook must be suppressed on %s; artifacts = %d", tc.target, len(result.Artifacts))
			}
			if !hasErrorCode(result.Diagnostics, "hook.support-path-unrepresentable") {
				t.Fatalf("want error-severity hook.support-path-unrepresentable on %s; diagnostics = %+v", tc.target, result.Diagnostics)
			}
		})
	}
}
