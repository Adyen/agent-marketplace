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

	args := append(hardenedGitArgs(), "clone", "--", origin, dir)
	if _, err := runGit(cloneCtx, workspace.env(), "", args); err != nil {
		return nil, fmt.Errorf("clone %q: %w", redactedOrigin, err)
	}

	commit, err := cache.ResolveRemoteRef(dir, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve ref %q in %q: %w", ref, redactedOrigin, err)
	}

	checkoutArgs := append(hardenedGitArgs(), "checkout", "--force", commit)
	if _, err := runGit(cloneCtx, workspace.env(), dir, checkoutArgs); err != nil {
		return nil, fmt.Errorf("checkout %q in %q: %w", commit, redactedOrigin, err)
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
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.remote.allow=never",
		"-c", "protocol.file.allow=always",
	}
}

func runGit(ctx context.Context, env []string, dir string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		redacted := redactSecrets(string(out))
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: context cancelled", redactArgs(args))
		}
		cmdStr := strings.Join(redactArgs(args), " ")
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
		askpass, err := writeAskpass(askpassDir, cred.Token)
		if err != nil {
			return nil, err
		}
		env["GIT_ASKPASS"] = askpass
		env["AGENT_MARKETPLACE_GIT_TOKEN"] = cred.Token
	}

	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out, nil
}

func writeAskpass(dir, token string) (string, error) {
	path := filepath.Join(dir, "askpass"+scriptExt())
	script := "#!/bin/sh\nprintf '%s\\n' \"$AGENT_MARKETPLACE_GIT_TOKEN\"\n"
	if runtime.GOOS == "windows" {
		script = "@echo off\r\necho %AGENT_MARKETPLACE_GIT_TOKEN%\r\n"
	}
	_ = token
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

func enforceLimits(dir string, limits Limits) error {
	var total int64
	var count int
	maxFile := int64(0)
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
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sz := info.Size()
		if limits.FileBytes > 0 && sz > limits.FileBytes {
			return fmt.Errorf("file %q size %d exceeds file_bytes limit %d", rel, sz, limits.FileBytes)
		}
		total += sz
		count++
		if sz > maxFile {
			maxFile = sz
		}
		if limits.RepositoryBytes > 0 && total > limits.RepositoryBytes {
			return fmt.Errorf("workspace size %d exceeds repository_bytes limit %d", total, limits.RepositoryBytes)
		}
		if limits.FileCount > 0 && count > limits.FileCount {
			return fmt.Errorf("workspace file count %d exceeds file_count limit %d", count, limits.FileCount)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("enforce repository limits: %w", err)
	}
	return nil
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func redactSecrets(s string) string {
	return redact.URL(s)
}

func redactArgs(args []string) []string {
	return redact.Strings(args)
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
