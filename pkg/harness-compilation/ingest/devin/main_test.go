package devin

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

// TestMain silences the process-wide slog default logger for this package's
// tests and benchmarks, mirroring the root package's TestMain for the same
// reason.
//
// The ingester logs a start/end pair per bundle plus one line per lossy
// decision. On the benchmark path that is one stderr write per iteration, which
// both dominates the measurement and interleaves with the `go test -bench`
// result lines so benchstat cannot parse them.
//
// Set HCC_TEST_LOG=1 to keep the logs when debugging a failure.
func TestMain(m *testing.M) {
	if os.Getenv("HCC_TEST_LOG") == "" {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	os.Exit(m.Run())
}
