// Package acquire fetches approved Git sources into isolated, bounded
// temporary workspaces for the Marketplace indexing worker.
package acquire

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/redact"
)

// Limits cap the size of an acquired repository.
type Limits struct {
	RepositoryBytes int64
	FileCount       int
	FileBytes       int64
}

// Credentials references server-side credential sources. No secret values are
// carried here; only file paths or resolved tokens passed to git subprocesses
// through controlled environment variables.
type Credentials struct {
	SSHKeyFile string
	Token      string
}

// Result is an acquired source workspace.
type Result struct {
	Dir    string
	Commit string

	cleanup func() error
}

// Release removes the temporary workspace.
func (r *Result) Release() error {
	if r != nil && r.cleanup != nil {
		return r.cleanup()
	}
	return nil
}

// Clone fetches origin at ref into an isolated temporary directory, resolves
// the ref to an exact commit, and enforces repository bounds. The caller must
// call Result.Release to clean up the workspace.
func Clone(ctx context.Context, origin, ref string, cred Credentials, limits Limits, timeout time.Duration) (*Result, error) {
	return clone(ctx, origin, ref, "", cred, limits, timeout)
}

// CloneAtCommit fetches origin and checks out exactly commit. It never falls
// back to the ref's current tip; the commit must be present in the clone and
// must remain the checked-out HEAD.
func CloneAtCommit(ctx context.Context, origin, ref, commit string, cred Credentials, limits Limits, timeout time.Duration) (*Result, error) {
	if !isFullCommit(commit) {
		return nil, fmt.Errorf("validate commit: %q is not a full lowercase Git commit", commit)
	}
	return clone(ctx, origin, ref, commit, cred, limits, timeout)
}

func clone(ctx context.Context, origin, ref, exactCommit string, cred Credentials, limits Limits, timeout time.Duration) (*Result, error) {
	if err := cache.ValidateGitURL(origin); err != nil {
		return nil, fmt.Errorf("validate origin: %w", err)
	}
	if err := cache.ValidateGitRef(ref); err != nil {
		return nil, fmt.Errorf("validate ref: %w", err)
	}

	dir, err := os.MkdirTemp("", "marketplace-acquire-")
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	redactedOrigin := redact.URL(origin)
	slog.Debug("acquire clone start", "origin", redactedOrigin, "ref", ref, "dir", dir)

	workspace, err := newWorkspace(cred)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = workspace.cleanup()
			_ = os.RemoveAll(dir)
		}
	}()

	cloneCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		cloneCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	args := append(hardenedGitArgs(), "clone", "--no-checkout", "--", origin, dir)
	if _, err := runGit(cloneCtx, workspace.env(), "", args); err != nil {
		return nil, fmt.Errorf("clone %q: %w", redactedOrigin, err)
	}

	commit := exactCommit
	if commit == "" {
		commit, err = cache.ResolveRemoteRef(dir, ref)
		if err != nil {
			return nil, fmt.Errorf("resolve ref %q in %q: %w", ref, redactedOrigin, err)
		}
	} else {
		if _, err := runGit(cloneCtx, workspace.env(), dir, append(hardenedGitArgs(), "cat-file", "-e", commit+"^{commit}")); err != nil {
			return nil, fmt.Errorf("exact commit %q is unavailable in %q: %w", commit, redactedOrigin, err)
		}
	}

	if err := enforceTreeLimits(cloneCtx, workspace.env(), dir, commit, limits); err != nil {
		return nil, err
	}

	checkoutArgs := append(hardenedGitArgs(), "checkout", "--force", commit)
	if _, err := runGit(cloneCtx, workspace.env(), dir, checkoutArgs); err != nil {
		return nil, fmt.Errorf("checkout %q in %q: %w", commit, redactedOrigin, err)
	}
	if exactCommit != "" {
		head, err := runGit(cloneCtx, workspace.env(), dir, append(hardenedGitArgs(), "rev-parse", "--verify", "HEAD"))
		if err != nil {
			return nil, fmt.Errorf("verify checked-out HEAD for %q in %q: %w", commit, redactedOrigin, err)
		}
		if got := strings.TrimSpace(string(head)); got != commit {
			return nil, fmt.Errorf("checked-out HEAD mismatch in %q: got %q, want %q", redactedOrigin, got, commit)
		}
	}

	if err := enforceLimits(dir, limits); err != nil {
		return nil, err
	}

	cleanup := func() error {
		_ = workspace.cleanup()
		return os.RemoveAll(dir)
	}

	slog.Debug("acquire clone end", "origin", redactedOrigin, "ref", ref, "commit", commit, "dir", dir)
	return &Result{Dir: dir, Commit: commit, cleanup: cleanup}, nil
}

func hardenedGitArgs() []string {
	return []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.remote.allow=never",
		"-c", "protocol.file.allow=always",
		"-c", "submodule.recurse=false",
		"-c", "credential.helper=",
	}
}

func runGit(ctx context.Context, env []string, dir string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		redacted := redact.URL(string(out))
		redactedArgs := redact.Strings(args)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: context cancelled", strings.Join(redactedArgs, " "))
		}
		cmdStr := strings.Join(redactedArgs, " ")
		if redacted == "" {
			return nil, fmt.Errorf("git %s: %w", cmdStr, err)
		}
		return nil, fmt.Errorf("git %s: %w: %s", cmdStr, err, truncate(redacted))
	}
	return out, nil
}

type workspace struct {
	askpassDir string
	envSlice   []string
}

