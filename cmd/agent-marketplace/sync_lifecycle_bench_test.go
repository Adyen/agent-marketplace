package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func BenchmarkSyncLocalGitDiskIONoNetwork(b *testing.B) {
	for _, warm := range []bool{false, true} {
		for _, count := range []int{1, 4} {
			b.Run(fmt.Sprintf("warm=%t/registries=%d", warm, count), func(b *testing.B) {
				repoRoot := setupLocalGitSyncBenchmark(b, count)
				runSyncBenchmark(b)
				lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
				beforeLock, err := os.ReadFile(lockPath)
				if err != nil && !os.IsNotExist(err) {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				b.StartTimer()
				for range b.N {
					if !warm {
						b.StopTimer()
						for _, path := range []string{os.Getenv("XDG_CACHE_HOME"), os.Getenv("XDG_STATE_HOME"), filepath.Join(repoRoot, ".factory"), filepath.Join(repoRoot, ".agents", "lock.yaml")} {
							if err := os.RemoveAll(path); err != nil {
								b.Fatal(err)
							}
						}
						b.StartTimer()
					}
					runSyncBenchmark(b)
				}
				b.StopTimer()
				if warm && beforeLock != nil {
					afterLock, err := os.ReadFile(lockPath)
					if err != nil || string(afterLock) != string(beforeLock) {
						b.Fatalf("unchanged sync changed lock: %v", err)
					}
				}
				for i := range count {
					if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", fmt.Sprintf("review-%d", i), "SKILL.md")); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkSyncMissingLockWarmCacheDiskIONoNetwork(b *testing.B) {
	for _, count := range []int{1, 4} {
		b.Run(fmt.Sprintf("registries=%d", count), func(b *testing.B) {
			repo := setupLocalGitSyncBenchmark(b, count)
			runSyncBenchmark(b)
			lockPath := filepath.Join(repo, ".agents", "lock.yaml")
			b.ReportAllocs()
			b.ResetTimer()
			b.StartTimer()
			for range b.N {
				b.StopTimer()
				if err := os.Remove(lockPath); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				runSyncBenchmark(b)
			}
		})
	}
}

func setupLocalGitSyncBenchmark(b *testing.B, count int) string {
	b.Helper()
	b.StopTimer()
	return setupLocalGitSyncFixture(b, count)
}

func setupLocalGitSyncFixture(b testing.TB, count int) string {
	b.Helper()
	root, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	home := filepath.Join(root, "home")
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "AGENT_MARKETPLACE_") {
			b.Setenv(key, "")
		}
	}
	for key, value := range map[string]string{
		"HOME":                              home,
		"XDG_CACHE_HOME":                    filepath.Join(home, ".cache"),
		"XDG_STATE_HOME":                    filepath.Join(home, ".local", "state"),
		"XDG_CONFIG_HOME":                   filepath.Join(home, ".config"),
		"CODEX_HOME":                        filepath.Join(home, ".codex"),
		"GIT_CONFIG_GLOBAL":                 os.DevNull,
		"GIT_CONFIG_NOSYSTEM":               "1",
		"GIT_TERMINAL_PROMPT":               "0",
		"GIT_CONFIG_COUNT":                  "0",
		"GIT_AUTHOR_NAME":                   "Benchmark",
		"GIT_AUTHOR_EMAIL":                  "benchmark@example.com",
		"GIT_COMMITTER_NAME":                "Benchmark",
		"GIT_COMMITTER_EMAIL":               "benchmark@example.com",
		"AGENT_MARKETPLACE_LOG_ENABLED":     "false",
		"AGENT_MARKETPLACE_TELEMETRY_LEVEL": "off",
	} {
		b.Setenv(key, value)
	}
	repoRoot := filepath.Join(root, "repo")
	var body strings.Builder
	body.WriteString("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n")
	for i := range count {
		name := fmt.Sprintf("review-%d", i)
		source := filepath.Join(root, fmt.Sprintf("source-%d", i))
		origin := filepath.Join(root, fmt.Sprintf("origin-%d.git", i))
		runSyncBenchmarkGit(b, "", "init", "--initial-branch=main", source)
		writeSyncBenchmarkFile(b, filepath.Join(source, "skills", name, "SKILL.md"), fmt.Sprintf("---\nname: %s\ndescription: Review changes\n---\n\n# Review\n", name))
		writeSyncBenchmarkFile(b, filepath.Join(source, "skills", name, "scripts", "review.sh"), "#!/bin/sh\necho review\n")
		runSyncBenchmarkGit(b, source, "add", ".")
		runSyncBenchmarkGit(b, source, "-c", "commit.gpgsign=false", "-c", "user.name=Benchmark", "-c", "user.email=benchmark@example.com", "commit", "-m", "seed registry")
		runSyncBenchmarkGit(b, "", "clone", "--bare", source, origin)
		fmt.Fprintf(&body, "  %s:\n    git: file://%s\n    ref: main\n", name, origin)
	}
	body.WriteString("resources:\n")
	for i := range count {
		name := fmt.Sprintf("review-%d", i)
		fmt.Fprintf(&body, "  %s:\n    include: [skill/%s]\n", name, name)
	}
	writeSyncBenchmarkFile(b, filepath.Join(repoRoot, ".agents", "config.yaml"), body.String())
	b.Chdir(repoRoot)
	b.Cleanup(config.ResetRepoRootCache)
	return repoRoot
}

func runSyncBenchmark(b *testing.B) {
	b.Helper()
	config.ResetRepoRootCache()
	if output, err := executeCommand("sync"); err != nil {
		b.Fatalf("sync: %v\n%s", err, output)
	}
}

func runSyncBenchmarkGit(tb testing.TB, dir string, args ...string) {
	tb.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func writeSyncBenchmarkFile(tb testing.TB, name, content string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		tb.Fatal(err)
	}
}
