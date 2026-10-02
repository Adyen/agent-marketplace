package analyze

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkDenseCommentSourceRanges(b *testing.B) {
	for _, count := range []int{128, 512, 2048} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			content := strings.Repeat("é<!-- inert note -->\n", count)
			b.SetBytes(int64(len(content)))
			b.ReportAllocs()
			for b.Loop() {
				if findings := ScanHTMLComments(content, "inert.md", "fixture"); len(findings) != count {
					b.Fatalf("finding count = %d", len(findings))
				}
			}
		})
	}
}
