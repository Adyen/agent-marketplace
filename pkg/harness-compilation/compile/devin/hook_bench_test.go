package devin

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Hook translation runs once per selected hook on every sync, convert, and
// status invocation, so it sits on the same user-visible latency path as skill
// and subagent translation. Unlike those two it also marshals JSON, which makes
// it the most allocation-heavy per-resource path in this target.
//
// Each shape gets its own sub-benchmark so a regression in one cannot hide
// behind another: minimal is the common case, rich exercises matcher, timeout,
// plugin-root expansion, and extras, user-scope adds the config.json envelope,
// and the failure shapes exercise the skip branches that allocate a diagnostic
// instead of an artifact.

func BenchmarkTranslateHook(b *testing.B) {
	source := ir.SourceRef{File: "hooks/hooks.json", Line: 1}
	timeout := 60
	negative := -1

	richOptions := compile.Options{}
	compile.WithPluginRoot("/plugins/demo")(&richOptions)
	userOptions := compile.Options{}
	compile.WithScope(compile.ScopeUser)(&userOptions)

	cases := []struct {
		name    string
		hook    *ir.Hook
		options compile.Options
	}{
		{
			name: "minimal",
			hook: ir.NewHook("pre-tool", "PreToolUse", "check.sh", "command", nil, source),
		},
		{
			name: "prompt",
			hook: ir.NewHookWithOptions("submit", "UserPromptSubmit", "", "prompt", nil, source,
				ir.WithHookPrompt("Summarize the diff")),
		},
		{
			name: "rich",
			hook: ir.NewHookWithMatcherOptions("rich", "PreToolUse", "Edit", "${CLAUDE_PLUGIN_ROOT}/scripts/guard.sh", "command",
				map[string]any{"showOutput": true, "workingDir": "."}, source, ir.WithHookTimeoutPtr(&timeout)),
			options: richOptions,
		},
		{
			name:    "user-scope",
			hook:    ir.NewHookWithMatcher("pre-tool", "PreToolUse", "Read", "check.sh", "command", nil, source),
			options: userOptions,
		},
		{
			name: "event-unmappable",
			hook: ir.NewHook("subagent-stop", "SubagentStop", "check.sh", "command", nil, source),
		},
		{
			name: "unsupported-type",
			hook: ir.NewHook("webhook", "PreToolUse", "check.sh", "http", nil, source),
		},
		{
			name: "matcher-unsupported-event",
			hook: ir.NewHookWithMatcher("start", "SessionStart", "Read", "seed.sh", "command", nil, source),
		},
		{
			name: "payload-missing",
			hook: ir.NewHook("empty", "PreToolUse", "", "command", nil, source),
		},
		{
			name: "timeout-invalid",
			hook: ir.NewHookWithOptions("slow", "PreToolUse", "check.sh", "command", nil, source,
				ir.WithHookTimeoutPtr(&negative)),
		},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := translateHook(c.hook, c.options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMergeHookFragments measures the aggregate-merge side of the hook
// path, including the duplicate-collapse comparison. It is the part that grows
// with the number of selected hooks, so a collapse implementation that became
// accidentally superlinear in the common case would show up here.
func BenchmarkMergeHookFragments(b *testing.B) {
	source := ir.SourceRef{File: "hooks/hooks.json", Line: 1}
	matchers := []string{"Read", "Edit", "Write", "Bash", "Grep", "Glob", "WebFetch", "WebSearch"}

	build := func(count int, duplicates bool) []artifact.Artifact {
		fragments := make([]artifact.Artifact, 0, count)
		for i := 0; i < count; i++ {
			matcher := matchers[i%len(matchers)]
			if duplicates {
				matcher = matchers[0]
			}
			hook := ir.NewHookWithMatcher("hook", "PreToolUse", matcher, "guard.sh", "command", nil, source)
			arts, _, err := translateHook(hook, compile.Options{})
			if err != nil {
				b.Fatal(err)
			}
			fragments = append(fragments, arts...)
		}
		return fragments
	}

	cases := []struct {
		name      string
		fragments []artifact.Artifact
	}{
		{name: "distinct-8", fragments: build(8, false)},
		{name: "duplicate-collapse-8", fragments: build(8, true)},
		{name: "duplicate-collapse-64", fragments: build(64, true)},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := artifact.MergeFragments(c.fragments); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
