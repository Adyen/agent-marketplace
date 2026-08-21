package cache

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"slices"
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
	state, err := store.classifyEntry(entryName)
	if err != nil {
		slog.Error("registry cache entry validation failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
		return "", fmt.Errorf("check cache path %q: %w", cachePath, err)
	}

	switch state {
	case entryMissing:
		slog.Debug("registry cache miss; cloning", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		return createCacheEntry(store, url, ref, entryName, cachePath)
	case entryCorrupt:
		// Rebuilt rather than refreshed: git would answer "not a repository" by
		// searching upwards, and rather than serve whatever it found there, the
		// entry is replaced by a clone of the registry it is supposed to hold.
		slog.Warn("registry cache entry is not a usable checkout; rebuilding", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		return rebuildCacheEntry(store, url, ref, entryName, cachePath, nil)
	}

	entry, err := store.openEntry(entryName)
	if err != nil {
		slog.Error("registry cache entry validation failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
		return "", fmt.Errorf("check cache path %q: %w", cachePath, err)
	}
	defer func() { _ = entry.Close() }()
	slog.Debug("registry cache hit; refreshing", "url", redactedURL, "ref", ref, "cache_path", cachePath)
	return refreshCacheEntry(store, entry, url, ref, entryName, cachePath)
}

func createCacheEntry(store *cacheStore, url, ref, entryName, cachePath string) (string, error) {
	redactedURL := redactSecrets(url)
	temp, err := buildCacheEntry(store, url, ref, entryName, nil)
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

	pin, pinErr := store.readPin(entryName)
	if pinErr != nil {
		// A pin this package wrote and cannot read back means the entry is not
		// reliably the checkout its name claims, so it is rebuilt rather than
		// refreshed against a ref class nothing vouches for.
		slog.Warn("registry cache pin unreadable; rebuilding entry", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", pinErr)
		return rebuildCacheEntry(store, url, ref, entryName, cachePath, nil)
	}

	// Fetching mutates only the object store and the remote-tracking refs, never
	// the worktree, so it is safe to do in the published entry: a reader of the
	// checked-out files cannot observe it.
	fetchErr := runGitInRepo(entry, "fetch", "--tags", "--prune", "origin")
	if fetchErr != nil {
		slog.Warn("using stale cache after fetch failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", fetchErr)
		// Offline, the entry is served as the exact commit the last successful run
		// resolved, taken from the pin. Re-resolving the ref instead would decide
		// what to serve from whatever refs the last fetch happened to leave
		// behind, which is how a stale same-named tag gets promoted into a
		// registry's content without anything upstream having changed.
		if pin != nil && cacheEntryIsAt(entry, pin.commit) {
			slog.Debug("serving pinned commit from stale registry cache", "url", redactedURL, "ref", ref, "cache_path", cachePath, "commit", pin.commit, "class", string(pin.class))
			return cachePath, nil
		}
	}

	target, class, resolveErr := resolveCommit(entry, ref, pin)
	if resolveErr != nil {
		if fetchErr != nil {
			slog.Error("stale registry cache resolution failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", resolveErr)
			return "", fmt.Errorf("use stale cache at %q: %w", cachePath, errors.Join(fetchErr, resolveErr))
		}
		slog.Error("registry ref resolution failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", resolveErr)
		return "", fmt.Errorf("checkout %q in %q: %w", ref, cachePath, resolveErr)
	}
	resolved := refPin{class: class, commit: target}

	if cacheEntryIsAt(entry, target) {
		// An entry from a release that predated pin recording, or one whose
		// branch has moved back onto the commit it already holds, gets its pin
		// written now: the record lives in .git, so writing it is invisible to a
		// reader walking the worktree.
		if pin == nil || *pin != resolved {
			if err := store.writePin(entryName, resolved); err != nil {
				slog.Warn("record registry cache pin", "cache_path", cachePath, "ref", ref, "error", err)
			}
		}
		slog.Debug("registry cache entry already at target commit", "url", redactedURL, "ref", ref, "cache_path", cachePath, "commit", target)
		return cachePath, nil
	}

	slog.Debug("rebuilding registry cache entry out of place", "url", redactedURL, "ref", ref, "cache_path", cachePath, "commit", target)
	return rebuildCacheEntry(store, url, ref, entryName, cachePath, &localRebuild{name: entryName, dir: entry, pin: resolved})
}

// rebuildCacheEntry builds a replacement for an existing entry out of place and
// swaps it in. from is nil when the existing entry cannot be reused as a local
// clone source — which is the case for every entry that is not a valid repository.
func rebuildCacheEntry(store *cacheStore, url, ref, entryName, cachePath string, from *localRebuild) (string, error) {
	redactedURL := redactSecrets(url)
	temp, err := buildCacheEntry(store, url, ref, entryName, from)
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

	slog.Debug("registry cache republished", "cache_path", cachePath, "ref", ref)
	return cachePath, nil
}

// localRebuild describes an existing published entry that a rebuild can be seeded
// from: `git clone --local` hardlinks its object store, so a refresh costs neither
// a second network transfer nor an object copy, and the hardlinks outlive the
// entry the swap retires.
type localRebuild struct {
	// name is the sibling entry name, for the clone.
	name string
	// dir is an open descriptor on that entry, for reading its refs. Reading them
	// through the descriptor rather than by name is what keeps the copy anchored.
	dir *os.File
	// pin is the commit and ref class already resolved against that entry.
	pin refPin
}

// buildCacheEntry clones into a fresh scratch sibling of entryName, checks out
// ref there, records the pin, and returns the scratch name for the caller to
// publish or swap in. The scratch directory is removed if anything fails, so a
// failed operation never leaves a partial entry.
func buildCacheEntry(store *cacheStore, url, ref, entryName string, from *localRebuild) (name string, err error) {
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

	// requested carries the ref class the caller already established, so that a
	// rebuild forced onto the registry keeps resolving the same *kind* of ref even
	// though it has to resolve the commit again.
	var requested *refPin
	if from != nil {
		established := from.pin
		requested = &established

		if seedErr := seedFromLocalEntry(store, temp, url, from); seedErr != nil {
			// A damaged source must not wedge the cache: a deterministic failure
			// here would otherwise fail every future sync until somebody deleted
			// the cache. Fall back to the registry and pay the network cost once.
			// The resolved commit is dropped with it, because it was resolved
			// against that source.
			slog.Warn("seeding the rebuilt registry cache entry from the existing one failed; cloning from the registry", "source", from.name, "error", seedErr)
			if resetErr := resetScratchEntry(store, temp); resetErr != nil {
				return "", resetErr
			}
			from = nil
		}
	}

	if from == nil {
		if err = runGitIn(store.dir(), "clone", "--no-checkout", "--", url, temp); err != nil {
			return "", err
		}
	}

	scratch, err := store.openEntry(temp)
	if err != nil {
		return "", err
	}
	defer func() { _ = scratch.Close() }()

	pin := refPin{}
	if from != nil {
		pin = from.pin
	}
	if pin.commit == "" {
		if pin.commit, pin.class, err = resolveCommit(scratch, ref, requested); err != nil {
			return "", err
		}
	}
	if err = runGitInRepo(scratch, "-c", "advice.detachedHead=false", "checkout", "--force", "--detach", pin.commit); err != nil {
		return "", err
	}
	if err = store.writePin(temp, pin); err != nil {
		return "", err
	}
	return temp, nil
}

// seedFromLocalEntry fills the scratch entry temp from the published entry a
// rebuild replaces, so that the rebuild costs no network transfer and no object
// copy: `git clone --local` hardlinks the object store, and the hardlinks outlive
// the entry the swap retires.
func seedFromLocalEntry(store *cacheStore, temp, url string, from *localRebuild) error {
	if err := runGitIn(store.dir(), "clone", "--no-checkout", "--local", "--", from.name, temp); err != nil {
		return err
	}
	scratch, err := store.openEntry(temp)
	if err != nil {
		return err
	}
	defer func() { _ = scratch.Close() }()

	// A local clone inherits the source entry's path as its origin URL, which
	// stops existing once the swap retires that entry.
	if err := runGitInRepo(scratch, "remote", "set-url", "origin", url); err != nil {
		return err
	}
	return copyRemoteTrackingRefs(from.dir, scratch)
}

// resetScratchEntry empties a scratch entry so a failed seeding attempt can be
// replaced by a clone from the registry.
func resetScratchEntry(store *cacheStore, temp string) error {
	if err := store.removeEntry(temp); err != nil {
		return err
	}
	return store.hash.Mkdir(temp, cacheDirPerm)
}

// remoteTrackingPrefix is the only ref namespace a pinned ref is resolved
// against, which is why it is also the only one a rebuild has to carry over.
const remoteTrackingPrefix = "refs/remotes/origin/"

// copyRemoteTrackingRefs makes target's remote-tracking refs identical to
// source's.
//
// `git clone` maps the *local* branches of the repository it clones into
// refs/remotes/origin/*, so a clone of a cache entry — which has one local branch
// left over from its own clone and a detached HEAD — comes out with almost none of
// the remote-tracking refs the entry had. Losing them is not cosmetic: they are
// what a pinned branch ref resolves against, so an offline refresh of a rebuilt
// entry would find no branch and could only fall back to some other kind of ref
// with the same name.
//
// Symbolic refs (refs/remotes/origin/HEAD) are skipped in both directions: they
// are set by the clone, are not resolution candidates, and updating one through
// update-ref would rewrite whatever it points at.
func copyRemoteTrackingRefs(source, target *os.File) error {
	want, err := remoteTrackingRefs(source)
	if err != nil {
		return fmt.Errorf("read remote-tracking refs of the cache entry being rebuilt: %w", err)
	}
	have, err := remoteTrackingRefs(target)
	if err != nil {
		return fmt.Errorf("read remote-tracking refs of the rebuilt cache entry: %w", err)
	}

	var updates strings.Builder
	for _, name := range slices.Sorted(maps.Keys(want)) {
		if have[name] == want[name] {
			continue
		}
		fmt.Fprintf(&updates, "update %s %s\n", name, want[name])
	}
	for _, name := range slices.Sorted(maps.Keys(have)) {
		if _, keep := want[name]; !keep {
			fmt.Fprintf(&updates, "delete %s\n", name)
		}
	}
	if updates.Len() == 0 {
		return nil
	}
	// One update-ref invocation is one transaction, so the rebuilt entry either
	// has exactly the source's remote-tracking refs or has none of the changes and
	// is discarded with the scratch directory. --no-deref keeps a symbolic ref on
	// the target side from turning an update into an update of its target.
	if err := gitStdinInRepo(target, updates.String(), "update-ref", "--no-deref", "--stdin"); err != nil {
		return fmt.Errorf("copy remote-tracking refs into the rebuilt cache entry: %w", err)
	}
	return nil
}

func remoteTrackingRefs(repo *os.File) (map[string]string, error) {
	out, err := gitOutputInRepo(repo, "for-each-ref", "--format=%(refname) %(objectname) %(symref)", remoteTrackingPrefix)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]string)
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		// Three fields means %(symref) was non-empty, i.e. a symbolic ref.
		if len(fields) != 2 {
			continue
		}
		if !strings.HasPrefix(fields[0], remoteTrackingPrefix) || !isObjectName(fields[1]) {
			continue
		}
		refs[fields[0]] = fields[1]
	}
	return refs, nil
}

// refClass is the *kind* of ref a cache entry was built from. It is recorded with
// the entry because the kind is part of what a config pinned, not an incidental
// detail of how it was found: a ref that used to name a branch and now names a tag
// names different content published by a different act.
type refClass string

const (
	refClassBranch refClass = "branch"
	refClassTag    refClass = "tag"
	refClassObject refClass = "object"
)

func (c refClass) valid() bool {
	switch c {
	case refClassBranch, refClassTag, refClassObject:
		return true
	}
	return false
}

// refPin is a resolved ref: the commit, and the kind of ref it came from.
type refPin struct {
	class  refClass
	commit string
}

// refCandidates maps a configured ref onto the refs it may resolve against.
// Either may be empty when the configured ref is already qualified, which is how
// a user disambiguates a name that is both a branch and a tag.
func refCandidates(ref string) (branchRef, tagRef string) {
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		return remoteTrackingPrefix + strings.TrimPrefix(ref, "refs/heads/"), ""
	case strings.HasPrefix(ref, "refs/tags/"):
		return "", ref
	default:
		return remoteTrackingPrefix + ref, "refs/tags/" + ref
	}
}

