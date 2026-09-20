package harnesscompile

import (
	"io"
	"log/slog"
	"testing"
)

func BenchmarkFactoryIdentityConversion(b *testing.B) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(old) })
	src := factoryIdentityFixture("commands", ".md")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Convert(src, HarnessFactory, TargetFactory); err != nil {
			b.Fatal(err)
		}
	}
}
