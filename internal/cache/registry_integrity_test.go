package cache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCloneOrPullDoesNotDiscoverRepositoryAboveTheCache is the upward-discovery
// exploit.
//
// Git's answer to "the current directory is not a repository" is to walk towards
// the filesystem root looking for one. A cache entry that exists but holds no
// repository — an interrupted clone, a half-finished `rm -rf`, a directory
// somebody created by hand — therefore used to make every git command in that
// entry operate on whichever repository *contains* the cache. The cache lives
// under the home directory, and `CacheDir` falls back to a path under the working
// directory when there is no home directory, so the containing repository is
// routinely the user's own checkout.
//
// The consequence is not only that the wrong repository is written to: `HEAD` of
// the outer repository resolves the pinned ref, and the entry then looks
// up-to-date, so an empty directory is published as a registry's content.
func TestCloneOrPullDoesNotDiscoverRepositoryAboveTheCache(t *testing.T) {
	outer, outerHead := createOuterRepository(t)
	t.Setenv("HOME", filepath.Join(outer, "home"))

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "registry content\n", "registry commit")

	cachePath := RegistryCachePath(origin, "main")
	// A cache entry that is a directory but not a repository.
	writeFile(t, filepath.Join(cachePath, "leftover.txt"), "half a clone\n")

	got, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("expected a corrupt entry to be rebuilt, got %v", err)
	}
	if got != cachePath {
		t.Fatalf("expected cache path %q, got %q", cachePath, got)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "registry content\n")
	if _, statErr := os.Stat(filepath.Join(cachePath, "leftover.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the rebuilt entry to replace the corrupt one, stat error: %v", statErr)
	}
	assertOuterRepositoryUntouched(t, outer, outerHead)
	assertNoScratchEntries(t, filepath.Dir(cachePath))
}

// TestCloneOrPullTreatsSymlinkedGitDirectoryAsCorrupt is the same escape by a
// shorter route: rather than relying on discovery, `.git` inside the entry points
// at a repository outside the cache. Git then takes its objects and refs from
// there while treating the cache entry as that repository's working tree, so
// `fetch` writes the outer repository's refs and `checkout --force` moves its HEAD.
func TestCloneOrPullTreatsSymlinkedGitDirectoryAsCorrupt(t *testing.T) {
	outer, outerHead := createOuterRepository(t)
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "registry content\n", "registry commit")

	cachePath := RegistryCachePath(origin, "main")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatalf("create cache entry: %v", err)
	}
	if err := os.Symlink(filepath.Join(outer, ".git"), filepath.Join(cachePath, ".git")); err != nil {
		t.Fatalf("plant symlinked git directory: %v", err)
	}

	if _, err := CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("expected an entry with a symlinked git directory to be rebuilt, got %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "registry content\n")
	assertOuterRepositoryUntouched(t, outer, outerHead)
}

// TestCloneOrPullRebuildsEntryWithoutRepository covers the shapes a corrupt entry
// can take when there is no outer repository to escape into: the entry must be
// rebuilt atomically rather than handed to git, and nothing may be left behind.
func TestCloneOrPullRebuildsEntryWithoutRepository(t *testing.T) {
	cases := map[string]func(t *testing.T, cachePath string){
		"empty directory": func(t *testing.T, cachePath string) {
			if err := os.MkdirAll(cachePath, 0o755); err != nil {
				t.Fatalf("create cache entry: %v", err)
			}
		},
		"files but no git directory": func(t *testing.T, cachePath string) {
			writeFile(t, filepath.Join(cachePath, "skill.txt"), "not from the registry\n")
		},
		"empty git directory": func(t *testing.T, cachePath string) {
			if err := os.MkdirAll(filepath.Join(cachePath, ".git"), 0o755); err != nil {
				t.Fatalf("create git directory: %v", err)
			}
		},
		"git directory without objects": func(t *testing.T, cachePath string) {
			writeFile(t, filepath.Join(cachePath, ".git", "HEAD"), "ref: refs/heads/main\n")
		},
		"git file instead of a git directory": func(t *testing.T, cachePath string) {
			writeFile(t, filepath.Join(cachePath, ".git"), "gitdir: /somewhere/else/.git\n")
		},
		"plain file at the entry name": func(t *testing.T, cachePath string) {
			writeFile(t, cachePath, "not a directory\n")
		},
	}

	for name, plant := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			origin, working := createBareRepoWithWorkingClone(t)
			writeAndPushCommit(t, working, "skill.txt", "registry content\n", "registry commit")

			cachePath := RegistryCachePath(origin, "main")
			plant(t, cachePath)

			if _, err := CloneOrPull(origin, "main"); err != nil {
				t.Fatalf("expected a corrupt entry to be rebuilt, got %v", err)
			}
			assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "registry content\n")
			assertNoScratchEntries(t, filepath.Dir(cachePath))
		})
	}
}

