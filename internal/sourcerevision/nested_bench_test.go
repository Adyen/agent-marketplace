package sourcerevision

import (
	"io"
	"log/slog"
	"testing"
)

func quietRevisionBenchmark(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previous) })
}

func BenchmarkBuildNested(b *testing.B) {
	quietRevisionBenchmark(b)
	root := b.TempDir()
	nestedRevisionFixture(b, root)
	input := Input{SnapshotRoot: root, SourceHarness: "claude"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Build(input); err != nil {
			b.Fatal(err)
		}
	}
}
