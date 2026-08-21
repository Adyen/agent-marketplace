package cache

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestGitRunsAnchoredToDescriptorNotPath is the fd-to-path TOCTOU exploit.
//
// The cache validates every component it opens, but os/exec can only express a
// child's working directory as a string, so a path-based implementation
// re-resolves that string after validation. This test performs the swap that
// window allows — the validated directory is moved aside and a symlink to a
// location outside the cache is planted at its name — and requires that git still
// operates on the inode that was validated.
func TestGitRunsAnchoredToDescriptorNotPath(t *testing.T) {
	if !gitExecutionIsAnchored {
		t.Skip("git execution is only descriptor-anchored on unix")
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	url := "https://example.com/org/registry.git"
	store, err := openCacheStore(CacheDir(), HashURL(url))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer store.close()

	scratch, err := store.makeTemp(cacheRefComponent("main"), tempEntryInfix)
	if err != nil {
		t.Fatalf("create scratch entry: %v", err)
	}
	anchored, err := store.openEntry(scratch)
	if err != nil {
		t.Fatalf("open scratch entry: %v", err)
	}
	defer func() { _ = anchored.Close() }()

	hashPath := filepath.Join(CacheDir(), HashURL(url))
	moved := filepath.Join(hashPath, "moved-aside")
	outside := t.TempDir()
	if err := os.Rename(filepath.Join(hashPath, scratch), moved); err != nil {
		t.Fatalf("move validated directory aside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(hashPath, scratch)); err != nil {
		t.Fatalf("plant symlink at the validated name: %v", err)
	}

	// Self-check: the swap must actually be able to redirect a path-based
	// implementation, otherwise the test proves nothing.
	redirected, err := filepath.EvalSymlinks(anchored.Name())
	if err != nil {
		t.Fatalf("resolve the descriptor's recorded path: %v", err)
	}
	if resolvedOutside, evalErr := filepath.EvalSymlinks(outside); evalErr != nil {
		t.Fatalf("resolve the outside directory: %v", evalErr)
	} else if redirected != resolvedOutside {
		t.Fatalf("test setup does not redirect the recorded path: %q resolves to %q, want %q", anchored.Name(), redirected, resolvedOutside)
	}

	if err := runGitIn(anchored, "init", "--quiet"); err != nil {
		t.Fatalf("run git anchored to the validated descriptor: %v", err)
	}

	if _, err := os.Stat(filepath.Join(moved, ".git")); err != nil {
		t.Fatalf("expected git to write to the validated directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("git was redirected outside the cache, stat error: %v", err)
	}
}

// TestCloneOrPullRefusesSymlinkedRelativeCacheParent covers the one place where
// following a symlinked cache parent would not be a user's own choice: with no
// home directory to derive the cache root from, the cache lands under the working
// directory, which may be a checkout somebody else wrote.
func TestCloneOrPullRefusesSymlinkedRelativeCacheParent(t *testing.T) {
	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "content\n", "initial commit")

	workDir := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "sentinel.txt"), "untouched\n")
	if err := os.Symlink(outside, filepath.Join(workDir, ".cache")); err != nil {
		t.Fatalf("plant symlinked cache parent: %v", err)
	}

	t.Setenv("HOME", "")
	t.Chdir(workDir)

	if _, err := CloneOrPull(origin, "main"); err == nil {
		t.Fatal("expected a symlinked relative cache parent to be refused")
	} else if !strings.Contains(err.Error(), "refusing symlinked cache parent") {
		t.Fatalf("expected the refusal to name the cause, got %v", err)
	}
	assertFileContents(t, filepath.Join(outside, "sentinel.txt"), "untouched\n")
	if _, statErr := os.Stat(filepath.Join(outside, "agent-marketplace")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the symlinked cache parent was used, stat error: %v", statErr)
	}
}

// TestCloneOrPullRejectsRefThatOnlyNamesATrackedFile is the pathspec-fallback
// exploit. `git checkout <name>` silently reinterprets its argument as a pathspec
// when no such ref exists, so pinning a registry to a ref that does not exist but
// that happens to match a tracked path used to "succeed" and serve the default
// branch's content under that ref's cache entry.
func TestCloneOrPullRejectsRefThatOnlyNamesATrackedFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "release", "not a ref\n", "add a file named like a ref")

	cachePath, err := CloneOrPull(origin, "release")
	if err == nil {
		t.Fatalf("expected a ref that only names a tracked file to be rejected, got cache path %q", cachePath)
	}
	if !strings.Contains(err.Error(), "does not resolve to a commit") {
		t.Fatalf("expected a resolution error, got %v", err)
	}
	if _, statErr := os.Stat(RegistryCachePath(origin, "release")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("an unresolvable ref published a cache entry, stat error: %v", statErr)
	}
}

