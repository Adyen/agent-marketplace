package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// refNameRE matches a conservative set of safe git ref-name characters.
var refNameRE = regexp.MustCompile(`^[A-Za-z0-9._+/@-]+$`)

func CloneOrPull(url, ref string) (string, error) {
	return WithCacheMutationLock(func() (string, error) {
		return cloneOrPull(url, ref)
	})
}

func cloneOrPull(url, ref string) (string, error) {
	if err := validateGitURL(url); err != nil {
		return "", fmt.Errorf("validate registry url: %w", err)
	}
	if err := validateGitRef(ref); err != nil {
		return "", fmt.Errorf("validate registry ref: %w", err)
	}

	cachePath := RegistryCachePath(url, ref)
	cacheExists, err := validateRegistryCachePath(cachePath)
	if err != nil {
		return "", fmt.Errorf("check cache path %q: %w", cachePath, err)
	}

	redactedURL := redactSecrets(url)

	if !cacheExists {
		slog.Debug("registry cache miss; cloning", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		if _, err := validateRegistryCachePath(filepath.Dir(cachePath)); err != nil {
			return "", fmt.Errorf("validate cache parent for %q: %w", cachePath, err)
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
			return "", fmt.Errorf("create cache parent for %q: %w", cachePath, err)
		}
		if _, err := validateRegistryCachePath(cachePath); err != nil {
			return "", fmt.Errorf("validate cache path before clone %q: %w", cachePath, err)
		}

		if err := runGit("", "clone", url, cachePath); err != nil {
			return "", fmt.Errorf("clone registry %q into %q: %w", redactedURL, cachePath, err)
		}
		cacheExists, err = validateRegistryCachePath(cachePath)
		if err != nil {
			return "", fmt.Errorf("validate cloned cache path %q: %w", cachePath, err)
		}
		if !cacheExists {
			return "", fmt.Errorf("clone registry %q did not create cache path %q", redactedURL, cachePath)
		}
	} else {
		slog.Debug("registry cache hit; fetching", "url", redactedURL, "ref", ref, "cache_path", cachePath)
		if _, err := validateRegistryCachePath(cachePath); err != nil {
			return "", fmt.Errorf("validate cache path before fetch %q: %w", cachePath, err)
		}
		if err := runGit(cachePath, "fetch"); err != nil {
			slog.Warn("using stale cache after fetch failed", "url", redactedURL, "ref", ref, "error", err)
			if _, validateErr := validateRegistryCachePath(cachePath); validateErr != nil {
				return "", fmt.Errorf("validate stale cache at %q: %w", cachePath, validateErr)
			}
			if checkoutErr := checkoutResolvedRef(cachePath, ref); checkoutErr != nil {
				return "", fmt.Errorf("use stale cache at %q: %w", cachePath, checkoutErr)
			}
			return cachePath, nil
		}
	}

	if _, err := validateRegistryCachePath(cachePath); err != nil {
		return "", fmt.Errorf("validate cache path before checkout %q: %w", cachePath, err)
	}
	slog.Debug("checking out registry ref", "url", redactedURL, "ref", ref, "cache_path", cachePath)
	if err := checkoutResolvedRef(cachePath, ref); err != nil {
		return "", fmt.Errorf("checkout %q in %q: %w", ref, cachePath, err)
	}

	return cachePath, nil
}

// DefaultBranch returns the branch advertised by a remote repository's HEAD.
func DefaultBranch(url string) (string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return "", errors.New("registry url is required")
	}

	output, err := runGitOutput("", "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return "", fmt.Errorf("determine default branch for registry %q: %w", redactSecrets(url), err)
	}

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "ref:" || fields[2] != "HEAD" {
			continue
		}
		const prefix = "refs/heads/"
		if !strings.HasPrefix(fields[1], prefix) || len(fields[1]) == len(prefix) {
			continue
		}
		return strings.TrimPrefix(fields[1], prefix), nil
	}

	return "", fmt.Errorf("remote did not advertise a default branch")
}

