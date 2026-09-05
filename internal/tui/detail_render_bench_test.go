package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
