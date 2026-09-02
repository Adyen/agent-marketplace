package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func BenchmarkSyncWarmUnchangedRemoteGitLock(b *testing.B) {
	repoRoot, lockPath := setupWarmLockedGitSyncBenchmark(b)
	previousDir, err := os.Getwd()
	if err != nil {
		b.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		b.Fatalf("enter benchmark repository: %v", err)
	}
	b.Cleanup(func() {
		config.ResetRepoRootCache()
		if err := os.Chdir(previousDir); err != nil {
			b.Errorf("restore working directory: %v", err)
		}
	})

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
	b.StopTimer()

	home := canonicalTempDir(b)
	b.Setenv("HOME", home)
	b.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	b.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	fixtureRoot := canonicalTempDir(b)
	source := filepath.Join(fixtureRoot, "source")
	origin := filepath.Join(fixtureRoot, "origin.git")
	runLockBenchmarkGit(b, "", "init", "--initial-branch=main", source)
	writeLockBenchmarkFile(b, filepath.Join(source, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: Review changes\nmetadata:\n  tags:\n    - benchmark\n---\n\n# Review\n")
	writeLockBenchmarkFile(b, filepath.Join(source, "skills", "review", "scripts", "review.sh"), "#!/bin/sh\necho review\n")
	runLockBenchmarkGit(b, source, "add", ".")
	runLockBenchmarkGit(b, source, "-c", "user.name=Benchmark", "-c", "user.email=benchmark@example.com", "commit", "-m", "seed registry")
	runLockBenchmarkGit(b, "", "clone", "--bare", source, origin)

	repoRoot = filepath.Join(canonicalTempDir(b), "repo")
	configBody := fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: %s\n    ref: main\n    skills:\n      include: [review]\n", origin)
	writeLockBenchmarkFile(b, filepath.Join(repoRoot, ".agents", "config.yaml"), configBody)

	previousDir, err := os.Getwd()
	if err != nil {
		b.Fatalf("get setup working directory: %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		b.Fatalf("enter setup repository: %v", err)
	}
	output, syncErr := executeCommand("sync")
	if err := os.Chdir(previousDir); err != nil {
		b.Fatalf("restore setup working directory: %v", err)
	}
	config.ResetRepoRootCache()
	if syncErr != nil {
		b.Fatalf("seed lock-backed sync: %v\n%s", syncErr, output)
	}
	if !strings.Contains(output, "Installed 1  Removed 0  Malformed 0  Errors 0") {
		b.Fatalf("seed sync did not materialize fixture: %s", output)
	}

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
	if _, err := os.Stat(filepath.Join(repoRoot, ".factory", "skills", "review", "SKILL.md")); err != nil {
		b.Fatalf("stat seeded materialization: %v", err)
	}
	return repoRoot, lockPath
}

func runLockBenchmarkGit(tb testing.TB, dir string, args ...string) {
	tb.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func writeLockBenchmarkFile(tb testing.TB, name, content string) {
	tb.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		tb.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		tb.Fatalf("write fixture %s: %v", name, err)
	}
}
