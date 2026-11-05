package yamlfm

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkParseYAMLFrontmatterKeys parses one mapping of unique keys, the
// worst case for yaml.v3's quadratic duplicate-key check. The 64 KiB case is
// the largest accepted block; 1 MiB is above the ceiling.
func BenchmarkParseYAMLFrontmatterKeys(b *testing.B) {
	for _, size := range []int{2 << 10, 64 << 10, 1 << 20} {
		var yaml strings.Builder
		for i := 0; yaml.Len()+len("k0000000: v\n") <= size; i++ {
			fmt.Fprintf(&yaml, "k%07d: v\n", i)
		}
		content := "---\n" + yaml.String() + "---\nbody\n"
		b.Run(fmt.Sprintf("frontmatter=%dKiB", size>>10), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _, _ = ParseYAMLFrontmatter(strings.NewReader(content))
			}
		})
	}
}
