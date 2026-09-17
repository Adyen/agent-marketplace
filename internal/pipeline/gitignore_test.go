package pipeline

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func writeGitignoreLocalConfig(t *testing.T, repo string) {
	t.Helper()
	dir := filepath.Join(repo, ".agents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.local.yaml"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureLocalConfigGitignored(t *testing.T) {
	t.Run("symlink preserves external sentinel", func(t *testing.T) {
		repo := t.TempDir()
		writeGitignoreLocalConfig(t, repo)
		outside := filepath.Join(t.TempDir(), "outside-ignore")
		if err := os.WriteFile(outside, []byte("sentinel\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitignore := filepath.Join(repo, ".gitignore")
		if err := os.Symlink(outside, gitignore); err != nil {
			t.Fatal(err)
		}

		EnsureLocalConfigGitignored(repo)

		got, err := os.ReadFile(outside)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "sentinel\n" {
			t.Fatalf("external sentinel = %q", got)
		}
		info, err := os.Lstat(gitignore)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("gitignore symlink was replaced")
		}
		target, err := os.Readlink(gitignore)
		if err != nil {
			t.Fatal(err)
		}
		if target != outside {
			t.Fatalf("symlink target = %q, want %q", target, outside)
		}
	})

	t.Run("dangling symlink preserves absent target", func(t *testing.T) {
		repo := t.TempDir()
		writeGitignoreLocalConfig(t, repo)
		outside := filepath.Join(t.TempDir(), "missing-ignore")
		gitignore := filepath.Join(repo, ".gitignore")
		if err := os.Symlink(outside, gitignore); err != nil {
			t.Fatal(err)
		}

		EnsureLocalConfigGitignored(repo)

		if _, err := os.Lstat(outside); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dangling target stat error = %v", err)
		}
		if info, err := os.Lstat(gitignore); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("gitignore symlink = %v, %v", info, err)
		}
	})

	t.Run("hard link preserves external sentinel", func(t *testing.T) {
		repo := t.TempDir()
		writeGitignoreLocalConfig(t, repo)
		outside := filepath.Join(t.TempDir(), "outside-ignore")
		if err := os.WriteFile(outside, []byte("sentinel\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(outside, filepath.Join(repo, ".gitignore")); err != nil {
			t.Fatal(err)
		}

		EnsureLocalConfigGitignored(repo)

		got, err := os.ReadFile(outside)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "sentinel\n" {
			t.Fatalf("external sentinel = %q", got)
		}
	})

	t.Run("directory is rejected", func(t *testing.T) {
		repo := t.TempDir()
		writeGitignoreLocalConfig(t, repo)
		gitignore := filepath.Join(repo, ".gitignore")
		if err := os.Mkdir(gitignore, 0o700); err != nil {
			t.Fatal(err)
		}

		EnsureLocalConfigGitignored(repo)

		entries, err := os.ReadDir(gitignore)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("directory entries = %d, want 0", len(entries))
		}
	})

	t.Run("fifo is rejected without hanging", func(t *testing.T) {
		repo := t.TempDir()
		writeGitignoreLocalConfig(t, repo)
		if err := syscall.Mkfifo(filepath.Join(repo, ".gitignore"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEnsureLocalConfigGitignoredFIFOChild$", "-test.v")
		cmd.Env = append(os.Environ(), "PIPELINE_GITIGNORE_FIFO_REPO="+repo)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		if ctx.Err() != nil {
			t.Fatalf("FIFO child timed out: %v\n%s", ctx.Err(), output.String())
		}
		if err != nil {
			t.Fatalf("FIFO child error: %v\n%s", err, output.String())
		}
	})

	t.Run("no local config leaves gitignore absent", func(t *testing.T) {
		repo := t.TempDir()
		EnsureLocalConfigGitignored(repo)
		if _, err := os.Lstat(filepath.Join(repo, ".gitignore")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("gitignore stat error = %v", err)
		}
	})

	t.Run("missing gitignore creates both entries", func(t *testing.T) {
		repo := t.TempDir()
		writeGitignoreLocalConfig(t, repo)
		EnsureLocalConfigGitignored(repo)
		got, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != ".agents/config.local.yaml\n.agents/lock.local.yaml\n" {
			t.Fatalf("gitignore = %q", got)
		}
	})

	for _, tc := range []struct {
		name    string
		initial string
		want    string
	}{
		{name: "newline", initial: "user-rule\n", want: "user-rule\n.agents/config.local.yaml\n.agents/lock.local.yaml\n"},
		{name: "no newline", initial: "user-rule", want: "user-rule\n.agents/config.local.yaml\n.agents/lock.local.yaml\n"},
	} {
		t.Run("regular "+tc.name, func(t *testing.T) {
			repo := t.TempDir()
			writeGitignoreLocalConfig(t, repo)
			gitignore := filepath.Join(repo, ".gitignore")
			if err := os.WriteFile(gitignore, []byte(tc.initial), 0o600); err != nil {
				t.Fatal(err)
			}
			EnsureLocalConfigGitignored(repo)
			got, err := os.ReadFile(gitignore)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("gitignore = %q, want %q", got, tc.want)
			}
			info, err := os.Stat(gitignore)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
			}
			matches, err := filepath.Glob(filepath.Join(repo, "*.tmp"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 0 {
				t.Fatalf("temporary residue = %v", matches)
			}
		})
	}

	t.Run("existing entries are a byte and mtime no-op", func(t *testing.T) {
		repo := t.TempDir()
		writeGitignoreLocalConfig(t, repo)
		gitignore := filepath.Join(repo, ".gitignore")
		initial := "user-rule\n.agents/config.local.yaml\n.agents/lock.local.yaml\n"
		if err := os.WriteFile(gitignore, []byte(initial), 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(gitignore)
		if err != nil {
			t.Fatal(err)
		}
		EnsureLocalConfigGitignored(repo)
		after, err := os.Stat(gitignore)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(gitignore)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != initial || !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("gitignore changed: bytes=%q before=%v after=%v", got, before.ModTime(), after.ModTime())
		}
	})
}

func TestEnsureLocalConfigGitignoredFIFOChild(t *testing.T) {
	repo := os.Getenv("PIPELINE_GITIGNORE_FIFO_REPO")
	if repo == "" {
		t.Skip("FIFO child only")
	}
	EnsureLocalConfigGitignored(repo)
}

func BenchmarkEnsureLocalConfigGitignoredNoop(b *testing.B) {
	repo := b.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "config.local.yaml"), []byte("{}\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".agents/config.local.yaml\n.agents/lock.local.yaml\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EnsureLocalConfigGitignored(repo)
	}
}