// TestCloneOrPullResolvesTagAndCommitRefs covers the resolution order that
// replaces the pathspec-prone checkout: remote-tracking ref, tag, then the ref as
// written (which is how a pinned commit resolves).
func TestCloneOrPullResolvesTagAndCommitRefs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "tagged\n", "tagged commit")
	runGitCommand(t, working, "tag", "v1.2.3")
	runGitCommand(t, working, "push", "origin", "v1.2.3")
	commit := strings.TrimSpace(runGitCommand(t, working, "rev-parse", "HEAD"))
	writeAndPushCommit(t, working, "skill.txt", "later\n", "later commit")

	tagPath, err := CloneOrPull(origin, "v1.2.3")
	if err != nil {
		t.Fatalf("expected tag ref to resolve: %v", err)
	}
	assertFileContents(t, filepath.Join(tagPath, "skill.txt"), "tagged\n")

	commitPath, err := CloneOrPull(origin, commit)
	if err != nil {
		t.Fatalf("expected commit ref to resolve: %v", err)
	}
	assertFileContents(t, filepath.Join(commitPath, "skill.txt"), "tagged\n")

	if tagPath == commitPath {
		t.Fatalf("tag and commit refs must occupy distinct cache entries, both %q", tagPath)
	}
}

// TestCloneOrPullChecksOutDetachedHead documents that an entry is pinned to a
// resolved object name rather than to a branch, so nothing in the entry can make
// a later checkout resolve somewhere else.
func TestCloneOrPullChecksOutDetachedHead(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "pinned\n", "initial commit")
	commit := strings.TrimSpace(runGitCommand(t, working, "rev-parse", "HEAD"))

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if head := strings.TrimSpace(runGitCommand(t, cachePath, "rev-parse", "HEAD")); head != commit {
		t.Fatalf("expected HEAD at %s, got %s", commit, head)
	}
	if branch := strings.TrimSpace(runGitCommand(t, cachePath, "rev-parse", "--abbrev-ref", "HEAD")); branch != "HEAD" {
		t.Fatalf("expected a detached HEAD, got branch %q", branch)
	}
}

// TestCloneOrPullReplacesEntryOutOfPlaceOnUpdate is the torn-read regression: an
// update must publish a new directory and swap it in, never rewrite the tree a
// reader may be walking.
func TestCloneOrPullReplacesEntryOutOfPlaceOnUpdate(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "version one\n", "initial commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	before, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat published entry: %v", err)
	}
	// A reader that already opened the published tree must keep seeing it whole.
	reader, err := os.Open(filepath.Join(cachePath, "skill.txt"))
	if err != nil {
		t.Fatalf("open published file: %v", err)
	}
	defer func() { _ = reader.Close() }()

	writeAndPushCommit(t, working, "skill.txt", "version two\n", "second commit")
	if _, err := CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	after, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat republished entry: %v", err)
	}
	if os.SameFile(before, after) {
		t.Fatal("expected the update to publish a new directory rather than mutate the published one")
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "version two\n")

	held, err := readAll(reader)
	if err != nil {
		t.Fatalf("read from the descriptor opened before the update: %v", err)
	}
	if held != "version one\n" {
		t.Fatalf("a reader holding the previous tree saw %q, want the complete previous content", held)
	}

	assertNoScratchEntries(t, filepath.Dir(cachePath))
}

// TestCloneOrPullLeavesUnchangedEntryUntouched is the other half of the
// no-torn-reads property: when the ref still resolves to the checked-out commit,
// the refresh writes nothing to the worktree at all.
func TestCloneOrPullLeavesUnchangedEntryUntouched(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "stable\n", "initial commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	beforeDir, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat published entry: %v", err)
	}
	beforeFile, err := os.Stat(filepath.Join(cachePath, "skill.txt"))
	if err != nil {
		t.Fatalf("stat published file: %v", err)
	}

	if _, err := CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	afterDir, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat entry after refresh: %v", err)
	}
	if !os.SameFile(beforeDir, afterDir) {
		t.Fatal("expected an unchanged ref to leave the published entry in place")
	}
	afterFile, err := os.Stat(filepath.Join(cachePath, "skill.txt"))
	if err != nil {
		t.Fatalf("stat file after refresh: %v", err)
	}
	if !afterFile.ModTime().Equal(beforeFile.ModTime()) {
		t.Fatalf("expected the worktree to be untouched, mtime moved from %v to %v", beforeFile.ModTime(), afterFile.ModTime())
	}
}