// TestCloneOrPullRefusesEntrySymlinkedInsideTheCache closes the gap the *os.Root
// anchoring leaves open. A symlink pointing *out* of the cache is refused by the
// root itself, but one pointing at a sibling entry resolves fine — and it makes two
// refs share one checkout, so a ref the caller did not ask for decides the content
// it gets. Nothing this package does creates such a link, so it is refused rather
// than repaired.
func TestCloneOrPullRefusesEntrySymlinkedInsideTheCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "wanted\n", "registry commit")

	other, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("seed a sibling entry: %v", err)
	}
	writeFile(t, filepath.Join(other, "planted.txt"), "sibling content\n")

	cachePath := RegistryCachePath(origin, "v1")
	if err := os.Symlink(filepath.Base(other), cachePath); err != nil {
		t.Fatalf("plant symlinked entry: %v", err)
	}

	if _, err := CloneOrPull(origin, "v1"); err == nil {
		t.Fatal("expected an entry that is a symlink inside the cache to be refused")
	} else if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected the refusal to name the cause, got %v", err)
	}
	// The sibling the link pointed at must be untouched: refusing is only
	// worthwhile if it happens before anything is written through the link.
	assertFileContents(t, filepath.Join(other, "planted.txt"), "sibling content\n")
	assertFileContents(t, filepath.Join(other, "skill.txt"), "wanted\n")
}

