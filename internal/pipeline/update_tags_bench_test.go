package pipeline

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Includes local Git process startup and packed-ref reads, but not clone/fetch,
// fixture construction, configuration edits, locking, or materialization.
func BenchmarkDiscoverTagBump(b *testing.B) {
	for _, n := range []int{0, 32, 2048, 16384} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			tags := make([]string, 0, n+1)
			for i := range n {
				tags = append(tags, fmt.Sprintf("v1.ignored-%06d-%s", i, strings.Repeat("x", 96)))
			}
			tags = append(tags, "v1.2")
			repo := tagBumpRepository(b, tags)
			b.ReportAllocs()
			for b.Loop() {
				got, ok, err := discoverTagBump(repo, "v1.0")
				if err != nil || !ok || got != "v1.2" {
					b.Fatalf("discoverTagBump = %q, %v, %v", got, ok, err)
				}
			}
		})
	}
}

// Measures only in-memory selection, including numeric validation and parsing;
// unlike BenchmarkDiscoverTagBump, no Git process or filesystem is involved.
func BenchmarkSelectTagBumpNumericSuffix(b *testing.B) {
	for _, n := range []int{8, 128, 70_000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			want := "v1." + strings.Repeat("0", n) + "2"
			output := want + "\n"
			b.ReportAllocs()
			for b.Loop() {
				got, err := selectTagBump(strings.NewReader(output), "v1.", 0)
				if err != nil || got != want {
					b.Fatalf("numeric selection: err=%v, matched=%v", err, got == want)
				}
			}
		})
	}
}
