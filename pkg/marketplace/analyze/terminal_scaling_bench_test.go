package analyze

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkUnterminatedOSC(b *testing.B) {
	for _, count := range []int{128, 512, 2048} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			content := strings.Repeat("\u00e9\x1b]inert\n", count)
			b.SetBytes(int64(len(content)))
			b.ReportAllocs()
			for b.Loop() {
				if findings := ScanTerminalControl(content, "fixture.md", "fixture"); len(findings) != count {
					b.Fatalf("findings = %d, want %d", len(findings), count)
				}
			}
		})
	}
}