func checkoutResolvedRef(repoDir, ref string) error {
	slog.Debug("checking out resolved registry ref", "repo_dir", repoDir, "ref", ref)
	return runGit(repoDir, "checkout", ref)
}

// validateRegistryCachePath rejects symlinked cache roots, ancestors, and
// existing paths before git or directory operations use them. The checks are
// intentionally best-effort process-local guards, not cross-process or
// openat-style atomic guarantees.
func validateRegistryCachePath(path string) (bool, error) {
	cacheRoot := CacheDir()
	cacheBoundary := cacheRoot
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		// Validate through HOME so a symlinked HOME/.cache ancestor cannot
		// redirect registry acquisition into an external tree.
		cacheBoundary = homeDir
	}
	if err := ValidateDirectoryAncestry(cacheBoundary, cacheRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("validate cache root %q: %w", cacheRoot, err)
	}
	if err := ValidateDirectoryAncestry(cacheRoot, path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("cache path %q is a symlink", path)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("cache path %q is not a directory", path)
	}
	return true, nil
}

func runGit(repoDir string, args ...string) error {
	_, err := runGitOutput(repoDir, args...)
	return err
}

func runGitOutput(repoDir string, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("runGit: no git command")
	}

	start := time.Now()
	safeArgs, err := prepareGitArgs(repoDir, args)
	if err != nil {
		return "", err
	}
	return runGitRawOutput(repoDir, safeArgs, start)
}

func prepareGitArgs(repoDir string, args []string) ([]string, error) {
	op := args[0]

	var safeArgs []string
	switch op {
	case "clone":
		if len(args) != 3 {
			return nil, fmt.Errorf("runGit: clone requires url and dir, got %d args", len(args))
		}
		url, dir := args[1], args[2]
		if err := validateGitURL(url); err != nil {
			return nil, err
		}
		safeArgs = []string{"clone", "--", url, dir}
	case "fetch":
		if len(args) != 1 {
			return nil, fmt.Errorf("runGit: fetch takes no arguments, got %d args", len(args))
		}
		safeArgs = []string{"fetch", "--tags", "--prune", "--", "origin"}
	case "checkout":
		var ref string
		switch {
		case len(args) == 2:
			ref = args[1]
		case len(args) == 3 && args[1] == "--force":
			ref = args[2]
		default:
			// Unknown checkout invocation; pass through raw (used by benchmarks).
			return args, nil
		}
		if err := validateGitRef(ref); err != nil {
			return nil, err
		}
		commit, err := resolveRemoteRef(repoDir, ref)
		if err != nil {
			return nil, err
		}
		if err := validateGitRef(commit); err != nil {
			return nil, err
		}
		safeArgs = []string{"checkout", "--force", commit}
	case "ls-remote":
		if len(args) != 4 || args[1] != "--symref" || args[3] != "HEAD" {
			return nil, fmt.Errorf("runGit: unsupported ls-remote arguments")
		}
		if err := validateGitURL(args[2]); err != nil {
			return nil, err
		}
		safeArgs = []string{"ls-remote", "--symref", "--", args[2], "HEAD"}
	default:
		// Other git subcommands used by benchmark setup; still run without a shell
		// and with the hardened git environment.
		return args, nil
	}

	return safeArgs, nil
}

// runGitRaw executes the already-sanitised git arguments with the hardened
// environment and logging used by runGit.
func runGitRaw(repoDir string, safeArgs []string, start time.Time) error {
	_, err := runGitRawOutput(repoDir, safeArgs, start)
	return err
}

