package cache

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/redact"
)

var (
	// errLockBusy reports that another open file description holds the lock.
	errLockBusy = errors.New("registry cache lock is held")
	// errLockUnsupported reports that the filesystem refuses advisory locks, in
	// which case serialization degrades to convergent publication.
	errLockUnsupported = errors.New("registry cache locking is unsupported")
)

// redactSecrets removes embedded credentials from any URLs found in s while
// preserving the rest for debugging. Strings without credentials are returned
// unchanged, so it is safe to apply to arbitrary log fields (URLs, git stderr).
func redactSecrets(s string) string {
	return redact.URL(s)
}

// redactArgs applies redactSecrets to each git argument (the clone argument
// list carries the registry URL, which may embed credentials).
func redactArgs(args []string) []string {
	return redact.Strings(args)
}

// CloneOrPull materializes url at ref inside the registry cache and returns the
// cache path.
//
// Containment: the ref is validated, then encoded into exactly one path
// component, and every filesystem operation is performed through descriptors
// anchored under the cache root (see cacheStore). Nothing a registry or a config
// file can say about a ref can name a location outside the cache.
//
// Atomicity: a published entry is never mutated in place. A refresh that changes
// content is built in a scratch sibling and swapped in with renames, so a
// concurrent reader always resolves the entry path to a complete tree, and a
// failed operation leaves neither a partial entry nor an escaped repository.
func CloneOrPull(url, ref string) (string, error) {
	redactedURL := redactSecrets(url)
	if err := validateGitURL(url); err != nil {
		slog.Error("registry validation failed", "field", "url", "error", err)
		return "", err
	}
	if err := validateGitRef(ref); err != nil {
		slog.Error("registry ref validation failed", "ref", ref, "error", err)
		return "", fmt.Errorf("validate registry ref: %w", err)
	}

	cachePath := RegistryCachePath(url, ref)
	entryName := cacheRefComponent(ref)

	store, err := openCacheStore(CacheDir(), HashURL(url))
	if err != nil {
		slog.Error("registry cache directory validation failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
		return "", fmt.Errorf("check cache path %q: %w", cachePath, err)
	}
	defer store.close()

	if err := store.lock(); err != nil {
		if !errors.Is(err, errLockUnsupported) {
			slog.Error("registry cache lock failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
			cleanupCacheSkeleton(store, cachePath)
			return "", fmt.Errorf("lock registry cache for %q: %w", cachePath, err)
		}
		// Degraded, not silent: publication is a rename that converges on a
		// concurrent winner, so an unserialized run stays correct.
		slog.Warn("registry cache locking unavailable; relying on convergent publication", "cache_path", cachePath, "error", err)
	}
	store.sweepScratch()

	resultPath, err := syncCacheEntry(store, url, ref, entryName, cachePath)
	if err != nil {
		cleanupCacheSkeleton(store, cachePath)
		return "", err
	}
	return resultPath, nil
}

// cleanupCacheSkeleton drops the directories this run created so a failed
// operation leaves no cache behind. The lock is released first: the directory it
// is taken on is one of the directories being removed.
func cleanupCacheSkeleton(store *cacheStore, cachePath string) {
	store.unlock()
	if err := store.cleanupNew(); err != nil {
		slog.Warn("remove unused registry cache directories", "cache_path", cachePath, "error", err)
	}
}

func syncCacheEntry(store *cacheStore, url, ref, entryName, cachePath string) (string, error) {
	redactedURL := redactSecrets(url)
	entry, err := store.openEntry(entryName)
	switch {
	case err == nil:
		defer func() { _ = entry.Close() }()
		slog.Debug("registry cache hit; refreshing", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		return refreshCacheEntry(store, entry, url, ref, entryName, cachePath)
	case errors.Is(err, os.ErrNotExist):
		slog.Debug("registry cache miss; cloning", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		return createCacheEntry(store, url, ref, entryName, cachePath)
	default:
		slog.Error("registry cache entry validation failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
		return "", fmt.Errorf("check cache path %q: %w", cachePath, err)
	}
}

func createCacheEntry(store *cacheStore, url, ref, entryName, cachePath string) (string, error) {
	redactedURL := redactSecrets(url)
	temp, err := buildCacheEntry(store, url, ref, entryName, "", "")
	if err != nil {
		slog.Error("registry cache clone failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
		return "", fmt.Errorf("clone registry %q into %q: %w", redactedURL, cachePath, err)
	}

	converged, err := store.publish(temp, entryName)
	if err != nil {
		if rmErr := store.removeEntry(temp); rmErr != nil {
			slog.Warn("remove unpublished registry cache entry", "entry", temp, "error", rmErr)
		}
		slog.Error("registry cache publication failed", "cache_path", cachePath, "ref", ref, "error", err)
		return "", fmt.Errorf("publish cache entry %q: %w", cachePath, err)
	}
	if converged {
		// Another process published the same ref first. Its entry is equivalent,
		// so discard ours rather than fighting over the name.
		slog.Debug("registry cache entry published concurrently; converging", "cache_path", cachePath, "ref", ref)
		if rmErr := store.removeEntry(temp); rmErr != nil {
			slog.Warn("remove superseded registry cache entry", "entry", temp, "error", rmErr)
		}
		return cachePath, nil
	}

	slog.Debug("registry cache published", "cache_path", cachePath, "ref", ref)
	return cachePath, nil
}

func refreshCacheEntry(store *cacheStore, entry *os.File, url, ref, entryName, cachePath string) (string, error) {
	redactedURL := redactSecrets(url)

	// Fetching mutates only the object store and the remote-tracking refs, never
	// the worktree, so it is safe to do in the published entry: a reader of the
	// checked-out files cannot observe it.
	fetchErr := runGitIn(entry, "fetch", "--tags", "--prune", "origin")
	if fetchErr != nil {
		slog.Warn("using stale cache after fetch failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", fetchErr)
	}

	target, resolveErr := resolveCommit(entry, ref)
	if resolveErr != nil {
		if fetchErr != nil {
			slog.Error("stale registry cache resolution failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", resolveErr)
			return "", fmt.Errorf("use stale cache at %q: %w", cachePath, errors.Join(fetchErr, resolveErr))
		}
		slog.Error("registry ref resolution failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", resolveErr)
		return "", fmt.Errorf("checkout %q in %q: %w", ref, cachePath, resolveErr)
	}

	if cacheEntryIsAt(entry, target) {
		slog.Debug("registry cache entry already at target commit", "url", redactedURL, "ref", ref, "cache_path", cachePath, "commit", target)
		return cachePath, nil
	}

	slog.Debug("rebuilding registry cache entry out of place", "url", redactedURL, "ref", ref, "cache_path", cachePath, "commit", target)
	temp, err := buildCacheEntry(store, url, ref, entryName, entryName, target)
	if err != nil {
		slog.Error("registry cache rebuild failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
		return "", fmt.Errorf("checkout %q in %q: %w", ref, cachePath, err)
	}
	if err := store.swap(temp, entryName); err != nil {
		if rmErr := store.removeEntry(temp); rmErr != nil {
			slog.Warn("remove unpublished registry cache entry", "entry", temp, "error", rmErr)
		}
		slog.Error("registry cache publication failed", "cache_path", cachePath, "ref", ref, "error", err)
		return "", fmt.Errorf("publish cache entry %q: %w", cachePath, err)
	}

	slog.Debug("registry cache republished", "cache_path", cachePath, "ref", ref, "commit", target)
	return cachePath, nil
}

// buildCacheEntry clones into a fresh scratch sibling of entryName, checks out
// ref there, and returns the scratch name for the caller to publish or swap in.
// The scratch directory is removed if anything fails, so a failed operation never
// leaves a partial entry.
//
// localSource, when non-empty, names a sibling cache entry to clone from:
// `git clone --local` hardlinks the object store, so a refresh costs neither a
// second network transfer nor an object copy, and the hardlinks outlive the
// source entry that the swap retires. target, when non-empty, is an
// already-resolved commit to check out.
func buildCacheEntry(store *cacheStore, url, ref, entryName, localSource, target string) (name string, err error) {
	temp, err := store.makeTemp(entryName, tempEntryInfix)
	if err != nil {
		return "", err
	}
	defer func() {
		if err == nil {
			return
		}
		if rmErr := store.removeEntry(temp); rmErr != nil {
			slog.Warn("remove failed temporary registry cache entry", "entry", temp, "error", rmErr)
		}
	}()

	hashDir := store.dir()
	if localSource != "" {
		if cloneErr := runGitIn(hashDir, "clone", "--no-checkout", "--local", "--", localSource, temp); cloneErr != nil {
			// A damaged local source must not wedge the cache: fall back to the
			// registry and pay the network cost once. The resolved target is
			// dropped with it, because it was resolved against that source.
			slog.Warn("local registry cache clone failed; cloning from registry", "source", localSource, "error", cloneErr)
			if rmErr := store.removeEntry(temp); rmErr != nil {
				return "", rmErr
			}
			if mkErr := store.hash.Mkdir(temp, cacheDirPerm); mkErr != nil {
				return "", mkErr
			}
			localSource, target = "", ""
		}
	}

	if localSource == "" {
		if err = runGitIn(hashDir, "clone", "--no-checkout", "--", url, temp); err != nil {
			return "", err
		}
	} else if err = runGitIn(hashDir, "-C", temp, "remote", "set-url", "origin", url); err != nil {
		// A local clone inherits the source entry's path as its origin URL, which
		// stops existing once the swap retires that entry.
		return "", err
	}

	scratch, err := store.openEntry(temp)
	if err != nil {
		return "", err
	}
	defer func() { _ = scratch.Close() }()

	if target == "" {
		if target, err = resolveCommit(scratch, ref); err != nil {
			return "", err
		}
	}
	if err = runGitIn(scratch, "-c", "advice.detachedHead=false", "checkout", "--force", "--detach", target); err != nil {
		return "", err
	}
	return temp, nil
}

// resolveCommit resolves ref to a full object name inside repo.
//
// Resolution is explicit and ordered — remote-tracking ref, tag, then the ref as
// written — and every candidate is suffixed with ^{commit} and passed after
// --end-of-options. That is what stops a ref from being reinterpreted as
// something else: `git checkout <ref>` falls back to treating its argument as a
// *pathspec* when no such ref exists, so a registry pinned to a ref that does
// not exist but that happens to name a tracked file "succeeds" and silently
// serves the default branch's content. rev-parse has no pathspec fallback, and a
// resolved object name checked out with --detach cannot be reinterpreted either.
func resolveCommit(repo *os.File, ref string) (string, error) {
	for _, candidate := range []string{"refs/remotes/origin/" + ref, "refs/tags/" + ref, ref} {
		out, err := gitOutputIn(repo, "rev-parse", "--verify", "--quiet", "--end-of-options", candidate+"^{commit}")
		if err != nil {
			continue
		}
		if commit := strings.TrimSpace(out); isObjectName(commit) {
			return commit, nil
		}
	}
	return "", fmt.Errorf("git ref %q does not resolve to a commit", ref)
}

// isObjectName reports whether s is a full git object name. Both hash lengths are
// accepted because a registry may be a SHA-256 repository.
func isObjectName(s string) bool {
	return (len(s) == 40 || len(s) == 64) && isHex(s)
}

// cacheEntryIsAt reports whether the entry's worktree already is target with no
// local modification.
//
// A matching, clean entry is left completely untouched, which is what keeps the
// common refresh free of worktree writes — and therefore free of any window in
// which a concurrent reader could see a half-updated tree.
func cacheEntryIsAt(entry *os.File, target string) bool {
	head, err := gitOutputIn(entry, "rev-parse", "--verify", "--quiet", "--end-of-options", "HEAD^{commit}")
	if err != nil || strings.TrimSpace(head) != target {
		return false
	}
	// `diff --quiet` exits non-zero on any difference from HEAD. Every error is
	// treated as "rebuild", so a damaged entry is replaced rather than served.
	return runGitIn(entry, "diff", "--quiet", "HEAD") == nil
}

func validateGitURL(url string) error {
	trimmed := strings.TrimSpace(url)
	if trimmed == "" {
		return errors.New("registry url is required")
	}
	// git clone is always given the URL after a "--" separator, but an
	// option-like URL is a configuration mistake worth reporting rather than
	// passing to git as a path.
	if strings.HasPrefix(trimmed, "-") {
		return errors.New("invalid registry url: option-like")
	}
	return nil
}

func validateGitRef(ref string) error {
	if strings.TrimSpace(ref) == "" {
		return errors.New("registry ref is required")
	}
	if strings.HasPrefix(ref, "-") {
		return errors.New("invalid git ref: option-like")
	}
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") {
		return errors.New("invalid git ref: absolute or tilde path")
	}
	if ref == "@" {
		return errors.New("invalid git ref: lone '@'")
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") {
		return errors.New("invalid git ref: contains '..' or '@{'")
	}
	if strings.HasSuffix(ref, "/") || strings.Contains(ref, "//") {
		return errors.New("invalid git ref: invalid slash")
	}
	if strings.HasSuffix(ref, ".") {
		return errors.New("invalid git ref: invalid suffix")
	}
	if strings.ContainsAny(ref, " ~^:?*[\\\\") {
		return errors.New("invalid git ref: invalid character")
	}
	for i := 0; i < len(ref); i++ {
		if ref[i] < 0x20 || ref[i] == 0x7f {
			return errors.New("invalid git ref: control character")
		}
	}
	for _, component := range strings.Split(ref, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return errors.New("invalid git ref: invalid component")
		}
	}
	return nil
}

// runGitIn runs git anchored to dir. Every path in args must be relative to dir.
func runGitIn(dir *os.File, args ...string) error {
	_, err := gitOutputIn(dir, args...)
	return err
}

// gitOutputIn runs git anchored to dir and returns its standard output. Standard
// error is kept out of the returned value so that a warning on stderr cannot be
// mistaken for output by a caller parsing it.
func gitOutputIn(dir *os.File, args ...string) (string, error) {
	cmd, err := gitCommand(dir, args)
	if err != nil {
		return "", err
	}
	return runGitProcess(cmd, dir.Name(), args)
}

// runGit runs git with a working directory given as a path. It is the unanchored
// variant, kept for the tests and benchmarks that set up fixture repositories
// outside the cache; everything that touches the cache uses runGitIn.
func runGit(repoDir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	_, err := runGitProcess(cmd, repoDir, args)
	return err
}

func runGitProcess(cmd *exec.Cmd, dir string, args []string) (string, error) {
	start := time.Now()
	redactedArgs := redactArgs(args)
	redactedCmd := strings.Join(redactedArgs, " ")
	slog.Debug("git command start", "dir", dir, "args", redactedArgs)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		slog.Debug("git command end", "dir", dir, "args", redactedArgs, "duration_ms", time.Since(start).Milliseconds(), "success", true)
		return stdout.String(), nil
	}

	diagnostics := redactSecrets(strings.TrimSpace(stderr.String() + "\n" + stdout.String()))
	slog.Debug("git command end", "dir", dir, "args", redactedArgs, "duration_ms", time.Since(start).Milliseconds(), "success", false, "stderr", diagnostics)
	if diagnostics == "" {
		return "", fmt.Errorf("git %s: %w", redactedCmd, err)
	}

	// Bound the error message to prevent screen flooding. Git can emit
	// multi-page stacktraces and verbose diagnostics; keep only the first
	// line for the user-facing error, preserving full detail in the log.
	firstLine := diagnostics
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = truncateGitError(firstLine)
	return "", fmt.Errorf("git %s: %w: %s", redactedCmd, err, firstLine)
}

const maxGitErrorChars = 200

func truncateGitError(message string) string {
	runes := []rune(message)
	if len(runes) <= maxGitErrorChars {
		return message
	}
	return string(runes[:maxGitErrorChars-1]) + "…"
}
