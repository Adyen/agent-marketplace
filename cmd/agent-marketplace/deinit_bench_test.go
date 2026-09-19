package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func BenchmarkDeinitDryRunWarmLocalGitDiskNoNetwork(b *testing.B) {
	repo, lockPath := setupWarmLockedGitSyncBenchmark(b)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		b.Fatal(err)
	}
	args := []string{"deinit", "--scope", "repo", "--dry-run"}
	if out, err := executeCommand(args...); err != nil || !strings.Contains(out, "Would remove 1 managed artifact(s)") {
		b.Fatalf("deinit positive control: %v\n%s", err, out)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.StartTimer()
	for range b.N {
		config.ResetRepoRootCache()
		if out, err := executeCommand(args...); err != nil {
			b.Fatalf("deinit: %v\n%s", err, out)
		}
	}
	b.StopTimer()
	after, err := os.ReadFile(lockPath)
	if err != nil || string(after) != string(before) {
		b.Fatalf("deinit dry run changed lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".factory", "skills", "review-0", "SKILL.md")); err != nil {
		b.Fatalf("deinit dry run removed installed skill: %v", err)
	}
}
