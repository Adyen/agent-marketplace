package harnesscompile

import (
	"io"
	"log/slog"
	"testing"
	"testing/fstest"
)

func BenchmarkMCPAbsenceIdentity(b *testing.B) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(old) })
	for _, present := range []bool{false, true} {
		name := "missing"
		if present {
			name = "present"
		}
		b.Run(name, func(b *testing.B) {
			src, p := absenceIdentityFixture(HarnessWindsurf)
			if present {
				src[p] = &fstest.MapFile{Data: []byte(`{"mcpServers":{"fixture":{"command":"fixture"}}}`)}
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Convert(src, HarnessWindsurf, TargetFactory); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
