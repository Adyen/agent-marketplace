package cache

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func CloneOrPull(url, ref string) (string, error) {
	redactedURL := redactSecrets(url)
	if strings.TrimSpace(url) == "" {
		err := errors.New("registry url is required")
		slog.Error("registry validation failed", "field", "url", "error", err)
		return "", err
	}
	if err := validateGitRef(ref); err != nil {
		slog.Error("registry ref validation failed", "ref", ref, "error", err)
		return "", fmt.Errorf("validate registry ref: %w", err)
	}

	cachePath := RegistryCachePath(url, ref)
	cacheDirs, err := openCacheDirs(CacheDir(), HashURL(url))
	if err == nil {
		defer cacheDirs.close()
		cacheEntry, entryErr := cacheDirs.openEntry(cacheRefComponent(ref))
		switch {
		case entryErr == nil:
			defer func() { _ = cacheEntry.Close() }()
			slog.Debug("registry cache hit; fetching", "url", redactedURL, "ref", ref, "cache_path", cachePath)
			if err := runGitAt(cacheEntry, "fetch", "--tags", "--prune", "origin"); err != nil {
				slog.Warn("using stale cache after fetch failed", "url", redactedURL, "ref", ref, "error", err)
				if checkoutErr := checkoutResolvedRefAt(cacheEntry, ref); checkoutErr != nil {
					slog.Error("stale registry cache checkout failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", checkoutErr)
					return "", fmt.Errorf("use stale cache at %q: %w", cachePath, checkoutErr)
				}
				return cachePath, nil
			}

			slog.Debug("checking out registry ref", "url", redactedURL, "ref", ref, "cache_path", cachePath)
			if err := checkoutResolvedRefAt(cacheEntry, ref); err != nil {
				slog.Error("registry cache checkout failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
				return "", fmt.Errorf("checkout %q in %q: %w", ref, cachePath, err)
			}
			return cachePath, nil
		case errors.Is(entryErr, os.ErrNotExist):
		default:
			slog.Error("registry cache entry validation failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", entryErr)
			return "", fmt.Errorf("check cache path %q: %w", cachePath, entryErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Error("registry cache directory validation failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
		return "", fmt.Errorf("check cache path %q: %w", cachePath, err)
	}

	slog.Debug("registry cache miss; cloning", "url", redactedURL, "ref", ref, "cache_path", cachePath)
	if err := cloneAndPublish(url, ref, cachePath); err != nil {
		slog.Error("registry cache clone failed", "url", redactedURL, "ref", ref, "cache_path", cachePath, "error", err)
		return "", fmt.Errorf("clone registry %q into %q: %w", redactedURL, cachePath, err)
	}
	return cachePath, nil
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

func cloneAndPublish(url, ref, cachePath string) error {
	cacheDirs, err := createCacheDirs(CacheDir(), HashURL(url))
	if err != nil {
		slog.Error("registry cache setup failed", "cache_path", cachePath, "ref", ref, "error", err)
		return fmt.Errorf("create cache parent for %q: %w", cachePath, err)
	}
	defer cacheDirs.close()

	tempName, err := cacheDirs.makeTemp("." + filepath.Base(cachePath) + ".tmp-")
	if err != nil {
		slog.Error("registry cache temporary directory setup failed", "cache_path", cachePath, "ref", ref, "error", err)
		_ = cacheDirs.cleanupNew()
		return fmt.Errorf("create temporary cache entry for %q: %w", cachePath, err)
	}
	published := false
	defer func() {
		if published {
			return
		}
		if cleanupErr := cacheDirs.removeTemp(tempName); cleanupErr != nil {
			slog.Warn("remove failed temporary registry cache", "cache_path", cachePath, "error", cleanupErr)
		}
		if cleanupErr := cacheDirs.cleanupNew(); cleanupErr != nil {
			slog.Warn("remove failed new registry cache directories", "cache_path", cachePath, "error", cleanupErr)
		}
	}()

	if err := runGitAt(cacheDirs.hash, "clone", url, tempName); err != nil {
		slog.Error("registry cache clone failed", "cache_path", cachePath, "ref", ref, "error", err)
		return err
	}
	tempDir, err := cacheDirs.openEntry(tempName)
	if err != nil {
		slog.Error("registry cache checkout setup failed", "cache_path", cachePath, "ref", ref, "error", err)
		return fmt.Errorf("open temporary cache entry: %w", err)
	}
	if err := checkoutResolvedRefAt(tempDir, ref); err != nil {
		_ = tempDir.Close()
		slog.Error("registry cache checkout failed", "cache_path", cachePath, "ref", ref, "error", err)
		return err
	}
	_ = tempDir.Close()

	if err := cacheDirs.publish(tempName, filepath.Base(cachePath)); err != nil {
		slog.Error("registry cache publication failed", "cache_path", cachePath, "ref", ref, "error", err)
		return fmt.Errorf("publish cache entry: %w", err)
	}
	published = true
	slog.Debug("registry cache published", "cache_path", cachePath, "ref", ref)
	return nil
}

func checkoutResolvedRefAt(repoDir *os.File, ref string) error {
	if !gitRefExistsAt(repoDir, "refs/remotes/origin/"+ref) {
		slog.Debug("checking out direct git ref", "repo_dir", "cache-fd", "ref", ref)
		return runGitAt(repoDir, "checkout", "--force", ref)
	}

	slog.Debug("checking out remote git ref", "repo_dir", "cache-fd", "ref", ref)
	return runGitAt(repoDir, "checkout", "--force", "-B", ref, "origin/"+ref)
}

func gitRefExistsAt(repoDir *os.File, ref string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = repoDir.Name()
	return cmd.Run() == nil
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

func runGitAt(repoDir *os.File, args ...string) error {
	return runGit(repoDir.Name(), args...)
}

const maxGitErrorChars = 200

func truncateGitError(message string) string {
	runes := []rune(message)
	if len(runes) <= maxGitErrorChars {
		return message
	}
	return string(runes[:maxGitErrorChars-1]) + "…"
}
