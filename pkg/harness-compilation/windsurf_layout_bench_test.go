package harnesscompile

import (
	"io"
	"log/slog"
	"testing"
	"testing/fstest"
)

func BenchmarkWindsurfDirectRoot(b *testing.B) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(old) })
	src := directWindsurfFixture()
	src["AGENTS.md"] = &fstest.MapFile{Data: []byte("# notes\n")}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Convert(src, HarnessWindsurf, HarnessWindsurf); err != nil {
			b.Fatal(err)
		}
	}
}
