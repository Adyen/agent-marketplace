package acquire

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkClone(b *testing.B) {
	repo := benchRepo(b)
	ctx := context.Background()
	limits := Limits{RepositoryBytes: 8 * 1024 * 1024, FileCount: 1024, FileBytes: 2 * 1024 * 1024}

	for _, tc := range []struct {
		name  string
		roots []string
	}{
		{name: "full"},
		{name: "selected-root", roots: []string{".agents"}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				res, err := Clone(ctx, "file://"+repo, "main", Credentials{}, limits, time.Minute, tc.roots...)
				if err != nil {
					b.Fatalf("Clone: %v", err)
				}
				_ = res.Release()
			}
		})
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
	unrelated := filepath.Join(repo, "unrelated")
	if err := os.MkdirAll(unrelated, 0o755); err != nil {
		b.Fatalf("mkdir unrelated: %v", err)
	}
	for i := range 4 {
		data := make([]byte, 1024*1024)
		if _, err := rand.Read(data); err != nil {
			b.Fatalf("random data: %v", err)
		}
		if err := os.WriteFile(filepath.Join(unrelated, fmt.Sprintf("blob-%d.bin", i)), data, 0o644); err != nil {
			b.Fatalf("write unrelated blob: %v", err)
		}
	}

	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "bench@example.com"},
		{"git", "config", "user.name", "Bench"},
		{"git", "config", "uploadpack.allowFilter", "true"},
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
