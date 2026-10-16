package cache

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newLocalStateRepo builds a real worktree repository with one detached
// commit, the shape an exact snapshot carries, then applies mutate to its
// metadata. The repository-local config is always rewritten to mode 0600,
// matching what sanitize produces.
func newLocalStateRepo(t *testing.T, algorithm string, mutate func(t *testing.T, repo, gitDir string)) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	repo := t.TempDir()
	gitTest(t, "", "init", "-q", "-b", "main", "--object-format="+algorithm, repo)
	gitTest(t, repo, "config", "user.email", "test@example.invalid")
	gitTest(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "resource.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "resource.txt")
	gitTest(t, repo, "commit", "-qm", "first")
	gitTest(t, repo, "checkout", "-q", "--detach", "HEAD")
	gitDir := filepath.Join(repo, ".git")
	rewriteLocalConfig(t, gitDir, canonicalConfigForAlgorithm(algorithm))
	if mutate != nil {
		mutate(t, repo, gitDir)
	}
	return repo
}

func canonicalConfigForAlgorithm(algorithm string) string {
	version, format := "0", ""
	if algorithm == "sha256" {
		version, format = "1", "[extensions]\n\tobjectformat = sha256\n"
	}
	return "[core]\n\trepositoryformatversion = " + version + "\n\tfilemode = true\n\tbare = false\n\tlogallrefupdates = true\n" + format
}

func rewriteLocalConfig(t *testing.T, gitDir, contents string) {
	t.Helper()
	path := filepath.Join(gitDir, "config")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

// subprocessLocalState runs the existing subprocess checks a snapshot open
// performs before the tracked-tree comparison and reports what they observe.
type subprocessLocalState struct {
	objectFormat string
	head         string
	topLevel     string
}

func runSubprocessLocalState(ctx context.Context, repo string) (subprocessLocalState, error) {
	var state subprocessLocalState
	if err := validateExactGitConfigWithContext(ctx, repo); err != nil {
		return state, err
	}
	format, err := exactGitOutputWithContext(ctx, repo, "rev-parse", "--show-object-format")
	if err != nil {
		return state, err
	}
	state.objectFormat = format
	head, err := exactGitOutputWithContext(ctx, repo, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return state, err
	}
	state.head = head
	top, err := exactGitOutputWithContext(ctx, repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return state, err
	}
	state.topLevel = top
	return state, nil
}

func TestExactGitLocalStateDifferential(t *testing.T) {
	canonicalSHA1 := canonicalConfigForAlgorithm("sha1")
	fixtures := []struct {
		name      string
		algorithm string
		bare      bool
		mutate    func(t *testing.T, repo, gitDir string)
	}{
		{name: "canonical-sha1", algorithm: "sha1"},
		{name: "canonical-sha256", algorithm: "sha256"},
		{name: "crlf", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, strings.ReplaceAll(canonicalSHA1, "\n", "\r\n"))
		}},
		{name: "blank-line", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, canonicalSHA1+"\n")
		}},
		{name: "comment-line", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, canonicalSHA1+"\t; comment\n")
		}},
		{name: "uppercase-key", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, strings.Replace(canonicalSHA1, "filemode", "FILEMODE", 1))
		}},
		{name: "spaced-section", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, strings.Replace(canonicalSHA1, "[core]", "[ core ]", 1))
		}},
		{name: "quoted-value", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, strings.Replace(canonicalSHA1, "bare = false", "bare = \"false\"", 1))
		}},
		{name: "duplicate-key", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, canonicalSHA1+"\tbare = false\n")
		}},
		{name: "include-path", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, canonicalSHA1+"[include]\n\tpath = /dev/null\n")
		}},
		{name: "core-worktree", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, canonicalSHA1+"\tworktree = /elsewhere\n")
		}},
		{name: "gitdir-file", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			real := filepath.Join(repo, "realgit")
			if err := os.Rename(gitDir, real); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(gitDir, []byte("gitdir: realgit\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "gitdir-symlink", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			real := filepath.Join(repo, "realgit")
			if err := os.Rename(gitDir, real); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("realgit", gitDir); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "commondir", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("..\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symbolic-head", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "short-head", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			head := strings.TrimSpace(gitTest(t, repo, "rev-parse", "HEAD"))
			if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head[:7]+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "no-trailing-newline", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, strings.TrimSuffix(canonicalSHA1, "\n"))
		}},
		{name: "world-readable-config", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			if err := os.Chmod(filepath.Join(gitDir, "config"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "worktree-bare-true", algorithm: "sha1", mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, strings.Replace(canonicalSHA1, "bare = false", "bare = true", 1))
		}},
		{name: "bare-store", algorithm: "sha1", bare: true, mutate: func(t *testing.T, repo, gitDir string) {
		}},
		{name: "bare-store-not-bare", algorithm: "sha1", bare: true, mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, strings.Replace(canonicalConfigForAlgorithm("sha1"), "bare = false", "bare = false", 1))
		}},
		{name: "bare-store-with-origin", algorithm: "sha1", bare: true, mutate: func(t *testing.T, repo, gitDir string) {
			rewriteLocalConfig(t, gitDir, canonicalConfigForAlgorithm("sha1")+"[remote \"origin\"]\n\turl = /somewhere\n")
		}},
	}
	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			var repo string
			if tc.bare {
				repo = newBareLocalStateRepo(t, tc.algorithm, tc.mutate)
			} else {
				repo = newLocalStateRepo(t, tc.algorithm, tc.mutate)
			}
			ctx := context.Background()
			state, ok := readExactGitLocalState(repo, tc.bare)
			if tc.bare {
				subErr := validateExactGitConfigWithContext(ctx, repo)
				format, formatErr := exactGitOutputWithContext(ctx, repo, "rev-parse", "--show-object-format")
				if !ok {
					t.Logf("fast path declined; subprocess validation err=%v format=%q/%v", subErr, format, formatErr)
					return
				}
				if subErr != nil || formatErr != nil {
					t.Fatalf("fast path accepted but the subprocess path rejected: %v / %v", subErr, formatErr)
				}
				if state.objectFormat != format {
					t.Fatalf("object format = %q, subprocess reports %q", state.objectFormat, format)
				}
				return
			}
			subprocess, subErr := runSubprocessLocalState(ctx, repo)
			if !ok {
				t.Logf("fast path declined; subprocess state=%+v err=%v", subprocess, subErr)
				return
			}
			if subErr != nil {
				t.Fatalf("fast path accepted but the subprocess path rejected: %v", subErr)
			}
			if state.objectFormat != subprocess.objectFormat {
				t.Fatalf("object format = %q, subprocess reports %q", state.objectFormat, subprocess.objectFormat)
			}
			if state.head != subprocess.head {
				t.Fatalf("HEAD = %q, subprocess reports %q", state.head, subprocess.head)
			}
			topAbs, topErr := filepath.EvalSymlinks(subprocess.topLevel)
			repoAbs, repoErr := filepath.EvalSymlinks(repo)
			if topErr != nil || repoErr != nil || state.worktreeRoot != topAbs || repoAbs != topAbs {
				t.Fatalf("worktree root = %q, subprocess reports %q", state.worktreeRoot, subprocess.topLevel)
			}
		})
	}
}

