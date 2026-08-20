package cache

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/redact"
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

// StaleCacheFallbackWarning reports that a registry fetch failed and a
// pre-existing cache checkout was used instead. It is a warning, not a
// failure: the returned cache path is usable, but its content may be older
// than the configured ref, so callers that claim an operation "refreshed"
// anything must surface it as a partial outcome.
type StaleCacheFallbackWarning struct {
	URL string // redacted registry URL
	Ref string
	Err error // the underlying fetch failure
}

func (w *StaleCacheFallbackWarning) Error() string {
	return fmt.Sprintf("registry %q (ref %q): fetch failed, using stale cache: %v", w.URL, w.Ref, w.Err)
}

func (w *StaleCacheFallbackWarning) Unwrap() error { return w.Err }

// DedupKey gives the warning a stable identity for diagnostic deduplication:
// one stale fallback per (URL, ref), independent of the underlying error
// text, which may carry transient network detail.
func (w *StaleCacheFallbackWarning) DedupKey() string {
	return "cache.stale-fallback\x00" + w.URL + "\x00" + w.Ref
}

// StaleCacheCollector records the typed stale-cache fallbacks observed through
// a wrapped fetch seam. Registry fetches run concurrently inside the catalog
// loader, so the collector is mutex-guarded; warnings keep first-observed
// order. Callers that report an operation outcome (CLI update/sync, TUI
// update) use Warnings to surface "completed from stale data" as a partial
// outcome instead of claiming a clean refresh.
type StaleCacheCollector struct {
	mu       sync.Mutex
	warnings []error
}

// Wrap adapts a detailed fetch seam (typically CloneOrPullDetailed) to the
// plain two-result form catalog.LoadRegistries and legacy callers consume,
// collecting every stale-cache fallback the seam reports.
func (c *StaleCacheCollector) Wrap(fetch func(url, ref string) (string, *StaleCacheFallbackWarning, error)) func(url, ref string) (string, error) {
	return func(url, ref string) (string, error) {
		path, warning, err := fetch(url, ref)
		if warning != nil {
			c.Add(warning)
		}
		return path, err
	}
}

// Add records one stale-cache fallback observed through another channel
// (e.g. a pipeline stage that reports the typed warning in its own result).
func (c *StaleCacheCollector) Add(warning *StaleCacheFallbackWarning) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warnings = append(c.warnings, warning)
}

// Warnings returns the collected stale-cache fallback warnings in
// first-observed order.
func (c *StaleCacheCollector) Warnings() []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]error(nil), c.warnings...)
}

func CloneOrPull(url, ref string) (string, error) {
	cachePath, _, err := CloneOrPullDetailed(url, ref)
	return cachePath, err
}

// CloneOrPullDetailed is CloneOrPull with an explicit stale-cache fallback
// signal: when the fetch fails but a previous cache checkout exists, it
// returns the cache path, a *StaleCacheFallbackWarning, and a nil error.
// Callers that only check the error keep the historical behavior; callers
// that need to report the fallback truthfully (e.g. the TUI update flow)
// consume the warning.
func CloneOrPullDetailed(url, ref string) (string, *StaleCacheFallbackWarning, error) {
	if strings.TrimSpace(url) == "" {
		return "", nil, errors.New("registry url is required")
	}

	if strings.TrimSpace(ref) == "" {
		return "", nil, errors.New("registry ref is required")
	}

	cachePath := RegistryCachePath(url, ref)
	cacheExists, err := dirExists(cachePath)
	if err != nil {
		return "", nil, fmt.Errorf("check cache path %q: %w", cachePath, err)
	}

	redactedURL := redactSecrets(url)

	if !cacheExists {
		slog.Debug("registry cache miss; cloning", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
			return "", nil, fmt.Errorf("create cache parent for %q: %w", cachePath, err)
		}

		if err := runGit("", "clone", url, cachePath); err != nil {
			return "", nil, fmt.Errorf("clone registry %q into %q: %w", redactedURL, cachePath, err)
		}
	} else {
		slog.Debug("registry cache hit; fetching", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		if err := runGit(cachePath, "fetch", "--tags", "--prune", "origin"); err != nil {
			slog.Warn("using stale cache after fetch failed", "url", redactedURL, "ref", ref, "error", err)
			if checkoutErr := checkoutResolvedRef(cachePath, ref); checkoutErr != nil {
				return "", nil, fmt.Errorf("use stale cache at %q: %w", cachePath, checkoutErr)
			}
			return cachePath, &StaleCacheFallbackWarning{URL: redactedURL, Ref: ref, Err: err}, nil
		}
	}

	slog.Debug("checking out registry ref", "url", redactedURL, "ref", ref, "cache_path", cachePath)
	if err := checkoutResolvedRef(cachePath, ref); err != nil {
		return "", nil, fmt.Errorf("checkout %q in %q: %w", ref, cachePath, err)
	}

	return cachePath, nil, nil
}

func checkoutResolvedRef(repoDir, ref string) error {
	if !gitRefExists(repoDir, "refs/remotes/origin/"+ref) {
		slog.Debug("checking out direct git ref", "repo_dir", repoDir, "ref", ref)
		return runGit(repoDir, "checkout", "--force", ref)
	}

	slog.Debug("checking out remote git ref", "repo_dir", repoDir, "ref", ref)
	return runGit(repoDir, "checkout", "--force", "-B", ref, "origin/"+ref)
}

func gitRefExists(repoDir, ref string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = repoDir
	return cmd.Run() == nil
}

func dirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func runGit(repoDir string, args ...string) error {
	start := time.Now()
	redactedArgs := redactArgs(args)
	redactedCmd := strings.Join(redactedArgs, " ")
	slog.Debug("git command start", "dir", repoDir, "args", redactedArgs)
	cmd := exec.Command("git", args...)
	if repoDir != "" {
		cmd.Dir = repoDir
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		slog.Debug("git command end", "dir", repoDir, "args", redactedArgs, "duration_ms", time.Since(start).Milliseconds(), "success", true)
		return nil
	}

	trimmedOutput := redactSecrets(strings.TrimSpace(string(output)))
	slog.Debug("git command end", "dir", repoDir, "args", redactedArgs, "duration_ms", time.Since(start).Milliseconds(), "success", false, "stderr", trimmedOutput)
	if trimmedOutput == "" {
		return fmt.Errorf("git %s: %w", redactedCmd, err)
	}

	// Bound the error message to prevent screen flooding. Git can emit
	// multi-page stacktraces and verbose diagnostics; keep only the first
	// line for the user-facing error, preserving full detail in the log.
	firstLine := trimmedOutput
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = truncateGitError(firstLine)
	return fmt.Errorf("git %s: %w: %s", redactedCmd, err, firstLine)
}

const maxGitErrorChars = 200

func truncateGitError(message string) string {
	runes := []rune(message)
	if len(runes) <= maxGitErrorChars {
		return message
	}
	return string(runes[:maxGitErrorChars-1]) + "…"
}
