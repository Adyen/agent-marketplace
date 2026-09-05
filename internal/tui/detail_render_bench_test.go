package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkCaptureAggregateDetailDiskIO includes bounded local disk reads for a
// refresh containing more maximum-sized entries than either aggregate budget.
func BenchmarkCaptureAggregateDetailDiskIO(b *testing.B) {
	path := filepath.Join(b.TempDir(), "maximum.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxDetailSourceBytes+1)), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(maxDetailCaptureBytes)
	b.ResetTimer()
	for b.Loop() {
		var budget detailCaptureBudget
		for i := 0; i < maxDetailCaptureEntries+16; i++ {
			_ = budget.capture(path)
		}
		if budget.bytes > maxDetailCaptureBytes || budget.entries > maxDetailCaptureEntries {
			b.Fatal("capture exceeded aggregate budget")
		}
	}
}

func BenchmarkRenderBoundedDetail(b *testing.B) {
	path := filepath.Join(b.TempDir(), "detail.md")
	content := "# Detail\n\n" + strings.Repeat("content ", 200)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatal(err)
	}
	r := mkRow("detail", withProject)
	r.Artifact.Path = path
	r.DetailSource = captureDetailSource(path)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if output := renderDetailFullContent(r, 80); output == "" {
			b.Fatal("empty detail")
		}
	}
}