func newBareLocalStateRepo(t *testing.T, algorithm string, mutate func(t *testing.T, repo, gitDir string)) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	repo := t.TempDir()
	gitTest(t, "", "init", "-q", "--bare", "--object-format="+algorithm, repo)
	rewriteLocalConfig(t, repo, strings.Replace(canonicalConfigForAlgorithm(algorithm), "bare = false", "bare = true", 1))
	if mutate != nil {
		mutate(t, repo, repo)
	}
	return repo
}

// TestExactGitLocalStateIgnoresGlobalCoreWorktree records the real-Git probe
// for task 3.3: a global core.worktree does not change --show-toplevel for a
// repository that has a .git directory, so the fast path derives the root.
func TestExactGitLocalStateIgnoresGlobalCoreWorktree(t *testing.T) {
	repo := newLocalStateRepo(t, "sha1", nil)
	globalConfig := filepath.Join(t.TempDir(), "config")
	other := t.TempDir()
	if err := os.WriteFile(globalConfig, []byte("[core]\n\tworktree = "+other+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	probe := exec.Command("git", "-C", repo, "rev-parse", "--show-toplevel")
	probe.Env = os.Environ()
	out, err := probe.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse --show-toplevel under a global core.worktree: %v\n%s", err, out)
	}
	top := strings.TrimSpace(string(out))
	repoAbs, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	topAbs, err := filepath.EvalSymlinks(top)
	if err != nil {
		t.Fatal(err)
	}
	if topAbs != repoAbs {
		t.Fatalf("global core.worktree changed --show-toplevel to %q; the fast path may not derive the worktree root", top)
	}
	state, ok := readExactGitLocalState(repo, false)
	if !ok {
		t.Fatal("fast path declined a canonical repository")
	}
	if state.worktreeRoot != repoAbs {
		t.Fatalf("fast-path worktree root = %q, want %q", state.worktreeRoot, repoAbs)
	}
}

func FuzzExactGitLocalState(f *testing.F) {
	canonicalSHA1 := canonicalConfigForAlgorithm("sha1")
	for _, seed := range []string{
		canonicalSHA1,
		canonicalConfigForAlgorithm("sha256"),
		"",
		"\n",
		"[core]\n",
		strings.ReplaceAll(canonicalSHA1, "\n", "\r\n"),
		canonicalSHA1 + "\tbare = false\n",
		canonicalSHA1 + "[include]\n\tpath = /dev/null\n",
		canonicalSHA1 + "\tworktree = /x\n",
		strings.TrimSuffix(canonicalSHA1, "\n"),
		strings.Replace(canonicalSHA1, "[core]", "[CORE]", 1),
		strings.Replace(canonicalSHA1, "filemode", "fileMode", 1),
		"garbage",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, configBytes []byte) {
		if strings.ContainsRune(string(configBytes), '\x00') {
			t.Skip("config files cannot contain NUL in a WriteFile round trip")
		}
		repo := newLocalStateRepo(t, "sha1", func(t *testing.T, repo, gitDir string) {
			if err := os.WriteFile(filepath.Join(gitDir, "config"), configBytes, 0o600); err != nil {
				t.Fatal(err)
			}
		})
		ctx := context.Background()
		state, ok := readExactGitLocalState(repo, false)
		if !ok {
			return
		}
		subprocess, err := runSubprocessLocalState(ctx, repo)
		if err != nil {
			t.Fatalf("fast path accepted %q but the subprocess path rejected: %v", configBytes, err)
		}
		if state.objectFormat != subprocess.objectFormat || state.head != subprocess.head {
			t.Fatalf("fast path state %+v disagrees with subprocess %+v", state, subprocess)
		}
	})
}
