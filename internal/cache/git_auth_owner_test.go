package cache

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// actAsAnotherUser makes files created by the test belong to "someone else"
// from the reader's point of view, since tests cannot chown to another account.
func actAsAnotherUser(t *testing.T) {
	t.Helper()
	original := gitAuthEffectiveUID
	other := os.Geteuid() + 1
	gitAuthEffectiveUID = func() int { return other }
	t.Cleanup(func() { gitAuthEffectiveUID = original })
}

// ownAsFixtureAccount gives path to the account actAsAnotherUser stands in
// for. Unprivileged runs already own their files; root is always trusted, so a
// root run hands the file to an unprivileged account.
func ownAsFixtureAccount(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		if err := os.Chown(path, 65534, 65534); err != nil {
			t.Fatal(err)
		}
	}
}

// installGitOnPath puts a git entry first on PATH that resolves, through a
// Homebrew-style symlink, to target.
func installGitOnPath(t *testing.T, target string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// installRelativeGitOnPath resolves git through a relative PATH entry, with
// Go's ErrDot protection disabled so the lookup itself returns a relative path.
func installRelativeGitOnPath(t *testing.T, target string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "relbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "relbin", "git")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("PATH", "relbin"+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GODEBUG", "execerrdot=0")
}

func gitWrapper(t *testing.T, realGit string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Cellar", "git", "bin", "git")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec \"$AUTH_REAL_GIT\" \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	ownAsFixtureAccount(t, path)
	t.Setenv("AUTH_REAL_GIT", realGit)
	return path
}

func rootOwnedGit(t *testing.T, candidates ...string) string {
	t.Helper()
	for _, candidate := range candidates {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil {
			continue
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid == 0 && info.Mode().Perm()&0o022 == 0 {
			return resolved
		}
	}
	t.Skip("no root-owned git executable available for the unrelated-owner control")
	return ""
}

// spec: git-authentication/configuration-owned-by-the-git-executable-owner-is-trusted
// spec: git-authentication/executable-owner-trust-requires-a-protected-executable
func TestGitAuthTrustsConfigurationOwnedByGitExecutableOwner(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if realGit, err = filepath.EvalSymlinks(realGit); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		git      func(t *testing.T) string
		relative bool
		trusted  bool
	}{
		{name: "executable owner", git: func(t *testing.T) string { return gitWrapper(t, realGit, 0o755) }, trusted: true},
		{name: "group-writable executable", git: func(t *testing.T) string { return gitWrapper(t, realGit, 0o775) }},
		{name: "other-writable executable", git: func(t *testing.T) string { return gitWrapper(t, realGit, 0o757) }},
		{name: "unrelated executable owner", git: func(t *testing.T) string { return rootOwnedGit(t, "/usr/bin/git", realGit) }},
		{name: "relative PATH entry", git: func(t *testing.T) string { return gitWrapper(t, realGit, 0o755) }, relative: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedGitAuth(t)
			f := newGitAuthServer(t)
			complete := `!f() { test "$1" != get || printf 'username=fixture-user\npassword=synthetic-auth-secret\n'; }; f`
			system := authFile(t, "system", fmt.Sprintf("[credential]\n\thelper = %q\n[http]\n\tsslCAInfo = %q\n", complete, f.ca), 0o644)
			ownAsFixtureAccount(t, system)
			t.Setenv("GIT_CONFIG_SYSTEM", system)
			if tc.relative {
				installRelativeGitOnPath(t, tc.git(t))
			} else {
				installGitOnPath(t, tc.git(t))
			}
			actAsAnotherUser(t)

			_, err := ResolveExactGitRef("https://example.invalid/owner-fixture", f.url, "main")
			if tc.trusted {
				if err != nil || f.accepted.Load() == 0 {
					t.Fatalf("configuration owned by the git executable owner was not trusted: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrGitAuthConfigUntrusted) {
				t.Fatalf("error = %v, want %v", err, ErrGitAuthConfigUntrusted)
			}
			if f.accepted.Load() != 0 || f.challenged.Load() != 0 {
				t.Fatal("untrusted configuration reached the registry")
			}
		})
	}
}
