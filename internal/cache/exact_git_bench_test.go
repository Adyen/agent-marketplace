package cache

import "testing"

func BenchmarkResolveExactGitRef(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	repo := newExactGitRepository(b, "sha1")
	canonical := "https://example.invalid/benchmark"
	resolveExactGitRefForTest(b, canonical, repo, "main")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ResolveExactGitRef(canonical, repo, "main"); err != nil {
			b.Fatal(err)
		}
	}
}
