package harnesscompile

import (
	"io"
	"log/slog"
	"testing"
	"testing/fstest"
)

func BenchmarkIngestSourceDefinitionCapture(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(logger) })
	cases := []struct {
		name string
		src  fstest.MapFS
	}{
		{"hook", fstest.MapFS{"hooks/hooks.json": {Data: []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","type":"command","command":"echo source-definition-benchmark"}]}}`)}}},
		{"skill-only", fstest.MapFS{"skills/definition-benchmark/SKILL.md": {Data: []byte("---\nname: definition-benchmark\ndescription: Source capture control\n---\nBody\n")}}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			if _, err := IngestBundle(tc.src, HarnessClaude); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				bundle, err := IngestBundle(tc.src, HarnessClaude)
				if err != nil || len(bundle.Resources) != 1 {
					b.Fatalf("ingest benchmark: resources=%v err=%v", bundle, err)
				}
			}
		})
	}
}
