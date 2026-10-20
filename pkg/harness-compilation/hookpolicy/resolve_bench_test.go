package hookpolicy

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var resolveBenchmarkResult Resolved

func BenchmarkResolveNativePluginReferences(b *testing.B) {
	cases := []struct {
		name    string
		source  target.Target
		command string
	}{
		{"factory-primary", target.TargetFactory, "${DROID_PLUGIN_ROOT}/check.sh"},
		{"factory-alias", target.TargetFactory, "${CLAUDE_PLUGIN_ROOT}/check.sh"},
		{"codex-primary", target.TargetCodex, "${PLUGIN_ROOT}/check.sh"},
		{"codex-alias", target.TargetCodex, "${CLAUDE_PLUGIN_ROOT}/check.sh"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "", tc.command, "command", nil, ir.SourceRef{}, ir.WithHookSource(string(tc.source)))
			Resolve(hook, tc.source, ScopeRepository, "hook-support/source/guard")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resolveBenchmarkResult = Resolve(hook, tc.source, ScopeRepository, "hook-support/source/guard")
			}
		})
	}
}

func BenchmarkResolveWindsurfProjectDirectoryReferences(b *testing.B) {
	for _, tc := range []struct{ name, command string }{
		{"whole-word", `cd "$CLAUDE_PROJECT_DIR" && pwd -P`},
		{"path-component", `"$CLAUDE_PROJECT_DIR"/bin/check`},
	} {
		b.Run(tc.name, func(b *testing.B) {
			hook := ir.NewHookWithMatcherOptions("guard", "PreToolUse", "Bash", tc.command, "command", nil, ir.SourceRef{}, ir.WithHookSource(string(target.TargetClaude)))
			Resolve(hook, target.TargetWindsurf, ScopeRepository, "")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resolveBenchmarkResult = Resolve(hook, target.TargetWindsurf, ScopeRepository, "")
			}
		})
	}
}

func BenchmarkResolveHookTimeouts(b *testing.B) {
	for _, tc := range []struct {
		name, event, source string
		target              target.Target
		timeout             int
	}{
		{"claude-seconds", "PreToolUse", "", target.TargetClaude, 30},
		{"gemini-milliseconds", "PreToolUse", "", target.TargetGemini, 30},
		{"codex-sessionend-bound", "SessionEnd", string(target.TargetCodex), target.TargetCodex, 0},
	} {
		b.Run(tc.name, func(b *testing.B) {
			matcher := "Bash"
			if tc.event != "PreToolUse" {
				matcher = ""
			}
			options := []ir.HookOption{ir.WithHookTimeout(tc.timeout)}
			if tc.source != "" {
				options = append(options, ir.WithHookSource(tc.source))
			}
			hook := ir.NewHookWithMatcherOptions("guard", tc.event, matcher, "echo check", "command", nil, ir.SourceRef{}, options...)
			Resolve(hook, tc.target, ScopeRepository, "")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resolveBenchmarkResult = Resolve(hook, tc.target, ScopeRepository, "")
			}
		})
	}
}