// TestCloneOrPullPreservesRemoteTrackingRefsAcrossRebuild is the lost-refs
// regression.
//
// A rebuild seeds itself with `git clone --local` from the entry it replaces, and
// clone maps the *local* branches of what it clones into refs/remotes/origin/*. A
// cache entry has a detached HEAD and one leftover local branch, so the rebuilt
// entry came out with almost none of the remote-tracking refs the old one had.
// Those refs are what a pinned branch resolves against, so the loss surfaces on the
// next offline refresh — the ref no longer resolves as a branch at all.
func TestCloneOrPullPreservesRemoteTrackingRefsAcrossRebuild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	pushBranch(t, working, "release", "skill.txt", "release one\n")

	cachePath, err := CloneOrPull(origin, "release")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	pushBranch(t, working, "release", "skill.txt", "release two\n")
	if _, err := CloneOrPull(origin, "release"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "release two\n")

	tracking := strings.TrimSpace(runGitCommand(t, cachePath, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/release^{commit}"))
	head := strings.TrimSpace(runGitCommand(t, cachePath, "rev-parse", "HEAD"))
	if tracking != head {
		t.Fatalf("rebuilt entry lost its remote-tracking ref: refs/remotes/origin/release is %q, HEAD is %q", tracking, head)
	}

	// Offline, the rebuilt entry must still serve the commit it was published
	// from, which is only possible if the refresh can still resolve the ref.
	if err := os.Rename(origin, origin+".offline"); err != nil {
		t.Fatalf("take the registry offline: %v", err)
	}
	if _, err := CloneOrPull(origin, "release"); err != nil {
		t.Fatalf("expected an offline refresh of a rebuilt entry to succeed: %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "release two\n")
	if offlineHead := strings.TrimSpace(runGitCommand(t, cachePath, "rev-parse", "HEAD")); offlineHead != head {
		t.Fatalf("offline refresh moved the entry from %q to %q", head, offlineHead)
	}
}

// TestCloneOrPullRefusesSameNamedTagWhenPinnedBranchDisappears is the
// substitution the pin record exists to prevent.
//
// Resolution tries a branch before a tag, so while the branch exists a same-named
// tag is harmless. The moment the branch is deleted upstream the tag becomes the
// next candidate — and pushing a tag is a weaker permission than pushing a branch
// in every forge — so a registry's content would change to whatever the tag points
// at, with nothing in any configuration having changed. The entry records the kind
// of ref it was published from, so this fails instead.
func TestCloneOrPullRefusesSameNamedTagWhenPinnedBranchDisappears(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	pushBranch(t, working, "release", "skill.txt", "trusted\n")

	cachePath, err := CloneOrPull(origin, "release")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// A tag with the branch's name, pointing at content the branch never had.
	runGitCommand(t, working, "checkout", "--detach")
	writeFile(t, filepath.Join(working, "skill.txt"), "substituted\n")
	runGitCommand(t, working, "add", "skill.txt")
	runGitCommand(t, working, "commit", "-m", "content behind the tag")
	runGitCommand(t, working, "tag", "release")
	runGitCommand(t, working, "push", "origin", "refs/tags/release")
	runGitCommand(t, working, "push", "origin", "--delete", "refs/heads/release")

	_, err = CloneOrPull(origin, "release")
	if err == nil {
		t.Fatal("expected a deleted branch to fail rather than resolve to a same-named tag")
	}
	if !strings.Contains(err.Error(), "refusing to substitute") {
		t.Fatalf("expected the refusal to name the cause, got %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "trusted\n")
}

// TestCloneOrPullRefusesAmbiguousBranchAndTagRef covers the first resolution of a
// name that is both, where there is no pin to say which was meant. Git resolves it
// with a precedence rule and a warning; here the content served would depend on
// that rule, so the ref has to be qualified instead.
func TestCloneOrPullRefusesAmbiguousBranchAndTagRef(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	pushBranch(t, working, "shared", "skill.txt", "from the branch\n")
	runGitCommand(t, working, "checkout", "--detach")
	writeFile(t, filepath.Join(working, "skill.txt"), "from the tag\n")
	runGitCommand(t, working, "add", "skill.txt")
	runGitCommand(t, working, "commit", "-m", "tagged commit")
	runGitCommand(t, working, "tag", "shared")
	runGitCommand(t, working, "push", "origin", "refs/tags/shared")

	_, err := CloneOrPull(origin, "shared")
	if err == nil {
		t.Fatal("expected a ref that is both a branch and a tag to be refused")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected an ambiguity error, got %v", err)
	}

	branchPath, err := CloneOrPull(origin, "refs/heads/shared")
	if err != nil {
		t.Fatalf("expected refs/heads/… to disambiguate: %v", err)
	}
	assertFileContents(t, filepath.Join(branchPath, "skill.txt"), "from the branch\n")

	tagPath, err := CloneOrPull(origin, "refs/tags/shared")
	if err != nil {
		t.Fatalf("expected refs/tags/… to disambiguate: %v", err)
	}
	assertFileContents(t, filepath.Join(tagPath, "skill.txt"), "from the tag\n")
}

// TestResolveCommitRefusesRefThatOnlyNamesALocalBranch covers the other leftover a
// clone creates. Every cache entry keeps a refs/heads/<default> from its own clone,
// so once the upstream branch is gone — deleted, renamed, pruned — a bare name
// still resolves *locally*, and serving it would answer a request for a branch the
// registry no longer has with a copy the cache happens to be holding.
func TestResolveCommitRefusesRefThatOnlyNamesALocalBranch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "content\n", "registry commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	if got := strings.TrimSpace(runGitCommand(t, cachePath, "rev-parse", "--verify", "--quiet", "refs/heads/main^{commit}")); got == "" {
		t.Skip("this clone left no local branch behind, so there is nothing to mis-resolve")
	}

	store, err := openCacheStore(CacheDir(), HashURL(origin))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer store.close()
	entry, err := store.openEntry(cacheRefComponent("main"))
	if err != nil {
		t.Fatalf("open cache entry: %v", err)
	}
	defer func() { _ = entry.Close() }()

	// What `fetch --prune` does once the branch is gone upstream.
	if err := runGitInRepo(entry, "update-ref", "-d", "refs/remotes/origin/main"); err != nil {
		t.Fatalf("drop the remote-tracking ref: %v", err)
	}

	if _, _, err := resolveCommit(entry, "main", nil); err == nil {
		t.Fatal("expected a ref that only names a local branch to be refused")
	} else if !strings.Contains(err.Error(), "only names the local ref") {
		t.Fatalf("expected the refusal to name the cause, got %v", err)
	}
}

// TestCloneOrPullResolvesAbbreviatedCommit is the other side of the guard above:
// an abbreviated commit is not a ref *and* not a full object name, which is
// exactly the shape the guard has to let through. It is checked across a refresh
// too, because that is where the recorded pin narrows resolution to one kind.
func TestCloneOrPullResolvesAbbreviatedCommit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "pinned\n", "pinned commit")
	commit := strings.TrimSpace(runGitCommand(t, working, "rev-parse", "HEAD"))
	writeAndPushCommit(t, working, "skill.txt", "later\n", "later commit")

	abbreviated := commit[:10]
	cachePath, err := CloneOrPull(origin, abbreviated)
	if err != nil {
		t.Fatalf("expected an abbreviated commit to resolve: %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "pinned\n")

	if _, err := CloneOrPull(origin, abbreviated); err != nil {
		t.Fatalf("expected a refresh of an abbreviated commit pin to succeed: %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "pinned\n")
}

// TestCloneOrPullRebuildsEntryWithMalformedPin covers the pin record being
// unreadable. It is this package's own bookkeeping, so garbage in it means the
// entry cannot be trusted to hold what its name says, and the entry is replaced.
func TestCloneOrPullRebuildsEntryWithMalformedPin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "content\n", "registry commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	before, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat published entry: %v", err)
	}
	writeFile(t, filepath.Join(cachePath, ".git", pinFileName), "class=branch\ncommit=not-an-object-name\n")

	if _, err := CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("expected an entry with a malformed pin to be rebuilt: %v", err)
	}
	after, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat rebuilt entry: %v", err)
	}
	if os.SameFile(before, after) {
		t.Fatal("expected a malformed pin to force a rebuilt entry")
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "content\n")
	assertNoScratchEntries(t, filepath.Dir(cachePath))
}

