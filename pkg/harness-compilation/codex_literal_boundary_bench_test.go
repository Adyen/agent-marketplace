package harnesscompile

import (
	"io"
	"log/slog"
	"testing"
)

func BenchmarkCodexLiteralConversion(b *testing.B) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(old) })
	for _, value := range []string{"ordinary", "$SYNTHETIC_KEY"} {
		b.Run(value, func(b *testing.B) {
			src := codexLiteralFixture(value)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Convert(src, HarnessCodex, HarnessWindsurf); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