// resolveCommit resolves ref to a full object name inside repo, and reports which
// kind of ref it resolved against.
//
// Resolution is explicit, and every candidate is suffixed with ^{commit} and
// passed after --end-of-options. That is what stops a ref from being reinterpreted
// as something else: `git checkout <ref>` falls back to treating its argument as a
// *pathspec* when no such ref exists, so a registry pinned to a ref that does not
// exist but that happens to name a tracked file "succeeds" and silently serves the
// default branch's content. rev-parse has no pathspec fallback, and a resolved
// object name checked out with --detach cannot be reinterpreted either.
//
// pin, when non-nil, is what the entry currently holds, and it narrows resolution
// to that one kind of ref. A pinned kind that has stopped resolving is an error
// rather than a reason to try the others, because the others are exactly the
// substitution worth preventing: a branch deleted upstream leaves a same-named tag
// — which anyone who can push a tag controls — as the next candidate, and silently
// promoting it changes the content a registry serves with nothing in the
// configuration having changed.
func resolveCommit(repo *os.File, ref string, pin *refPin) (string, refClass, error) {
	branchRef, tagRef := refCandidates(ref)

	if pin != nil {
		pinned := ""
		switch pin.class {
		case refClassObject:
			commit, err := resolveObjectRef(repo, ref)
			if err != nil {
				return "", "", err
			}
			return commit, refClassObject, nil
		case refClassBranch:
			pinned = branchRef
		case refClassTag:
			pinned = tagRef
		}
		if pinned != "" {
			commit, err := resolveCommitish(repo, pinned)
			if err != nil {
				return "", "", fmt.Errorf("git ref %q was published from %s %q, which no longer resolves; refusing to substitute a different kind of ref (remove the cache entry to re-pin): %w", ref, pin.class, pinned, err)
			}
			return commit, pin.class, nil
		}
	}

	branchCommit, branchErr := resolveCandidate(repo, branchRef)
	tagCommit, tagErr := resolveCandidate(repo, tagRef)
	switch {
	case branchErr == nil && tagErr == nil && branchCommit != tagCommit:
		// Git itself calls this ambiguous and picks one with a warning. Picking one
		// here would mean the content served depends on a precedence rule nobody
		// configured, so it is refused with the two qualified names that resolve it.
		return "", "", fmt.Errorf("git ref %q is ambiguous: branch %q is %s and tag %q is %s; pin the ref as refs/heads/… or refs/tags/…", ref, branchRef, branchCommit, tagRef, tagCommit)
	case branchErr == nil:
		return branchCommit, refClassBranch, nil
	case tagErr == nil:
		return tagCommit, refClassTag, nil
	}

	commit, err := resolveObjectRef(repo, ref)
	if err != nil {
		return "", "", err
	}
	return commit, refClassObject, nil
}