// TestCloneOrPullRecordsPinInsideGitDirectory states where the pin lives, because
// the location is load-bearing: a file in the worktree would differ from HEAD and
// make every entry look locally modified, forcing a rebuild on every refresh.
func TestCloneOrPullRecordsPinInsideGitDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "content\n", "registry commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	record, err := os.ReadFile(filepath.Join(cachePath, ".git", pinFileName))
	if err != nil {
		t.Fatalf("read pin record: %v", err)
	}
	if !strings.Contains(string(record), "class="+string(refClassBranch)) {
		t.Fatalf("expected the pin to record the branch class, got %q", record)
	}
	if status := runGitCommand(t, cachePath, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("expected the pin to leave the worktree clean, got %q", status)
	}
}

// TestCloneOrPullIgnoresAmbientGitRepositoryEnvironment is the inherited-environment
// exploit. Every one of these variables re-points git at a repository, index or
// object store of somebody else's choosing, and an agent-marketplace run inherits
// its environment from a shell, a CI job or a git hook — git sets GIT_DIR and
// GIT_INDEX_FILE for every hook it runs, so a `sync` from a hook carries them.
func TestCloneOrPullIgnoresAmbientGitRepositoryEnvironment(t *testing.T) {
	outer, outerHead := createOuterRepository(t)
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "registry content\n", "registry commit")

	// Set after the fixtures exist: these variables would otherwise redirect the
	// fixture repositories' own git commands.
	outerGitDir := filepath.Join(outer, ".git")
	for name, value := range map[string]string{
		"GIT_DIR":                          outerGitDir,
		"GIT_WORK_TREE":                    outer,
		"GIT_COMMON_DIR":                   outerGitDir,
		"GIT_INDEX_FILE":                   filepath.Join(outerGitDir, "index"),
		"GIT_OBJECT_DIRECTORY":             filepath.Join(outerGitDir, "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": filepath.Join(outerGitDir, "objects"),
		"GIT_NAMESPACE":                    "hijacked",
		"GIT_CEILING_DIRECTORIES":          "/",
		"GIT_CONFIG_COUNT":                 "1",
		"GIT_CONFIG_KEY_0":                 "core.worktree",
		"GIT_CONFIG_VALUE_0":               outer,
	} {
		t.Setenv(name, value)
	}

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("expected an ambient git environment to be ignored, got %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "registry content\n")
	assertOuterRepositoryUntouched(t, outer, outerHead)

	// A refresh takes a different path through the package (fetch, resolve,
	// rebuild out of place), so it is checked separately.
	writeAndPushCommit(t, working, "skill.txt", "second version\n", "second commit")
	if _, err := CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("expected a refresh to ignore the ambient git environment, got %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "second version\n")
	assertOuterRepositoryUntouched(t, outer, outerHead)
}

func TestGitEnvironDropsRepositoryScopingVariables(t *testing.T) {
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open anchor directory: %v", err)
	}
	defer func() { _ = dir.Close() }()

	dropped := []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_INDEX_VERSION",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
		"GIT_GRAFT_FILE", "GIT_SHALLOW_FILE", "GIT_PREFIX", "GIT_REPLACE_REF_BASE",
		"GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT",
		"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0",
	}
	for _, name := range dropped {
		t.Setenv(name, "planted")
	}
	// Transport and authentication settings are how a private registry is
	// reached, and they cannot change which repository is written to.
	kept := map[string]string{
		"GIT_SSH_COMMAND":     "ssh -i /dev/null",
		"GIT_ASKPASS":         "/usr/bin/true",
		"GIT_TERMINAL_PROMPT": "0",
	}
	for name, value := range kept {
		t.Setenv(name, value)
	}

	environ := gitEnviron(dir, "SENTINEL=extra")
	seen := make(map[string]string, len(environ))
	for _, entry := range environ {
		name, value, _ := strings.Cut(entry, "=")
		seen[name] = value
	}

	for _, name := range dropped {
		if value, found := seen[name]; found {
			t.Fatalf("expected %s to be dropped, got %q", name, value)
		}
	}
	for name, value := range kept {
		if seen[name] != value {
			t.Fatalf("expected %s to be kept as %q, got %q", name, value, seen[name])
		}
	}
	if seen["SENTINEL"] != "extra" {
		t.Fatalf("expected explicitly added entries to survive, got %q", seen["SENTINEL"])
	}
	if want := filepath.Clean(dir.Name()); seen["GIT_CEILING_DIRECTORIES"] != want {
		t.Fatalf("expected the ceiling to be %q, got %q", want, seen["GIT_CEILING_DIRECTORIES"])
	}
	if seen["GIT_DISCOVERY_ACROSS_FILESYSTEM"] != "false" {
		t.Fatalf("expected discovery across filesystems to be disabled, got %q", seen["GIT_DISCOVERY_ACROSS_FILESYSTEM"])
	}
}