// TestCloneOrPullRestoresLocallyModifiedEntry keeps the guarantee the previous
// unconditional force-checkout provided: a cache entry someone edited is replaced
// rather than served.
func TestCloneOrPullRestoresLocallyModifiedEntry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "pristine\n", "initial commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	writeFile(t, filepath.Join(cachePath, "skill.txt"), "tampered\n")

	if _, err := CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "pristine\n")
}

// TestCloneOrPullConvergesUnderConcurrentColdClone exercises the cold-publication
// race: several callers discover the same missing entry at once. Every one of them
// must succeed on the same published entry, and none may leave scratch behind.
func TestCloneOrPullConvergesUnderConcurrentColdClone(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "shared\n", "initial commit")

	const callers = 8
	paths := make([]string, callers)
	errs := make([]error, callers)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for i := range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			paths[i], errs[i] = CloneOrPull(origin, "main")
		}()
	}
	close(start)
	wait.Wait()

	want := RegistryCachePath(origin, "main")
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d failed: %v", i, errs[i])
		}
		if paths[i] != want {
			t.Fatalf("caller %d returned %q, want %q", i, paths[i], want)
		}
	}
	assertFileContents(t, filepath.Join(want, "skill.txt"), "shared\n")
	assertNoScratchEntries(t, filepath.Dir(want))
}

// TestCloneOrPullConvergesUnderConcurrentRefresh exercises the same race on the
// update path, where the winner swaps a new directory in underneath the losers.
func TestCloneOrPullConvergesUnderConcurrentRefresh(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "one\n", "initial commit")
	if _, err := CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	writeAndPushCommit(t, working, "skill.txt", "two\n", "second commit")

	const callers = 6
	errs := make([]error, callers)
	var wait sync.WaitGroup
	start := make(chan struct{})
	for i := range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errs[i] = CloneOrPull(origin, "main")
		}()
	}
	close(start)
	wait.Wait()

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d failed: %v", i, errs[i])
		}
	}
	cachePath := RegistryCachePath(origin, "main")
	assertFileContents(t, filepath.Join(cachePath, "skill.txt"), "two\n")
	assertNoScratchEntries(t, filepath.Dir(cachePath))
}

// TestPublishConvergesWhenAnotherWriterPublishedFirst covers the degraded case the
// lock cannot help with — a filesystem that refuses advisory locks — by driving two
// independent stores, which is what two processes are.
func TestPublishConvergesWhenAnotherWriterPublishedFirst(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	url := "https://example.com/org/registry.git"
	entryName := cacheRefComponent("main")

	first, err := openCacheStore(CacheDir(), HashURL(url))
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	defer first.close()
	second, err := openCacheStore(CacheDir(), HashURL(url))
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}
	defer second.close()

	firstTemp, err := first.makeTemp(entryName, tempEntryInfix)
	if err != nil {
		t.Fatalf("first scratch: %v", err)
	}
	secondTemp, err := second.makeTemp(entryName, tempEntryInfix)
	if err != nil {
		t.Fatalf("second scratch: %v", err)
	}
	writeFile(t, filepath.Join(CacheDir(), HashURL(url), firstTemp, "marker.txt"), "first\n")
	writeFile(t, filepath.Join(CacheDir(), HashURL(url), secondTemp, "marker.txt"), "second\n")

	if converged, err := first.publish(firstTemp, entryName); err != nil || converged {
		t.Fatalf("first publish: converged=%v err=%v", converged, err)
	}
	converged, err := second.publish(secondTemp, entryName)
	if err != nil {
		t.Fatalf("second publish must converge rather than fail: %v", err)
	}
	if !converged {
		t.Fatal("expected the second publish to report convergence on the existing entry")
	}
	assertFileContents(t, filepath.Join(CacheDir(), HashURL(url), entryName, "marker.txt"), "first\n")
}

// TestCloneOrPullSweepsAbandonedScratchEntries covers the crash case: a process
// killed between clone and publication leaves a scratch directory that nothing
// else would ever remove.
func TestCloneOrPullSweepsAbandonedScratchEntries(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "swept\n", "initial commit")

	hashPath := filepath.Join(CacheDir(), HashURL(origin))
	abandoned := filepath.Join(hashPath, scratchEntryPrefix+cacheRefComponent("main")+tempEntryInfix+"0123456789abcdef")
	writeFile(t, filepath.Join(abandoned, "partial.txt"), "half a clone\n")

	if _, err := CloneOrPull(origin, "main"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(abandoned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the abandoned scratch entry to be swept, stat error: %v", err)
	}
	assertNoScratchEntries(t, hashPath)
}