func newWorkspace(cred Credentials) (*workspace, error) {
	askpassDir := ""
	if strings.TrimSpace(cred.Token) != "" {
		var err error
		askpassDir, err = os.MkdirTemp("", "marketplace-askpass-")
		if err != nil {
			return nil, fmt.Errorf("create askpass dir: %w", err)
		}
	}
	env, err := gitEnv(cred, askpassDir)
	if err != nil {
		_ = os.RemoveAll(askpassDir)
		return nil, err
	}
	return &workspace{envSlice: env, askpassDir: askpassDir}, nil
}

func (w *workspace) env() []string { return w.envSlice }

func (w *workspace) cleanup() error {
	if w.askpassDir != "" {
		_ = os.RemoveAll(w.askpassDir)
	}
	return nil
}

func gitEnv(cred Credentials, askpassDir string) ([]string, error) {
	base := cache.GitEnv()
	env := make(map[string]string, len(base))
	for _, e := range base {
		key, value, found := strings.Cut(e, "=")
		if !found {
			key = e
			value = ""
		}
		env[key] = value
	}

	if strings.TrimSpace(cred.SSHKeyFile) != "" {
		ssh := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o BatchMode=yes", shQuote(cred.SSHKeyFile))
		env["GIT_SSH_COMMAND"] = ssh
	}

	if strings.TrimSpace(cred.Token) != "" {
		if askpassDir == "" {
			var err error
			askpassDir, err = os.MkdirTemp("", "marketplace-askpass-")
			if err != nil {
				return nil, fmt.Errorf("create askpass dir: %w", err)
			}
		}
		askpass, err := writeAskpass(askpassDir)
		if err != nil {
			return nil, err
		}
		env["GIT_ASKPASS"] = askpass
		env["AGENT_MARKETPLACE_GIT_TOKEN"] = cred.Token
	}

	// Do not download LFS objects during clone/checkout; pointer files are kept
	// as small blobs and evaluated later.
	env["GIT_LFS_SKIP_SMUDGE"] = "1"

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out, nil
}

func writeAskpass(dir string) (string, error) {
	path := filepath.Join(dir, "askpass"+scriptExt())
	script := "#!/bin/sh\nprintf '%s\\n' \"$AGENT_MARKETPLACE_GIT_TOKEN\"\n"
	if runtime.GOOS == "windows" {
		script = "@echo off\r\necho %AGENT_MARKETPLACE_GIT_TOKEN%\r\n"
	}
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write askpass script: %w", err)
	}
	return path, nil
}

func scriptExt() string {
	if runtime.GOOS == "windows" {
		return ".bat"
	}
	return ".sh"
}

func enforceTreeLimits(ctx context.Context, env []string, repoDir, commit string, limits Limits) error {
	args := append(hardenedGitArgs(), "ls-tree", "-r", "-l", commit)
	out, err := runGit(ctx, env, repoDir, args)
	if err != nil {
		return fmt.Errorf("list tree for %q: %w", commit, err)
	}

	var total int64
	var count int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		meta, path, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 4 {
			continue
		}
		mode, typ, sizeStr := fields[0], fields[1], fields[3]
		if typ != "blob" {
			// Reject submodules and other non-blob tree entries.
			return fmt.Errorf("%w: tree contains non-blob entry %q (type %s)", ErrLimitExceeded, path, typ)
		}
		if mode == "120000" {
			return fmt.Errorf("%w: tree contains symlink %q", ErrLimitExceeded, path)
		}
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			return fmt.Errorf("parse tree size for %q: %w", path, err)
		}
		if limits.FileBytes > 0 && size > limits.FileBytes {
			return fmt.Errorf("%w: tree entry %q size %d exceeds file_bytes limit %d", ErrLimitExceeded, path, size, limits.FileBytes)
		}
		total += size
		count++
		if limits.RepositoryBytes > 0 && total > limits.RepositoryBytes {
			return fmt.Errorf("%w: tree size %d exceeds repository_bytes limit %d", ErrLimitExceeded, total, limits.RepositoryBytes)
		}
		if limits.FileCount > 0 && count > limits.FileCount {
			return fmt.Errorf("%w: tree entry count %d exceeds file_count limit %d", ErrLimitExceeded, count, limits.FileCount)
		}
	}
	return nil
}

func enforceLimits(dir string, limits Limits) error {
	var total int64
	var count int
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return fs.SkipDir
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("%w: path escapes workspace: %q", ErrLimitExceeded, rel)
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlinks are not allowed: %q", ErrLimitExceeded, rel)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%w: non-regular files are not allowed: %q", ErrLimitExceeded, rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sz := info.Size()
		if limits.FileBytes > 0 && sz > limits.FileBytes {
			return fmt.Errorf("%w: file %q size %d exceeds file_bytes limit %d", ErrLimitExceeded, rel, sz, limits.FileBytes)
		}
		total += sz
		count++
		if limits.RepositoryBytes > 0 && total > limits.RepositoryBytes {
			return fmt.Errorf("%w: workspace size %d exceeds repository_bytes limit %d", ErrLimitExceeded, total, limits.RepositoryBytes)
		}
		if limits.FileCount > 0 && count > limits.FileCount {
			return fmt.Errorf("%w: workspace file count %d exceeds file_count limit %d", ErrLimitExceeded, count, limits.FileCount)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("enforce repository limits: %w", err)
	}
	return nil
}

func isFullCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for i := range value {
		if (value[i] < '0' || value[i] > '9') && (value[i] < 'a' || value[i] > 'f') {
			return false
		}
	}
	return true
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func truncate(s string) string {
	const max = 200
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// Errors for callers to detect.
var (
	// ErrLimitExceeded is returned when a repository exceeds configured bounds.
	ErrLimitExceeded = errors.New("acquire: repository limit exceeded")
)
