package lockfile

import (
	"fmt"
	"testing"
)

// BenchmarkProjectGitRoots measures matching only, with no disk or network I/O.
func BenchmarkProjectGitRoots(b *testing.B) {
	for _, mode := range []string{"explicit", "implicit", "paired"} {
		for _, count := range []int{1, 32, 256, 2048} {
			b.Run(fmt.Sprintf("%s/%d", mode, count), func(b *testing.B) {
				var roots []GitRoot
				var wants []GitRootRequirement
				for i := range count {
					path := fmt.Sprintf("skills/team-%04d", i)
					roots = append(roots, GitRoot{Path: path, SourceHarness: "claude", RevisionDigest: Digest(sha256a)})
					if mode == "paired" {
						roots = append(roots, GitRoot{Path: path, SourceHarness: "factory", RevisionDigest: Digest(sha256a)})
						wants = append(wants, GitRootRequirement{Path: path}, GitRootRequirement{Path: path, SourceHarness: "factory"}, GitRootRequirement{Path: path, SourceHarness: "claude"})
					} else {
						harness := ""
						if mode == "explicit" {
							harness = "claude"
						}
						wants = append(wants, GitRootRequirement{Path: path, SourceHarness: harness})
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					projected, ok := ProjectGitRoots(roots, wants)
					if !ok || len(projected) != len(roots) {
						b.Fatalf("projection = %d/%v, want %d/true", len(projected), ok, len(roots))
					}
				}
			})
		}
	}
}
