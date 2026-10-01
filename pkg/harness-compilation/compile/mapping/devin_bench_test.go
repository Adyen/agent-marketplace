package mapping

import "testing"

// The mapping-table lookups below run once per resource per compile, so every
// sync and convert invocation pays for them. Each table gets its own
// sub-benchmark so a regression in one table cannot hide behind the others.

func BenchmarkModel(b *testing.B) {
	cases := []struct {
		name  string
		model string
	}{
		{"devin/stable-alias", "sonnet"},
		{"devin/unmappable-versioned", "claude-sonnet-4-5-20250929"},
		{"devin/miss", "no-such-model"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = Model(c.model, TargetDevin)
			}
		})
	}
}

func BenchmarkHookEvent(b *testing.B) {
	cases := []struct {
		name    string
		event   string
		matcher string
	}{
		{"devin/mapped", "PreToolUse", "Read"},
		{"devin/unmappable", "PreCompact", ""},
		{"devin/miss", "NoSuchEvent", ""},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = HookEvent(c.event, c.matcher, TargetDevin)
			}
		})
	}
}

func BenchmarkMcpInterpolation(b *testing.B) {
	cases := []struct {
		name   string
		source string
	}{
		{"devin/mapped", "${env:VAR}"},
		{"devin/unmappable", "${file:PATH}"},
		{"devin/miss", "no-such-pattern"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = McpInterpolationSupported(c.source, TargetDevin)
			}
		})
	}
}

func BenchmarkEnvToken(b *testing.B) {
	cases := []struct {
		name   string
		source string
	}{
		{"devin/plugin-root", "${CLAUDE_PLUGIN_ROOT}"},
		{"devin/miss", "${NO_SUCH_TOKEN}"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = EnvToken(c.source, TargetDevin)
			}
		})
	}
}