func runGitRawOutput(repoDir string, safeArgs []string, start time.Time) (string, error) {
	redactedSafe := redactArgs(safeArgs)
	slog.Debug("git command start", "dir", repoDir, "args", redactedSafe)

	output, err := execGit(repoDir, safeArgs)

	if err == nil {
		slog.Debug("git command end", "dir", repoDir, "args", redactedSafe, "duration_ms", time.Since(start).Milliseconds(), "success", true)
		return string(output), nil
	}

	trimmedOutput := redactSecrets(strings.TrimSpace(string(output)))
	slog.Debug("git command end", "dir", repoDir, "args", redactedSafe, "duration_ms", time.Since(start).Milliseconds(), "success", false, "stderr", trimmedOutput)

	redactedCmd := strings.Join(redactedSafe, " ")
	if trimmedOutput == "" {
		return "", fmt.Errorf("git %s: %w", redactedCmd, err)
	}

	// Bound the error message to prevent screen flooding. Git can emit
	// multi-page stacktraces and verbose diagnostics; keep only the first
	// line for the user-facing error, preserving full detail in the log.
	firstLine := trimmedOutput
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = truncateGitError(firstLine)
	return "", fmt.Errorf("git %s: %w: %s", redactedCmd, err, firstLine)
}

// execGit runs git in repoDir with the minimal allowlisted environment and
// hardened protocol allowlist.
func execGit(repoDir string, args []string) ([]byte, error) {
	return execGitContext(context.Background(), repoDir, args)
}

// execGitContext runs git with cancellation while retaining the protocol and
// environment hardening shared by registry and exact-snapshot operations.
func execGitContext(ctx context.Context, repoDir string, args []string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	hardened := []string{
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.remote.allow=never",
		"-c", "protocol.file.allow=always",
	}
	cmd := exec.CommandContext(ctx, "git", append(hardened, args...)...)
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	cmd.Env = gitEnv()
	return cmd.CombinedOutput()
}

// resolveRemoteRef resolves a validated ref to a commit hash in the repository.
// It tries the remote tracking branch first, then tags, then a raw ref.
func resolveRemoteRef(repoDir, ref string) (string, error) {
	if err := validateGitRef(ref); err != nil {
		return "", err
	}

	candidates := []string{
		fmt.Sprintf("origin/%s", ref),
		fmt.Sprintf("refs/tags/%s^{commit}", ref),
		fmt.Sprintf("%s^{commit}", ref),
	}

	var lastErr error
	var lastOutput []byte
	for _, candidate := range candidates {
		slog.Debug("resolving remote ref", "repo_dir", repoDir, "candidate", candidate)
		output, err := execGit(repoDir, []string{"rev-parse", "--verify", candidate})
		if err == nil {
			return strings.TrimSpace(string(output)), nil
		}
		lastErr = err
		lastOutput = output
	}

	trimmed := redactSecrets(strings.TrimSpace(string(lastOutput)))
	if trimmed == "" {
		return "", fmt.Errorf("resolve ref %q: %w", ref, lastErr)
	}
	firstLine := trimmed
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = truncateGitError(firstLine)
	return "", fmt.Errorf("resolve ref %q: %w: %s", ref, lastErr, firstLine)
}

// gitEnv returns a minimal allowlisted environment for git subprocesses.
// It drops all GIT_* variables except an explicit allowlist, then sets the
// safe overrides required by the Git acquisition hardening policy.
func gitEnv() []string {
	allowedGIT := map[string]bool{
		"GIT_SSH":         true,
		"GIT_SSH_COMMAND": true,
		"GIT_SSL_CERT":    true,
		"GIT_SSL_KEY":     true,
		"GIT_HTTP_PROXY":  true,
		"GIT_HTTPS_PROXY": true,
		"GIT_NO_PROXY":    true,
	}

	var env []string
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(key, "GIT_") && !allowedGIT[key] {
			continue
		}
		env = append(env, e)
	}

	env = append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=echo",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	return env
}

