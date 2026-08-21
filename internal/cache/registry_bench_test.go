package cache

import (
	"strings"
	"testing"
)

// BenchmarkCacheRefComponent covers the ref encoding, which runs on every path
// construction — RegistryCachePath is called for every registry entry on every
// status, sync and TUI refresh.
func BenchmarkCacheRefComponent(b *testing.B) {
	cases := map[string]string{
		"short":  "main",
		"nested": "refs/heads/feature/security-hardening",
		"digest": strings.Repeat("release-candidate/", 40),
	}
	for name, ref := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if cacheRefComponent(ref) == "" {
					b.Fatal("empty component")
				}
			}
		})
	}
}

// BenchmarkGitInvocation measures what descriptor anchoring costs: "anchored" is
// the production path (re-exec of this binary, which fchdirs and becomes git),
// "unanchored" is a plain git spawn with a path working directory. The delta is
// the price of closing the fd-to-path window, and it is paid once per git command
// — of which a warm refresh runs three.
func BenchmarkGitInvocation(b *testing.B) {
	homeDir := b.TempDir()
	b.Setenv("HOME", homeDir)

	store, err := openCacheStore(CacheDir(), HashURL("https://example.com/org/registry.git"))
	if err != nil {
		b.Fatalf("open cache store: %v", err)
	}
	defer store.close()

	b.Run("anchored", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := runGitIn(store.dir(), "--version"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("unanchored", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := runGit(store.dir().Name(), "--version"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
