package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func BenchmarkMergeInstallBindingsInMemory(b *testing.B) {
	commit, err := lockfile.ParseGitOID("sha1:" + strings.Repeat("a", 40))
	if err != nil {
		b.Fatal(err)
	}
	digest := lockfile.Digest("sha256:" + strings.Repeat("b", 64))
	for _, count := range []int{1, 16} {
		b.Run(fmt.Sprintf("registries=%d", count), func(b *testing.B) {
			base := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: lockfile.ScopeRepo}
			for i := range count {
				base.Registries = append(base.Registries, lockfile.GitRegistry{
					URL: fmt.Sprintf("https://example.invalid/registry-%d", i), Ref: "main", Commit: commit,
					Roots: []lockfile.GitRoot{{Path: ".", SourceHarness: "claude", RevisionSchema: "v3", RevisionDigest: digest,
						Resources: []lockfile.GitResource{{Kind: "skill", Name: "review", ContentDigest: digest, ArchiveDigest: digest}}}},
				})
			}
			base, err := lockfile.Normalize(base)
			if err != nil {
				b.Fatal(err)
			}
			bindings := append([]lockfile.GitRegistry(nil), base.Registries...)
			b.ReportAllocs()
			for b.Loop() {
				merged, err := mergeInstallBindings(base, lockfile.ScopeRepo, bindings)
				if err != nil || len(merged.Registries) != count {
					b.Fatalf("merge: registries=%d error=%v", len(merged.Registries), err)
				}
			}
		})
	}
}

func BenchmarkProjectLockedRoots(b *testing.B) {
	roots := make([]lockfile.GitRoot, 0, 32)
	entries := make([]config.RegistryEntry, 0, 64)
	for i := range 32 {
		path := fmt.Sprintf("skills/team-%d", i)
		roots = append(roots, lockfile.GitRoot{Path: path, SourceHarness: "factory"})
		entries = append(entries, config.RegistryEntry{Path: path}, config.RegistryEntry{Path: path, SourceHarness: "factory"})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		projected, ok := projectLockedRoots(roots, entries)
		if !ok || len(projected) != 32 {
			b.Fatal("matching roots rejected")
		}
	}
}