// TestCacheRefComponentIsCaseFoldSafe is the case-collision regression. The cache
// entry name is a filesystem path component, and APFS and HFS+ are
// case-insensitive by default, so an encoding whose alphabet contains both cases
// can map two distinct refs onto one entry — serving one registry ref's checkout
// for another's.
func TestCacheRefComponentIsCaseFoldSafe(t *testing.T) {
	refs := []string{"main", "MAIN", "Main", "mAiN", "release", "RELEASE", "v1.2.3", "V1.2.3", "feature/security", "FEATURE/SECURITY", "Feature/Security"}
	for r := range 512 {
		refs = append(refs, fmt.Sprintf("topic/%c%c", 'a'+r%26, 'A'+(r/26)%26))
	}
	for r := range 256 {
		refs = append(refs, strings.Repeat(fmt.Sprintf("long-%03d-", r), 40))
	}

	folded := make(map[string]string, len(refs))
	for _, ref := range refs {
		component := cacheRefComponent(ref)
		if strings.ContainsAny(component, "/\\") || component == "." || component == ".." {
			t.Fatalf("cacheRefComponent(%q) = %q is not a single safe path component", ref, component)
		}
		if len(component) > maxCacheRefComponentLength {
			t.Fatalf("cacheRefComponent(%q) is %d bytes, want at most %d", ref, len(component), maxCacheRefComponentLength)
		}
		key := strings.ToLower(component)
		if previous, clash := folded[key]; clash && previous != ref {
			t.Fatalf("refs %q and %q both fold onto cache entry %q", previous, ref, key)
		}
		folded[key] = ref
	}
}

// TestCacheRefComponentUsesASingleCaseAlphabet states the property the fold-safety
// above rests on, so a future encoding change cannot quietly reintroduce the
// collision.
func TestCacheRefComponentUsesASingleCaseAlphabet(t *testing.T) {
	shortComponent := cacheRefComponent("feature/security")
	if strings.ToUpper(shortComponent) != shortComponent {
		t.Fatalf("encoded component %q mixes case; base32 output must be upper-case only", shortComponent)
	}

	longComponent := cacheRefComponent(strings.Repeat("a", 4096))
	if !strings.HasPrefix(longComponent, refDigestPrefix) {
		t.Fatalf("digest component %q must start with %q so it cannot fold onto an encoded one", longComponent, refDigestPrefix)
	}
	digest := strings.TrimPrefix(longComponent, refDigestPrefix)
	if strings.ToLower(digest) != digest {
		t.Fatalf("digest component %q mixes case; hex output must be lower-case only", longComponent)
	}
	if strings.ContainsAny(shortComponent, refDigestPrefix) {
		t.Fatalf("encoded component %q must not contain the digest prefix %q", shortComponent, refDigestPrefix)
	}
}

// TestCloneOrPullCaseVariantRefsUseDistinctEntries pins the end-to-end
// consequence: two refs differing only in case are two cache entries, on a
// case-insensitive filesystem as much as anywhere else.
func TestCloneOrPullCaseVariantRefsUseDistinctEntries(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	url := "https://example.com/org/registry.git"
	lower := RegistryCachePath(url, "release/alpha")
	upper := RegistryCachePath(url, "release/ALPHA")
	if lower == upper {
		t.Fatalf("case-variant refs collapsed onto one cache path: %q", lower)
	}
	if strings.EqualFold(filepath.Base(lower), filepath.Base(upper)) {
		t.Fatalf("case-variant refs produced case-folding cache components %q and %q", filepath.Base(lower), filepath.Base(upper))
	}
}

func assertNoScratchEntries(t *testing.T, hashPath string) {
	t.Helper()

	entries, err := os.ReadDir(hashPath)
	if err != nil {
		t.Fatalf("read cache directory %q: %v", hashPath, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), scratchEntryPrefix) {
			t.Fatalf("cache directory %q still contains scratch entry %q", hashPath, entry.Name())
		}
	}
}

// readAll reads through the descriptor rather than by path: the point of the
// assertion is what the reader that opened the previous tree still sees, and the
// path now resolves to a different inode.
func readAll(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
