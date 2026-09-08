package cache

import (
	"context"
	"fmt"
	"testing"
)

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

func BenchmarkVerifyExactGitTrackedTreeDiskNoNetwork(b *testing.B) {
	for _, algorithm := range []string{"sha1", "sha256"} {
		for _, size := range []int{4 << 10, 8 << 20} {
			b.Run(fmt.Sprintf("%s/%d", algorithm, size), func(b *testing.B) {
				root := exactGitBlobFixture(b, algorithm, size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if err := verifyExactGitTrackedTreeWithContext(context.Background(), root, algorithm); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkGitAuthEnvironment(b *testing.B) {
	isolatedGitAuth(b)
	b.Setenv("GIT_CONFIG_SYSTEM", authFile(b, "system", "[credential]\n\thelper = fixture-helper\n", 0o600))
	b.Setenv("GIT_CONFIG_GLOBAL", authFile(b, "global", "[credential \"https://example.invalid\"]\n\thelper =\n\thelper = fixture-helper\n", 0o600))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := gitAuthEnvironment(context.Background(), "", gitEnv(), "https://example.invalid/fixture"); err != nil {
			b.Fatal(err)
		}
	}
}
