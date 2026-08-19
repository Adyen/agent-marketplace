package acquire

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCloneValidRepo(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"README.md": "hello",
	})

	res, err := Clone(context.Background(), repo, "main", Credentials{}, Limits{}, time.Minute)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	defer func() { _ = res.Release() }()

	data, err := os.ReadFile(filepath.Join(res.Dir, "README.md"))
	if err != nil {
		t.Fatalf("read cloned README: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("README = %q, want hello", data)
	}
	if len(res.Commit) != 40 {
		t.Fatalf("commit = %q, want 40-char hex", res.Commit)
	}
}

func TestCloneInvalidURL(t *testing.T) {
	_, err := Clone(context.Background(), "--option", "main", Credentials{}, Limits{}, time.Minute)
	if err == nil {
		t.Fatalf("expected error for option-like URL")
	}
}

func TestCloneInvalidRef(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"README.md": "hello",
	})

	_, err := Clone(context.Background(), repo, "--bad", Credentials{}, Limits{}, time.Minute)
	if err == nil {
		t.Fatalf("expected error for option-like ref")
	}
}

func TestCloneEnforcesFileBytes(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"small.txt": strings.Repeat("x", 100),
		"large.txt": strings.Repeat("x", 200),
	})

	limits := Limits{RepositoryBytes: 1024 * 1024, FileCount: 1024, FileBytes: 150}
	_, err := Clone(context.Background(), repo, "main", Credentials{}, limits, time.Minute)
	if err == nil {
		t.Fatalf("expected error for file exceeding file_bytes")
	}
	if !strings.Contains(err.Error(), "file_bytes") {
		t.Fatalf("expected file_bytes limit error, got %v", err)
	}
}

func TestCloneEnforcesFileCount(t *testing.T) {
	files := make(map[string]string)
	for i := 0; i < 10; i++ {
		files[fmt.Sprintf("file%d.txt", i)] = "x"
	}
	repo := createGitRepo(t, files)

	limits := Limits{RepositoryBytes: 1024 * 1024, FileCount: 5, FileBytes: 1024}
	_, err := Clone(context.Background(), repo, "main", Credentials{}, limits, time.Minute)
	if err == nil {
		t.Fatalf("expected error for file count exceeding limit")
	}
}

func TestCloneEnforcesRepositoryBytes(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"big.txt": strings.Repeat("x", 500),
	})

	limits := Limits{RepositoryBytes: 100, FileCount: 1024, FileBytes: 1024}
	_, err := Clone(context.Background(), repo, "main", Credentials{}, limits, time.Minute)
	if err == nil {
		t.Fatalf("expected error for repository bytes exceeding limit")
	}
}

func TestCloneContextCancel(t *testing.T) {
	repo := createGitRepo(t, map[string]string{
		"README.md": "hello",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Clone(ctx, repo, "main", Credentials{}, Limits{}, time.Minute)
	if err == nil {
		t.Fatalf("expected error for cancelled context")
	}
}

func createGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	for path, content := range files {
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}

	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "add", "."},
		{"git", "commit", "-q", "-m", "initial"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	return repo
}
