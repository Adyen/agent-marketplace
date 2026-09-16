package cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkCloneOrPull(b *testing.B) {
	b.Setenv("HOME", b.TempDir())

	origin, working := createBareRepoForBenchmark(b)
	writeAndPushCommitForBenchmark(b, working, "skill.txt", "version one\n", "initial commit")

	// Pre-warm the cache so the benchmark loop measures the fetch+checkout path.
	if _, err := CloneOrPull(origin, "main"); err != nil {
		b.Fatalf("pre-warm CloneOrPull: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CloneOrPull(origin, "main"); err != nil {
			b.Fatalf("CloneOrPull: %v", err)
		}
	}
}

func BenchmarkValidateGitURL(b *testing.B) {
	urls := []string{
		"https://example.com/org/repo.git",
		"git@example.com:org/repo.git",
		"ext::sh -c 'evil'",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, u := range urls {
			_ = validateGitURL(u)
		}
	}
}

func BenchmarkValidateGitRef(b *testing.B) {
	refs := []string{
		"main",
		"v1.2.3",
		"main@{1.day.ago}",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ref := range refs {
			_ = validateGitRef(ref)
		}
	}
}

func createBareRepoForBenchmark(b *testing.B) (origin, working string) {
	b.Helper()

	baseDir := b.TempDir()
	source := filepath.Join(baseDir, "source")
	origin = filepath.Join(baseDir, "origin.git")
	working = filepath.Join(baseDir, "working")

	runGitForBenchmark(b, "", "init", "--initial-branch=main", source)
	writeFileForBenchmark(b, filepath.Join(source, ".gitkeep"), "seed\n")
	runGitForBenchmark(b, source, "add", ".gitkeep")
	runGitForBenchmark(b, source, "commit", "-m", "seed")

	runGitForBenchmark(b, "", "clone", "--bare", source, origin)
	runGitForBenchmark(b, "", "clone", origin, working)

	return origin, working
}

func writeAndPushCommitForBenchmark(b *testing.B, repoDir, relPath, content, message string) {
	b.Helper()

	writeFileForBenchmark(b, filepath.Join(repoDir, relPath), content)
	runGitForBenchmark(b, repoDir, "add", relPath)
	runGitForBenchmark(b, repoDir, "commit", "-m", message)
	runGitForBenchmark(b, repoDir, "push", "origin", "main")
}

func writeFileForBenchmark(b *testing.B, path, content string) {
	b.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatalf("mkdir %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatalf("write %q: %v", path, err)
	}
}

func runGitForBenchmark(b *testing.B, dir string, args ...string) string {
	b.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Benchmark User",
		"GIT_AUTHOR_EMAIL=bench@example.com",
		"GIT_COMMITTER_NAME=Benchmark User",
		"GIT_COMMITTER_EMAIL=bench@example.com",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		b.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
