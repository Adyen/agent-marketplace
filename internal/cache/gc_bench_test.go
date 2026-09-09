package cache

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// BenchmarkActiveRegistryHashes measures the cost of building the active-hash
// set consumed by GarbageCollect. GC runs every time a user invokes the CLI's
// cache-cleaning path; the function is linear in total registry count across
// layers but allocation-heavy because of the per-URL SHA-256.
func BenchmarkActiveRegistryHashes(b *testing.B) {
	const registriesPerLayer = 16

	makeConfig := func(prefix string) *config.Config {
		out := &config.Config{}
		for i := 0; i < registriesPerLayer; i++ {
			out.Registries = append(out.Registries, config.RegistryEntry{
				URL: fmt.Sprintf("git@example.com:%s-%d.git", prefix, i),
				Ref: "main",
			})
		}
		// Sprinkle some non-git entries that must be skipped.
		out.Registries = append(out.Registries,
			config.RegistryEntry{URL: "/local/skills"},
			config.RegistryEntry{URL: "   "},
		)
		return out
	}

	cfgs := []*config.Config{
		makeConfig("org"),
		makeConfig("user"),
		makeConfig("repo"),
		nil, // exercise the nil-skip branch
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = activeRegistryHashes(cfgs...)
	}
}

func BenchmarkExactGitGCDryRunFilesystem(b *testing.B) {
	b.Setenv("HOME", b.TempDir())
	cfg := &config.Config{}
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
			cfg.Registries = append(cfg.Registries, config.RegistryEntry{URL: source, Ref: "main"})
		}
		for _, path := range []string{exactGitObjectStorePath(source), exactGitSnapshotPath(source, oid)} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				b.Fatal(err)
			}
		}
	}
	docs := []lockfile.Document{doc}
	if _, err := GarbageCollectWithLocks(true, docs, cfg); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := GarbageCollectWithLocks(true, docs, cfg); err != nil {
			b.Fatal(err)
		}
	}
}
