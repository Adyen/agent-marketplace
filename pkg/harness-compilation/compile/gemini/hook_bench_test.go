package gemini

import (
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkNativeHookCompile(b *testing.B) {
	hook := ir.NewHookWithMatcherOptions("audit", "PreToolUse", "Read|Edit", "echo audit", "command", map[string]any{"description": "audit commands", "env": map[string]any{"AUDIT_MODE": "local"}}, ir.SourceRef{}, ir.WithHookTimeout(2500))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := translateHook(hook, compile.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}
