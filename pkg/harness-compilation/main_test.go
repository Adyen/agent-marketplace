package harnesscompile

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

// TestMain silences the process-wide slog default logger for this package's
// tests and benchmarks.
//
// The library logs compile/ingest start and end, plus every lossy or skipped
// mapping decision. On the benchmark path that turns into one stderr write per
// iteration, which both dominates the measurement and interleaves with the
// `go test -bench` result lines so benchstat cannot parse them. Silencing it
// here keeps the benchmark numbers about compilation and keeps benchstat input
// well-formed.
//
// Set HCC_TEST_LOG=1 to keep the logs when debugging a failure. Tests that
// assert on log output install their own handler with slog.SetDefault and
// restore this one, so they are unaffected either way.
func TestMain(m *testing.M) {
	if os.Getenv("HCC_TEST_LOG") == "" {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	os.Exit(m.Run())
}
