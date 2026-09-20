package harnesscompile

import (
	"io"
	"log/slog"
	"testing"
	"testing/fstest"
)

func BenchmarkWindsurfHookScope(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
	for _, sample := range []struct {
		name     string
		from, to Harness
		source   fstest.MapFS
	}{
		{"write-to-claude", HarnessWindsurf, HarnessClaude, fstest.MapFS{".windsurf/hooks.json": {Data: []byte(`{"hooks":{"pre_write_code":[{"command":"guard"}]}}`)}}},
		{"native-write", HarnessWindsurf, HarnessWindsurf, fstest.MapFS{".windsurf/hooks.json": {Data: []byte(`{"hooks":{"pre_write_code":[{"command":"guard"}]}}`)}}},
		{"subagent-to-windsurf", HarnessClaude, HarnessWindsurf, fstest.MapFS{"hooks/hooks.json": {Data: []byte(`{"SubagentStop":[{"hooks":[{"type":"command","command":"subagent-only"}]}]}`)}}},
	} {
		b.Run(sample.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Convert(sample.source, sample.from, sample.to); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
