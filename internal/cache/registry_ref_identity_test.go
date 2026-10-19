package cache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type nestedRefRegistry struct {
	t   *testing.T
	url string
}

// newNestedRefRegistry publishes tag foo, whose tree contains skills/evil/,
// and branches foo/skills/evil, foo/other/branch, feature/a and feature/b.
func newNestedRefRegistry(t *testing.T) nestedRefRegistry {
	t.Helper()
	base := t.TempDir()
	work, origin := filepath.Join(base, "work"), filepath.Join(base, "origin.git")
	gitIn := func(args ...string) {
		t.Helper()
		if out, err := execGit("", args); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	commit := func(rel, content, message string) {
		t.Helper()
		writeFile(t, filepath.Join(work, rel), content)
		gitIn("-C", work, "add", ".")
		gitIn("-C", work, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", message)
	}
	gitIn("init", "-q", "--initial-branch=main", work)
	commit("skills/evil/SKILL.md", "from-tag-foo\n", "foo")
	gitIn("-C", work, "tag", "foo")
	for _, branch := range []string{"foo/skills/evil", "foo/other/branch", "feature/a", "feature/b"} {
		gitIn("-C", work, "checkout", "-q", "-b", branch, "foo")
		commit("ROOT.md", branch+"\n", branch)
	}
	gitIn("clone", "-q", "--bare", work, origin)
	return nestedRefRegistry{t: t, url: "file://" + origin}
}

func (r nestedRefRegistry) git(dir string, args ...string) string {
	r.t.Helper()
	out, err := execGit(dir, args)
	if err != nil {
		r.t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// checkoutState captures everything a foreign Git operation could change.
func (r nestedRefRegistry) checkoutState(dir string) string {
	r.t.Helper()
	return strings.Join([]string{
		r.git(dir, "rev-parse", "HEAD"),
		r.git(dir, "for-each-ref", "--format=%(refname) %(objectname)"),
		r.git(dir, "status", "--porcelain", "--untracked-files=all", "--ignored"),
	}, "\n")
}

// spec: mutable-ref-cache/nested-ref-inside-a-cached-checkout-is-refused
func TestCloneOrPullRefusesRefNestedInsideAnotherRefCheckout(t *testing.T) {
	for _, tc := range []struct {
		name, ref string
	}{
		{name: "nested path exists in parent tree", ref: "foo/skills/evil"},
		{name: "nested path absent from parent tree", ref: "foo/other/branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateMutableGit(t)
			registry := newNestedRefRegistry(t)
			parent, err := CloneOrPull(registry.url, "foo")
			if err != nil {
				t.Fatal(err)
			}
			before := registry.checkoutState(parent)

			got, err := CloneOrPull(registry.url, tc.ref)
			if err == nil {
				t.Fatalf("nested ref %q acquired at %q inside the %q checkout", tc.ref, got, parent)
			}
			if !strings.Contains(err.Error(), tc.ref) || !strings.Contains(err.Error(), parent) {
				t.Fatalf("refusal must name the ref and the enclosing checkout: %v", err)
			}
			if after := registry.checkoutState(parent); after != before {
				t.Fatalf("enclosing checkout changed:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if _, err := os.Lstat(filepath.Join(parent, "other")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refused acquisition created a directory inside the enclosing checkout: %v", err)
			}
			if again, err := CloneOrPull(registry.url, "foo"); err != nil || again != parent {
				t.Fatalf("enclosing ref no longer refreshes: %q, %v", again, err)
			}
		})
	}
}

// spec: mutable-ref-cache/ref-cache-directory-without-its-own-repository-is-refused
func TestCloneOrPullRefusesCacheDirectoryWithoutItsOwnRepository(t *testing.T) {
	isolateMutableGit(t)
	registry := newNestedRefRegistry(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// An enclosing repository, such as a dotfiles checkout at HOME, must never
	// receive the registry's refs or have its working tree checked out.
	registry.git("", "init", "-q", "--initial-branch=main", home)
	writeFile(t, filepath.Join(home, ".profile"), "user content\n")
	registry.git(home, "add", ".profile")
	registry.git(home, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "dotfiles")

	nested, err := CloneOrPull(registry.url, "foo/skills/evil")
	if err != nil {
		t.Fatal(err)
	}
	homeBefore := registry.checkoutState(home)
	nestedBefore := registry.checkoutState(nested)
	parentPath := RegistryCachePath(registry.url, "foo")

	got, err := CloneOrPull(registry.url, "foo")
	if err == nil {
		t.Fatalf("ref acquired at %q without its own repository", got)
	}
	if !strings.Contains(err.Error(), `"foo"`) || !strings.Contains(err.Error(), parentPath) {
		t.Fatalf("refusal must name the ref and its cache directory: %v", err)
	}
	if after := registry.checkoutState(home); after != homeBefore {
		t.Fatalf("enclosing HOME repository changed:\nbefore:\n%s\nafter:\n%s", homeBefore, after)
	}
	if content, err := os.ReadFile(filepath.Join(home, ".profile")); err != nil || string(content) != "user content\n" {
		t.Fatalf("enclosing working tree changed: %q, %v", content, err)
	}
	if after := registry.checkoutState(nested); after != nestedBefore {
		t.Fatalf("nested checkout changed:\nbefore:\n%s\nafter:\n%s", nestedBefore, after)
	}
}

// spec: mutable-ref-cache/sibling-slash-refs-remain-independent
func TestCloneOrPullKeepsSiblingSlashRefsIndependent(t *testing.T) {
	isolateMutableGit(t)
	registry := newNestedRefRegistry(t)
	for round := 0; round < 2; round++ {
		for _, ref := range []string{"feature/a", "feature/b", "main"} {
			got, err := CloneOrPull(registry.url, ref)
			if err != nil {
				t.Fatalf("round %d ref %q: %v", round, ref, err)
			}
			if want := RegistryCachePath(registry.url, ref); got != want {
				t.Fatalf("ref %q cache path = %q, want %q", ref, got, want)
			}
			if top := registry.git(got, "rev-parse", "--show-toplevel"); top != got {
				resolvedTop, _ := filepath.EvalSymlinks(top)
				resolvedGot, _ := filepath.EvalSymlinks(got)
				if resolvedTop != resolvedGot {
					t.Fatalf("ref %q resolved inside %q", ref, top)
				}
			}
			if ref != "main" {
				assertFileContents(t, filepath.Join(got, "ROOT.md"), ref+"\n")
			}
		}
	}
}
