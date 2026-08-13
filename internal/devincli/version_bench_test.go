package devincli

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkDetect measures the non-exec cost of a probe. doctor runs
// interactively, so the classification path is on a user-visible latency path
// even though the dominant cost in production is the child process itself.
func BenchmarkDetect(b *testing.B) {
	banner := []byte(fmt.Sprintf("devin %d.%d.27 (0becb483)\n", FloorMajor, FloorMinor))
	lookPath := func(string) (string, error) { return "/usr/local/bin/devin", nil }
	run := func(context.Context, string) ([]byte, error) { return banner, nil }
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if probe := Detect(ctx, WithLookPath(lookPath), WithRunner(run)); probe.Status != StatusSupported {
			b.Fatalf("unexpected status %q", probe.Status)
		}
	}
}