// createOuterRepository builds the repository an escape would land in, and returns
// its path and the commit its HEAD is on.
func createOuterRepository(t *testing.T) (string, string) {
	t.Helper()

	outer := t.TempDir()
	runGitCommand(t, "", "init", "--initial-branch=main", outer)
	writeFile(t, filepath.Join(outer, "outer.txt"), "belongs to the user\n")
	runGitCommand(t, outer, "add", "outer.txt")
	runGitCommand(t, outer, "commit", "-m", "outer commit")

	return outer, strings.TrimSpace(runGitCommand(t, outer, "rev-parse", "HEAD"))
}

// assertOuterRepositoryUntouched checks the things an escape would disturb: the
// checked-out content, which commit HEAD is on, whether HEAD is still on a branch
// (a stray `checkout --detach` moves it off), and whether anything was fetched in.
func assertOuterRepositoryUntouched(t *testing.T, outer, head string) {
	t.Helper()

	assertFileContents(t, filepath.Join(outer, "outer.txt"), "belongs to the user\n")
	if got := strings.TrimSpace(runGitCommand(t, outer, "rev-parse", "HEAD")); got != head {
		t.Fatalf("outer repository HEAD moved from %q to %q", head, got)
	}
	if got := strings.TrimSpace(runGitCommand(t, outer, "symbolic-ref", "--quiet", "HEAD")); got != "refs/heads/main" {
		t.Fatalf("outer repository HEAD is no longer on its branch: %q", got)
	}
	if got := strings.TrimSpace(runGitCommand(t, outer, "for-each-ref", "--format=%(refname)", "refs/remotes", "refs/tags")); got != "" {
		t.Fatalf("outer repository gained refs: %q", got)
	}
	if got := strings.TrimSpace(runGitCommand(t, outer, "diff", "--name-only", "HEAD")); got != "" {
		t.Fatalf("outer repository worktree was modified: %q", got)
	}
}

// pushBranch commits content on branch in the working clone and pushes it.
func pushBranch(t *testing.T, working, branch, relPath, content string) {
	t.Helper()

	if _, err := tryGitCommand(working, "checkout", branch); err != nil {
		runGitCommand(t, working, "checkout", "-b", branch)
	}
	writeFile(t, filepath.Join(working, relPath), content)
	runGitCommand(t, working, "add", relPath)
	runGitCommand(t, working, "commit", "-m", "commit on "+branch)
	runGitCommand(t, working, "push", "origin", "refs/heads/"+branch+":refs/heads/"+branch)
}