func BenchmarkLoadStableEffectiveIncludes(b *testing.B) {
	root, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Setenv("HOME", root)
	for name, data := range map[string]string{
		"config.yaml": "apiVersion: agent-marketplace/v2\ninclude: [middle.yaml, leaf.yaml]\n",
		"middle.yaml": "include: leaf.yaml\n",
		"leaf.yaml":   "harnesses: [claude]\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	opts := config.LoadEffectiveOptions{RepoRoot: root, LocalConfigDir: root, OrganizationPath: filepath.Join(root, "missing-org"), GlobalPath: filepath.Join(root, "config.yaml")}
	for _, test := range []struct {
		name string
		load func(context.Context, config.LoadEffectiveOptions) config.EffectiveConfig
	}{
		{"raw", func(ctx context.Context, opts config.LoadEffectiveOptions) config.EffectiveConfig {
			return pipeline.LoadEffective(ctx, pipeline.ConfigLoadStrict, opts, nil)
		}},
		{"online", func(ctx context.Context, opts config.LoadEffectiveOptions) config.EffectiveConfig {
			return pipeline.LoadEffective(ctx, pipeline.ConfigLoadOnline, opts, nil)
		}},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := loadStableEffectiveWith(context.Background(), opts, test.load); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkInstallFromWarmExactLock measures repeated warm remote installs.
// This benchmark includes local disk I/O but excludes network requests (no Internet).
func BenchmarkInstallFromWarmExactLock(b *testing.B) {
	repoRoot, path := setupWarmLockedGitSyncBenchmark(b)
	cfg, err := config.LoadConfigFromPath(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if err != nil || cfg == nil || len(cfg.Registries) != 1 {
		b.Fatalf("load benchmark registry: %v", err)
	}
	args := []string{"install", "review-0", "--from", cfg.Registries[0].URL, "--ref", cfg.Registries[0].Ref}
	if out, err := executeCommand(args...); err != nil {
		b.Fatalf("warm install: %v\n%s", err, out)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.StartTimer()
	for range b.N {
		config.ResetRepoRootCache()
		if out, err := executeCommand(args...); err != nil {
			b.Fatalf("install: %v\n%s", err, out)
		}
	}
	b.StopTimer()
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		b.Fatalf("warm install changed lock: %v", err)
	}
}

func BenchmarkConfiguredInstallWarmExactLockLocalGitDiskNoNetwork(b *testing.B) {
	_, path := setupWarmLockedGitSyncBenchmark(b)
	args := []string{"install", "review-0"}
	if out, err := executeCommand(args...); err != nil {
		b.Fatalf("warm install: %v\n%s", err, out)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.StartTimer()
	for range b.N {
		config.ResetRepoRootCache()
		if out, err := executeCommand(args...); err != nil {
			b.Fatalf("install: %v\n%s", err, out)
		}
	}
	b.StopTimer()
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		b.Fatalf("warm install changed lock: %v", err)
	}
}

func BenchmarkRemoveBlockedResourceLinkLocalGitDiskNoNetwork(b *testing.B) {
	for _, kind := range []string{"command", "subagent"} {
		b.Run(kind, func(b *testing.B) {
			b.StopTimer()
			repo := setupLocalGitSyncFixture(b, 0)
			source := filepath.Join(filepath.Dir(repo), "resource-source")
			origin := filepath.Join(filepath.Dir(repo), "resource-origin.git")
			dir, native := "commands", filepath.Join(repo, ".devin", "skills", "remove")
			if kind == "subagent" {
				dir, native = "agents", filepath.Join(repo, ".devin", "agents", "remove.md")
			}
			runSyncBenchmarkGit(b, "", "init", "--initial-branch=main", source)
			writeSyncBenchmarkFile(b, filepath.Join(source, dir, "remove.md"), "---\nname: remove\ndescription: Removal benchmark\n---\n\nReview changes.\n")
			runSyncBenchmarkGit(b, source, "add", ".")
			runSyncBenchmarkGit(b, source, "-c", "commit.gpgsign=false", "commit", "-m", "seed resource")
			runSyncBenchmarkGit(b, "", "clone", "--bare", source, origin)
			configPath := filepath.Join(repo, ".agents", "config.yaml")
			body := "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nregistries:\n  - url: file://" + origin + "\n    ref: main\n    source_harness: claude\n    default: true\n"
			writeSyncBenchmarkFile(b, configPath, body)
			runSyncBenchmark(b)
			linkTarget, err := os.Readlink(native)
			if err != nil {
				b.Fatal(err)
			}
			writeSyncBenchmarkFile(b, filepath.Join(os.Getenv("HOME"), ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nallowed_registries: []\n")
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
					b.Fatal(err)
				}
				if err := os.Remove(native); err != nil && !os.IsNotExist(err) {
					b.Fatal(err)
				}
				if err := os.Symlink(linkTarget, native); err != nil {
					b.Fatal(err)
				}
				config.ResetRepoRootCache()
				b.StartTimer()
				if out, err := executeCommand("remove", "remove", "--kind", kind, "--from", "file://"+origin, "--ref", "main"); err != nil {
					b.Fatalf("blocked removal: %v\n%s", err, out)
				}
				if _, err := os.Lstat(native); !os.IsNotExist(err) {
					b.Fatalf("removed link remains: %v", err)
				}
			}
		})
	}
}

func BenchmarkRemoveBlockedSkillSymlinkLocalGitDiskNoNetwork(b *testing.B) {
	repo, _ := setupWarmLockedGitSyncBenchmark(b)
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		b.Fatal(err)
	}
	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil || cfg == nil || len(cfg.Registries) != 1 {
		b.Fatalf("load removal fixture: %v", err)
	}
	link := filepath.Join(repo, ".factory", "skills", "review-0")
	target, err := os.Readlink(link)
	if err != nil {
		b.Fatal(err)
	}
	writeSyncBenchmarkFile(b, filepath.Join(os.Getenv("HOME"), ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nallowed_registries: []\n")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := os.WriteFile(configPath, before, 0o644); err != nil {
			b.Fatal(err)
		}
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			b.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			b.Fatal(err)
		}
		config.ResetRepoRootCache()
		b.StartTimer()
		if out, err := executeCommand("remove", "review-0", "--kind", "skill", "--from", cfg.Registries[0].URL, "--ref", cfg.Registries[0].Ref); err != nil {
			b.Fatalf("blocked removal: %v\n%s", err, out)
		}
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			b.Fatalf("removed link remains: %v", err)
		}
	}
}

func BenchmarkSyncWarmUnchangedRemoteGitLock(b *testing.B) {
	_, lockPath := setupWarmLockedGitSyncBenchmark(b)

	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		b.Fatalf("read seeded lock: %v", err)
	}
	b.Cleanup(func() {
		afterLock, err := os.ReadFile(lockPath)
		if err != nil {
			b.Errorf("read lock after benchmark: %v", err)
			return
		}
		if string(afterLock) != string(beforeLock) {
			b.Error("unchanged sync rewrote the lock")
		}
	})
	if output, err := executeCommand("sync"); err != nil {
		b.Fatalf("warm-up sync: %v\n%s", err, output)
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.StartTimer()
	for range b.N {
		config.ResetRepoRootCache()
		if output, err := executeCommand("sync"); err != nil {
			b.Fatalf("unchanged normal sync: %v\n%s", err, output)
		}
	}
}

func setupWarmLockedGitSyncBenchmark(b *testing.B) (repoRoot, lockPath string) {
	b.Helper()
	repoRoot = setupLocalGitSyncBenchmark(b, 1)
	runSyncBenchmark(b)

	lockPath = filepath.Join(repoRoot, ".agents", "lock.yaml")
	doc, err := lockfile.Read(lockPath)
	if err != nil {
		b.Fatalf("read seeded exact lock: %v", err)
	}
	if len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 || len(doc.Registries[0].Roots[0].Resources) != 1 {
		b.Fatalf("seeded lock lacks exact registry inventory: %+v", doc.Registries)
	}
	oid, err := cache.ParseGitOID(doc.Registries[0].Commit.String())
	if err != nil {
		b.Fatalf("parse seeded lock commit: %v", err)
	}
	if _, err := cache.OpenExactGit(doc.Registries[0].URL, oid); err != nil {
		b.Fatalf("open seeded exact snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "review-0", "SKILL.md")); err != nil {
		b.Fatalf("stat seeded materialization: %v", err)
	}
	return repoRoot, lockPath
}
