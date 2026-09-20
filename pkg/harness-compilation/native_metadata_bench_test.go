package harnesscompile

import (
	"io"
	"log/slog"
	"testing"
	"testing/fstest"
)

func BenchmarkNativeMetadataConversion(b *testing.B) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(old) })
	for _, tc := range []struct {
		name, path, body string
		from, to         Harness
	}{
		{"opaque-workflow", "commands/demo.md", "auto_execution_mode: always\n", HarnessFactory, HarnessWindsurf},
		{"native-workflow", ".windsurf/workflows/demo.md", "auto_execution_mode: always\n", HarnessWindsurf, HarnessWindsurf},
		{"opaque-inline-mcp", "droids/demo.md", "mcpServers:\n  hidden:\n    command: fixture\n", HarnessFactory, HarnessGemini},
	} {
		b.Run(tc.name, func(b *testing.B) {
			src := fstest.MapFS{tc.path: {Data: []byte("---\nname: demo\ndescription: Demo\n" + tc.body + "---\nBody\n")}}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Convert(src, tc.from, tc.to); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
