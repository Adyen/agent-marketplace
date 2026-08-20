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
	if strings.TrimSpace(url) == "" {
		return "", errors.New("registry url is required")
	}
	if err := validateGitRef(ref); err != nil {
		return "", fmt.Errorf("validate registry ref: %w", err)
	}

	cachePath := RegistryCachePath(url, ref)
	cacheExists, err := dirExists(cachePath)
	if err != nil {
		return "", fmt.Errorf("check cache path %q: %w", cachePath, err)
	}

	redactedURL := redactSecrets(url)

	if !cacheExists {
		slog.Debug("registry cache miss; cloning", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		if err := cloneAndPublish(url, ref, cachePath); err != nil {
			return "", fmt.Errorf("clone registry %q into %q: %w", redactedURL, cachePath, err)
		}
	} else {
		slog.Debug("registry cache hit; fetching", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		if err := runGit(cachePath, "fetch", "--tags", "--prune", "origin"); err != nil {
			slog.Warn("using stale cache after fetch failed", "url", redactedURL, "ref", ref, "error", err)
			if checkoutErr := checkoutResolvedRef(cachePath, ref); checkoutErr != nil {
				return "", fmt.Errorf("use stale cache at %q: %w", cachePath, checkoutErr)
			}
			return cachePath, nil
		}

		slog.Debug("checking out registry ref", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		if err := checkoutResolvedRef(cachePath, ref); err != nil {
			return "", fmt.Errorf("checkout %q in %q: %w", ref, cachePath, err)
		}
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
	cacheDir := filepath.Dir(cachePath)
	cacheRoot := CacheDir()
	cacheRootExisted, err := dirExists(cacheRoot)
	if err != nil {
		return fmt.Errorf("check cache root %q: %w", cacheRoot, err)
	}
	cacheDirExisted, err := dirExists(cacheDir)
	if err != nil {
		return fmt.Errorf("check cache parent %q: %w", cacheDir, err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("create cache parent for %q: %w", cachePath, err)
	}

	tempPath, err := os.MkdirTemp(cacheDir, "."+filepath.Base(cachePath)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary cache entry for %q: %w", cachePath, err)
	}
	published := false
	defer func() {
		if published {
			return
		}
		if cleanupErr := os.RemoveAll(tempPath); cleanupErr != nil {
			slog.Warn("remove failed temporary registry cache", "path", tempPath, "error", cleanupErr)
		}
		if !cacheDirExisted {
			_ = os.Remove(cacheDir)
			if !cacheRootExisted {
				_ = os.Remove(cacheRoot)
			}
		}
	}()

	if err := runGit("", "clone", url, tempPath); err != nil {
		return err
	}
	if err := checkoutResolvedRef(tempPath, ref); err != nil {
		return err
	}
	if err := os.Rename(tempPath, cachePath); err != nil {
		return fmt.Errorf("publish cache entry: %w", err)
	}
	published = true
	slog.Debug("registry cache published", "cache_path", cachePath, "ref", ref)
	return nil
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
