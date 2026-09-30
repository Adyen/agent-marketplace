package cache

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// BenchmarkActiveRegistryHashes measures the cost of building the active-hash
// set consumed by GarbageCollect. GC runs every time a user invokes the CLI's
// cache-cleaning path; the function is linear in total registry count across
// layers but allocation-heavy because of the per-URL SHA-256.
func BenchmarkActiveRegistryHashes(b *testing.B) {
	const registriesPerLayer = 16

	var sources []string
	for l := 0; l < 3; l++ {
		for i := 0; i < registriesPerLayer; i++ {
			sources = append(sources, fmt.Sprintf("git@example.com:%s-%d.git", []string{"org", "user", "repo"}[l], i))
		}
	}
	// Sprinkle a blank entry that must be skipped.
	sources = append(sources, "   ")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = activeRegistryHashes(sources)
	}
}

func BenchmarkExactGitGCDryRunFilesystem(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	var sources []string
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV1, Scope: lockfile.ScopeRepo}
	oid, err := ParseGitOID("sha1:" + strings.Repeat("1", 40))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 48; i++ {
		source := fmt.Sprintf("https://example.com/catalog-%d", i)
		if i < 16 {
			doc.Registries = append(doc.Registries, lockfile.GitRegistry{URL: source, Ref: "main", Commit: oid})
		} else if i < 32 {
			sources = append(sources, source)
		}
		for _, path := range []string{exactGitObjectStorePath(source), exactGitSnapshotPath(source, oid)} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				b.Fatal(err)
			}
		}
	}
	reader := staticReachability([]lockfile.Document{doc}, sources...)
	if _, err := GarbageCollect(context.Background(), true, reader); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := GarbageCollect(context.Background(), true, reader); err != nil {
			b.Fatal(err)
		}
	}
}