// resolveCandidate resolves a fully qualified candidate, treating an empty
// candidate as "does not apply".
func resolveCandidate(repo *os.File, candidate string) (string, error) {
	if candidate == "" {
		return "", errors.New("no candidate ref")
	}
	return resolveCommitish(repo, candidate)
}

// resolveObjectRef resolves ref as an object expression — which is how a commit
// pin works — and refuses anything that is still a ref name.
//
// The guard matters because the tracking candidates have already failed by this
// point, so any *ref* that still resolves is one this cache created rather than
// anything upstream: `git clone` leaves refs/heads/<default> behind, and serving
// it would mean answering a request for a branch that no longer exists upstream
// with a local leftover.
func resolveObjectRef(repo *os.File, ref string) (string, error) {
	commit, err := resolveCommitish(repo, ref)
	if err != nil {
		return "", fmt.Errorf("git ref %q does not resolve to a commit", ref)
	}
	if isObjectName(ref) {
		return commit, nil
	}
	// No --end-of-options here: rev-parse echoes it into the output in this mode,
	// which would make every argument look as though it had resolved to a ref. It
	// is not needed either, because validateGitRef has already refused an
	// option-like ref.
	name, nameErr := gitOutputInRepo(repo, "rev-parse", "--quiet", "--symbolic-full-name", ref)
	if nameErr != nil {
		return commit, nil
	}
	// rev-parse prints nothing for an argument that resolved to an object rather
	// than to a ref, so a printed ref name is the whole signal. It is matched by
	// shape because rev-parse also echoes arguments it could not interpret.
	for line := range strings.SplitSeq(name, "\n") {
		if resolved := strings.TrimSpace(line); strings.HasPrefix(resolved, "refs/") || resolved == "HEAD" {
			return "", fmt.Errorf("git ref %q does not resolve to a commit in the registry; it only names the local ref %q", ref, resolved)
		}
	}
	return commit, nil
}

