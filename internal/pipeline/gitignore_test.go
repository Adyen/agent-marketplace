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

	"golang.org/x/sys/unix"
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

// isolateGitIgnoreEnv keeps the developer's global and system excludes out of
// git check-ignore so pattern-coverage assertions are deterministic.
func isolateGitIgnoreEnv(tb testing.TB) {
	tb.Helper()
	home := tb.TempDir()
	tb.Setenv("HOME", home)
	tb.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	tb.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tb.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
}

func initGitignoreRepo(tb testing.TB) string {
	tb.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		tb.Skip("git not available")
	}
	repo := tb.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		tb.Fatalf("git init: %v\n%s", err, out)
	}
	return repo
}

func TestEnsureLocalConfigGitignoredRespectsExistingPatterns(t *testing.T) {
	isolateGitIgnoreEnv(t)
	for _, tc := range []struct {
		name    string
		initial string
		want    string
	}{
		{
			name:    "recursive agents directory with negations",
			initial: "**/config.local.yaml\n.agents/config.local.yaml\n**/.agents/**\n!**/.agents/rules/\n!**/.agents/rules/**\n!**/.agents/config.yaml\n!**/.agents/lock.yaml\n",
		},
		{
			name:    "local wildcard",
			initial: ".agents/*.local.*\n",
		},
		{
			name:    "pattern covers only config",
			initial: "**/config.local.yaml\n",
			want:    "**/config.local.yaml\n.agents/lock.local.yaml\n",
		},
		{
			name:    "negation re-includes lock",
			initial: ".agents/*\n!.agents/lock.local.yaml\n",
			want:    ".agents/*\n!.agents/lock.local.yaml\n.agents/lock.local.yaml\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initGitignoreRepo(t)
			writeGitignoreLocalConfig(t, repo)
			gitignore := filepath.Join(repo, ".gitignore")
			if err := os.WriteFile(gitignore, []byte(tc.initial), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(gitignore)
			if err != nil {
				t.Fatal(err)
			}
			EnsureLocalConfigGitignored(repo)
			got, err := os.ReadFile(gitignore)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want
			if want == "" {
				want = tc.initial
				after, err := os.Stat(gitignore)
				if err != nil {
					t.Fatal(err)
				}
				if !before.ModTime().Equal(after.ModTime()) {
					t.Fatalf("covered gitignore mtime changed: before=%v after=%v", before.ModTime(), after.ModTime())
				}
			}
			if string(got) != want {
				t.Fatalf("gitignore = %q, want %q", got, want)
			}
		})
	}
}

func TestEnsureLocalConfigGitignoredFIFOChild(t *testing.T) {
	repo := os.Getenv("PIPELINE_GITIGNORE_FIFO_REPO")
	if repo == "" {
		t.Skip("FIFO child only")
	}
	EnsureLocalConfigGitignored(repo)
}

func TestAppendLocalConfigIgnoreEntriesRestoresBlockingDescriptor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	flags := func() int {
		var got int
		var controlErr error
		conn, err := f.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Control(func(fd uintptr) { got, controlErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
			t.Fatal(err)
		}
		if controlErr != nil {
			t.Fatal(controlErr)
		}
		return got
	}
	before := flags()
	if before&unix.O_NONBLOCK == 0 || before&unix.O_APPEND == 0 {
		t.Fatalf("initial flags = %#x", before)
	}
	if err := appendLocalConfigIgnoreEntries(f, nil); err != nil {
		t.Fatal(err)
	}
	after := flags()
	if after&unix.O_NONBLOCK != 0 || after&unix.O_APPEND == 0 {
		t.Fatalf("restored flags = %#x", after)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != ".agents/config.local.yaml\n.agents/lock.local.yaml\n" {
		t.Fatalf("gitignore = %q err=%v", data, err)
	}
}

func TestAppendLocalConfigIgnoreEntriesRejectsFIFOBeforeClearingNonblock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := appendLocalConfigIgnoreEntries(f, nil); !errors.Is(err, errUnsafeGitignore) {
		t.Fatalf("error = %v, want errUnsafeGitignore", err)
	}
	var flags int
	conn, err := f.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Control(func(fd uintptr) { flags, err = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.O_NONBLOCK == 0 {
		t.Fatalf("FIFO nonblock flag cleared: %#x", flags)
	}
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

func BenchmarkEnsureLocalConfigGitignoredPatternCovered(b *testing.B) {
	isolateGitIgnoreEnv(b)
	repo := initGitignoreRepo(b)
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "config.local.yaml"), []byte("{}\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("**/.agents/**\n!**/.agents/config.yaml\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EnsureLocalConfigGitignored(repo)
	}
}