// validateGitURL rejects option-like URLs, NUL/vertical-tab characters, and
// unsupported or dangerous transport protocols while allowing the protocols
// used by the project (ssh, https, http, file) and local filesystem paths.
func validateGitURL(raw string) error {
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x20 || raw[i] == 0x7f {
			return errors.New("invalid git URL: control character")
		}
	}

	u := strings.TrimSpace(raw)
	if u == "" {
		return errors.New("registry url is required")
	}
	if strings.HasPrefix(u, "-") {
		return errors.New("invalid git URL: option-like")
	}
	if strings.Contains(u, "..") {
		return errors.New("invalid git URL: path traversal")
	}

	for i := 0; i < len(u); i++ {
		if u[i] < 0x20 || u[i] == 0x7f {
			return errors.New("invalid git URL: control character")
		}
	}

	lower := strings.ToLower(u)
	if strings.Contains(lower, "ext::") || strings.Contains(lower, "remote::") || strings.Contains(lower, "helper::") {
		return errors.New("invalid git URL: unsupported transport helper")
	}

	if scheme, rest, ok := strings.Cut(u, "://"); ok {
		scheme = strings.ToLower(scheme)
		switch scheme {
		case "https", "http", "ssh", "file":
			// Allowed protocols.
		default:
			if scheme == "git" {
				return errors.New("invalid git URL: git:// protocol is not allowed")
			}
			return fmt.Errorf("invalid git URL: unsupported protocol %q", scheme)
		}

		// Reject hosts that start with '-', which can be interpreted as ssh/curl
		// command-line options (e.g. ssh://-oProxyCommand=...).
		if scheme != "file" {
			auth := rest
			if i := strings.IndexByte(auth, '/'); i >= 0 {
				auth = auth[:i]
			}
			if i := strings.LastIndexByte(auth, '@'); i >= 0 {
				auth = auth[i+1:]
			}
			if i := strings.LastIndexByte(auth, ':'); i > 0 {
				auth = auth[:i]
			}
			if auth == "" || strings.HasPrefix(auth, "-") {
				return errors.New("invalid git URL: host starts with '-'")
			}
		}
		return nil
	}

	if strings.Contains(lower, "::") {
		return errors.New("invalid git URL: unsupported transport helper")
	}

	if at := strings.IndexByte(u, '@'); at >= 0 {
		host := u[at+1:]
		var path string
		switch colon := strings.IndexByte(host, ':'); colon {
		case -1:
			if slash := strings.IndexByte(host, '/'); slash >= 0 {
				host = host[:slash]
			}
		default:
			path = host[colon+1:]
			host = host[:colon]
		}
		if host == "" || strings.HasPrefix(host, "-") {
			return errors.New("invalid git URL: host starts with '-'")
		}
		if strings.HasPrefix(path, "-") {
			return errors.New("invalid git URL: path starts with '-'")
		}
		return nil
	}

	if strings.HasPrefix(u, "~") {
		return errors.New("invalid git URL: tilde path is not allowed")
	}

	return nil
}

// validateGitRef rejects option-like refs, path traversal patterns, control
// characters, and git extended syntax that could alter rev-parse behaviour.
func validateGitRef(raw string) error {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return errors.New("registry ref is required")
	}
	if strings.HasPrefix(ref, "-") {
		return errors.New("invalid git ref: option-like")
	}
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") {
		return errors.New("invalid git ref: absolute or tilde path")
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") {
		return errors.New("invalid git ref: contains '..' or '@{'")
	}

	for i := 0; i < len(ref); i++ {
		if ref[i] < 0x20 || ref[i] == 0x7f {
			return errors.New("invalid git ref: control character")
		}
	}

	if !refNameRE.MatchString(ref) {
		return errors.New("invalid git ref: invalid character")
	}

	if strings.HasSuffix(ref, ".") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".lock") {
		return errors.New("invalid git ref: invalid suffix")
	}

	return nil
}

const maxGitErrorChars = 200

func truncateGitError(message string) string {
	runes := []rune(message)
	if len(runes) <= maxGitErrorChars {
		return message
	}
	return string(runes[:maxGitErrorChars-1]) + "…"
}

// ValidateGitURL wraps validateGitURL for reuse by Marketplace acquisition.
func ValidateGitURL(raw string) error { return validateGitURL(raw) }

// ValidateGitRef wraps validateGitRef for reuse by Marketplace acquisition.
func ValidateGitRef(raw string) error { return validateGitRef(raw) }

// ResolveRemoteRef wraps resolveRemoteRef for reuse by Marketplace acquisition.
func ResolveRemoteRef(repoDir, ref string) (string, error) { return resolveRemoteRef(repoDir, ref) }

// GitEnv returns the hardened environment used for git subprocesses.
func GitEnv() []string { return gitEnv() }
