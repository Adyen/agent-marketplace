package acquire

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkClone(b *testing.B) {
	repo := benchRepo(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := Clone(ctx, repo, "main", Credentials{}, Limits{RepositoryBytes: 1024 * 1024, FileCount: 1024, FileBytes: 1024 * 1024}, time.Minute)
		if err != nil {
			b.Fatalf("Clone: %v", err)
		}
		_ = res.Release()
	}
}

func benchRepo(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		b.Fatalf("mkdir repo: %v", err)
	}

	skillDir := filepath.Join(repo, ".agents", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		b.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\nlicense: MIT\n---\nReview.\n"), 0o644); err != nil {
		b.Fatalf("write skill: %v", err)
	}

	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "bench@example.com"},
		{"git", "config", "user.name", "Bench"},
		{"git", "add", "."},
		{"git", "commit", "-q", "-m", "initial"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	return repo
}