func resolveCommitish(repo *os.File, expression string) (string, error) {
	out, err := gitOutputInRepo(repo, "rev-parse", "--verify", "--quiet", "--end-of-options", expression+"^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(out)
	if !isObjectName(commit) {
		return "", fmt.Errorf("git %q did not resolve to an object name", expression)
	}
	return commit, nil
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
	head, err := gitOutputInRepo(entry, "rev-parse", "--verify", "--quiet", "--end-of-options", "HEAD^{commit}")
	if err != nil || strings.TrimSpace(head) != target {
		return false
	}
	// `diff --quiet` exits non-zero on any difference from HEAD. Every error is
	// treated as "rebuild", so a damaged entry is replaced rather than served.
	return runGitInRepo(entry, "diff", "--quiet", "HEAD") == nil
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

// repoScopeArgs pin git to the repository *in* the anchored directory.
//
// They are what stops upward repository discovery. Without an explicit --git-dir,
// git answers "this is not a repository" by walking towards the filesystem root
// looking for one, so an entry missing its .git would make `fetch`, `diff` and
// `checkout --force` operate on whichever repository contains the cache — the
// user's own checkout, when the cache sits under it. With it, the same situation
// is a clean "not a git repository: '.git'".
//
// --work-tree is given too so that neither core.worktree nor a bare-repository
// setting inside the entry can move the tree the checkout writes to.
var repoScopeArgs = []string{"--git-dir=" + gitDirName, "--work-tree=."}

// runGitIn runs git anchored to dir, without assuming dir is a repository. Every
// path in args must be relative to dir.
func runGitIn(dir *os.File, args ...string) error {
	_, err := gitOutputIn(dir, args...)
	return err
}

// gitOutputIn runs git anchored to dir and returns its standard output. Standard
// error is kept out of the returned value so that a warning on stderr cannot be
// mistaken for output by a caller parsing it.
func gitOutputIn(dir *os.File, args ...string) (string, error) {
	return gitIn(dir, "", args)
}

// runGitInRepo runs git against the repository in the anchored directory repo.
func runGitInRepo(repo *os.File, args ...string) error {
	_, err := gitOutputInRepo(repo, args...)
	return err
}

func gitOutputInRepo(repo *os.File, args ...string) (string, error) {
	return gitIn(repo, "", scopedGitArgs(args))
}

// gitStdinInRepo runs git against the repository in repo, feeding it stdin. It
// exists for `update-ref --stdin`, whose whole point is that one invocation is one
// transaction.
func gitStdinInRepo(repo *os.File, stdin string, args ...string) error {
	_, err := gitIn(repo, stdin, scopedGitArgs(args))
	return err
}

func scopedGitArgs(args []string) []string {
	scoped := make([]string, 0, len(repoScopeArgs)+len(args))
	scoped = append(scoped, repoScopeArgs...)
	return append(scoped, args...)
}

func gitIn(dir *os.File, stdin string, args []string) (string, error) {
	cmd, err := gitCommand(dir, args)
	if err != nil {
		return "", err
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
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
