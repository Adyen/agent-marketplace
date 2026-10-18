package gemini

import (
	"fmt"
	"testing"
	"testing/fstest"
)

func hookNameCollisionSource(tb testing.TB, count int, grouped bool) fstest.MapFS {
	tb.Helper()
	types := make([]string, count)
	for i := range types {
		types[i] = "command"
	}
	return hookNameSource(tb, map[string][]string{"BeforeTool": types}, grouped)
}

// Include JSON validation/decoding and IR construction, excluding fixture
// creation and disk/network I/O.
func BenchmarkGeminiHookNames(b *testing.B) {
	for _, grouped := range []bool{false, true} {
		for _, count := range []int{1, 64, 512, 2048} {
			b.Run(fmt.Sprintf("grouped=%t/hooks=%d", grouped, count), func(b *testing.B) {
				src := hookNameCollisionSource(b, count, grouped)
				wantLast := "pretooluse-command"
				if count > 1 {
					wantLast = fmt.Sprintf("pretooluse-command-%d", count)
				}
				b.ReportAllocs()
				for b.Loop() {
					hooks, diagnostics, err := LoadHooks(src, settingsPath)
					if err != nil || len(diagnostics) != 0 || len(hooks) != count {
						b.Fatalf("hook load failed: %v, diagnostics=%+v", err, diagnostics)
					}
					if hooks[0].Name() != "pretooluse-command" || hooks[count-1].Name() != wantLast {
						b.Fatal("generated names changed")
					}
				}
			})
		}
	}
}

func BenchmarkGeminiHookNamesBundle(b *testing.B) {
	for _, count := range []int{1, 512} {
		b.Run(fmt.Sprintf("hooks=%d", count), func(b *testing.B) {
			src := hookNameCollisionSource(b, count, true)
			b.ReportAllocs()
			for b.Loop() {
				bundle, err := LoadBundle(src)
				if err != nil || len(bundle.Resources) != count || len(bundle.Diagnostics) != 0 {
					b.Fatalf("bundle load failed: %v, bundle=%+v", err, bundle)
				}
			}
		})
	}
}
