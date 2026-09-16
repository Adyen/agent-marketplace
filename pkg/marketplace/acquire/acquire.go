// Package acquire fetches approved Git registries into isolated, bounded
// temporary workspaces for the Marketplace indexing worker.
package acquire

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
	CAFile     string
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
func Clone(ctx context.Context, origin, ref string, cred Credentials, limits Limits, timeout time.Duration, roots ...string) (*Result, error) {
	return clone(ctx, origin, ref, "", roots, cred, limits, timeout)
}

// CloneAtCommit fetches origin and checks out exactly commit. It never falls
// back to the ref's current tip; the commit must be present in the clone and
// must remain the checked-out HEAD.
func CloneAtCommit(ctx context.Context, origin, ref, commit string, cred Credentials, limits Limits, timeout time.Duration, roots ...string) (*Result, error) {
	if !isFullCommit(commit) {
		return nil, fmt.Errorf("validate commit: %q is not a full lowercase Git commit", commit)
	}
	return clone(ctx, origin, ref, commit, roots, cred, limits, timeout)
}

func clone(ctx context.Context, origin, ref, exactCommit string, roots []string, cred Credentials, limits Limits, timeout time.Duration) (*Result, error) {
	if err := cache.ValidateGitURL(origin); err != nil {
		return nil, fmt.Errorf("validate origin: %w", err)
	}
	if err := cache.ValidateGitRef(ref); err != nil {
		return nil, fmt.Errorf("validate ref: %w", err)
	}
	roots, err := normalizeRoots(roots)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "marketplace-acquire-")
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	redactedOrigin := redact.URL(origin)
	slog.Debug("acquire clone start", "origin", redactedOrigin, "ref", ref, "dir", dir)

	workspace, err := newWorkspace(cred, origin)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	keepWorkspace := false
	defer func() {
		if !keepWorkspace {
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

	if _, err := runGit(cloneCtx, workspace.env(), "", append(hardenedGitArgs(), "init", "--", dir)); err != nil {
		return nil, fmt.Errorf("initialize clone workspace for %q: %w", redactedOrigin, err)
	}
	if _, err := runGit(cloneCtx, workspace.env(), dir, append(hardenedGitArgs(), "remote", "add", "origin", origin)); err != nil {
		return nil, fmt.Errorf("configure clone origin %q: %w", redactedOrigin, err)
	}
	fetchOutput, err := runGit(cloneCtx, workspace.env(), dir, fetchArgs(ref, exactCommit == ""))
	if err != nil {
		if exactCommit != "" || !strings.Contains(err.Error(), "dumb http transport does not support shallow capabilities") {
			return nil, fmt.Errorf("clone %q: %w", redactedOrigin, err)
		}
		slog.Info("acquire shallow fetch unsupported; retrying without depth", "origin", redactedOrigin)
		fetchOutput, err = runGit(cloneCtx, workspace.env(), dir, fetchArgs(ref, false))
		if err != nil {
			return nil, fmt.Errorf("clone %q: %w", redactedOrigin, err)
		}
	}
	if strings.Contains(string(fetchOutput), "filtering not recognized by server, ignoring") {
		slog.Warn("acquire server ignored partial clone filter", "origin", redactedOrigin)
	}

	commit := exactCommit
	if commit == "" {
		out, err := runGit(cloneCtx, workspace.env(), dir, append(hardenedGitArgs(), "rev-parse", "--verify", "FETCH_HEAD^{commit}"))
		if err != nil {
			return nil, fmt.Errorf("resolve ref %q in %q: %w", ref, redactedOrigin, err)
		}
		commit = strings.TrimSpace(string(out))
	} else {
		if _, err := runGit(cloneCtx, workspace.env(), dir, append(hardenedGitArgs(), "cat-file", "-e", commit+"^{commit}")); err != nil {
			return nil, fmt.Errorf("exact commit %q is unavailable in %q: %w", commit, redactedOrigin, err)
		}
	}

	if len(roots) > 0 {
		patterns := make([]string, len(roots))
		for i, root := range roots {
			patterns[i] = "/" + escapeSparsePattern(root) + "/"
		}
		sparseArgs := append(hardenedGitArgs(), "sparse-checkout", "set", "--no-cone", "--stdin")
		if _, err := runGitInput(cloneCtx, workspace.env(), dir, sparseArgs, strings.Join(patterns, "\n")+"\n"); err != nil {
			return nil, fmt.Errorf("configure sparse checkout in %q: %w", redactedOrigin, err)
		}
		slog.Debug("acquire sparse checkout configured", "origin", redactedOrigin, "roots", roots)
	}

	if err := enforceTreeLimits(cloneCtx, workspace.env(), dir, commit, roots, limits); err != nil {
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

	if err := dereferenceRepositorySymlinks(cloneCtx, dir, limits); err != nil {
		return nil, err
	}
	if err := enforceLimits(cloneCtx, dir, limits); err != nil {
		return nil, err
	}

	cleanup := func() error {
		_ = workspace.cleanup()
		return os.RemoveAll(dir)
	}

	slog.Debug("acquire clone end", "origin", redactedOrigin, "ref", ref, "commit", commit, "dir", dir)
	keepWorkspace = true
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

func fetchArgs(ref string, shallow bool) []string {
	args := append(hardenedGitArgs(), "fetch", "--filter=blob:none", "--no-tags")
	if shallow {
		args = append(args, "--depth=1")
	}
	ref = strings.TrimPrefix(ref, "refs/remotes/origin/")
	ref = strings.TrimPrefix(ref, "origin/")
	return append(args, "origin", ref)
}

func normalizeRoots(roots []string) ([]string, error) {
	out := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		valid := root != "" && root == strings.TrimSpace(root)
		for i := 0; valid && i < len(root); i++ {
			valid = root[i] >= 0x20 && root[i] != 0x7f
		}
		root = filepath.Clean(root)
		if root == "." && valid {
			return nil, nil
		}
		if !valid || filepath.IsAbs(root) || root == ".." || strings.HasPrefix(root, ".."+string(filepath.Separator)) || strings.HasPrefix(root, "-") {
			return nil, fmt.Errorf("validate root: %q must be a relative repository path", root)
		}
		root = filepath.ToSlash(root)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		out = append(out, root)
	}
	return out, nil
}

func escapeSparsePattern(path string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[").Replace(path)
}

func runGit(ctx context.Context, env []string, dir string, args []string) ([]byte, error) {
	return runGitInput(ctx, env, dir, args, "")
}

func runGitInput(ctx context.Context, env []string, dir string, args []string, input string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		redacted := redact.URL(string(out))
		redactedArgs := redact.Strings(args)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(redactedArgs, " "), ctx.Err())
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
	tempDir  string
	envSlice []string
}

func newWorkspace(cred Credentials, origin string) (*workspace, error) {
	tempDir := ""
	if strings.TrimSpace(cred.CAFile) != "" {
		var err error
		tempDir, err = os.MkdirTemp("", "marketplace-git-")
		if err != nil {
			return nil, fmt.Errorf("create git workspace: %w", err)
		}
	}
	if strings.TrimSpace(cred.CAFile) != "" {
		data, err := os.ReadFile(strings.TrimSpace(cred.CAFile))
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("read source CA file: %w", err)
		}
		cred.CAFile = filepath.Join(tempDir, "ca.pem")
		if err := os.WriteFile(cred.CAFile, data, 0o600); err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("copy source CA file: %w", err)
		}
	}
	env, err := gitEnv(cred, origin)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, err
	}
	return &workspace{envSlice: env, tempDir: tempDir}, nil
}

func (w *workspace) env() []string { return w.envSlice }

func (w *workspace) cleanup() error {
	if w.tempDir != "" {
		_ = os.RemoveAll(w.tempDir)
	}
	return nil
}

func gitEnv(cred Credentials, origin string) ([]string, error) {
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
	delete(env, "GIT_ASKPASS")
	delete(env, "GIT_SSL_CAINFO")
	delete(env, "GIT_SSL_CAPATH")

	if strings.TrimSpace(cred.SSHKeyFile) != "" {
		ssh := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o BatchMode=yes", shQuote(cred.SSHKeyFile))
		env["GIT_SSH_COMMAND"] = ssh
	}
	if strings.TrimSpace(cred.CAFile) != "" {
		env["GIT_SSL_CAINFO"] = strings.TrimSpace(cred.CAFile)
	}

	token := strings.TrimSpace(cred.Token)
	if token != "" {
		env["GIT_CONFIG_COUNT"] = "1"
		env["GIT_CONFIG_KEY_0"] = "http." + origin + ".extraHeader"
		env["GIT_CONFIG_VALUE_0"] = "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(token+":"+token))
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

func enforceTreeLimits(ctx context.Context, env []string, repoDir, commit string, roots []string, limits Limits) error {
	args := append(hardenedGitArgs(), "ls-tree", "-r", commit)
	if len(roots) > 0 {
		args = append(args, "--")
		args = append(args, roots...)
	}
	out, err := runGit(ctx, env, repoDir, args)
	if err != nil {
		return fmt.Errorf("list tree for %q: %w", commit, err)
	}

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
		if len(fields) < 3 {
			continue
		}
		typ := fields[1]
		if typ != "blob" {
			// Reject submodules and other non-blob tree entries.
			return fmt.Errorf("%w: tree contains non-blob entry %q (type %s)", ErrLimitExceeded, path, typ)
		}
		count++
		if limits.FileCount > 0 && count > limits.FileCount {
			return fmt.Errorf("%w: tree entry count %d exceeds file_count limit %d", ErrLimitExceeded, count, limits.FileCount)
		}
	}
	return nil
}

type repositoryBudget struct {
	limits Limits
	total  int64
	count  int
}

func (b *repositoryBudget) add(path string, size int64) error {
	if b.limits.FileBytes > 0 && size > b.limits.FileBytes {
		return fmt.Errorf("%w: file %q size %d exceeds file_bytes limit %d", ErrLimitExceeded, path, size, b.limits.FileBytes)
	}
	b.total += size
	b.count++
	if b.limits.RepositoryBytes > 0 && b.total > b.limits.RepositoryBytes {
		return fmt.Errorf("%w: workspace size %d exceeds repository_bytes limit %d", ErrLimitExceeded, b.total, b.limits.RepositoryBytes)
	}
	if b.limits.FileCount > 0 && b.count > b.limits.FileCount {
		return fmt.Errorf("%w: workspace file count %d exceeds file_count limit %d", ErrLimitExceeded, b.count, b.limits.FileCount)
	}
	return nil
}

func dereferenceRepositorySymlinks(ctx context.Context, dir string, limits Limits) error {
	budget, err := scanRepository(ctx, dir, limits, false)
	if err != nil {
		return fmt.Errorf("prepare symlink expansion: %w", err)
	}
	var links []string
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return fs.SkipDir
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links = append(links, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("find repository symlinks: %w", err)
	}
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(link)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if err := materializeRepositorySymlink(ctx, dir, link, budget, make(map[string]bool)); err != nil {
			rel, _ := filepath.Rel(dir, link)
			return fmt.Errorf("dereference symlink %q: %w", rel, err)
		}
	}
	return nil
}

func materializeRepositorySymlink(ctx context.Context, root, link string, budget *repositoryBudget, active map[string]bool) error {
	stageDir, err := os.MkdirTemp(filepath.Dir(link), ".agent-marketplace-symlink-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stageDir) }()
	stage := filepath.Join(stageDir, "value")
	if err := copyRepositorySymlink(ctx, root, link, stage, budget, active); err != nil {
		return err
	}
	if err := os.Remove(link); err != nil {
		return err
	}
	return os.Rename(stage, link)
}

func copyRepositorySymlink(ctx context.Context, root, link, dst string, budget *repositoryBudget, active map[string]bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	link = filepath.Clean(link)
	if active[link] {
		return fmt.Errorf("symlink cycle at %q", repositoryRelative(root, link))
	}
	active[link] = true
	defer delete(active, link)

	target, info, err := resolveRepositoryPath(ctx, root, link)
	if err != nil {
		return err
	}
	if info.IsDir() && pathContains(target, link) {
		return fmt.Errorf("symlink cycle from %q to ancestor %q", repositoryRelative(root, link), repositoryRelative(root, target))
	}
	return copyRepositoryNode(ctx, root, target, dst, budget, active)
}

func resolveRepositoryPath(ctx context.Context, root, path string) (string, fs.FileInfo, error) {
	current := filepath.Clean(path)
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		rel, err := repositoryPath(root, current)
		if err != nil {
			return "", nil, err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		probe := root
		followed := false
		for i, part := range parts {
			if part == "." {
				continue
			}
			probe = filepath.Join(probe, part)
			info, err := os.Lstat(probe)
			if errors.Is(err, os.ErrNotExist) {
				return "", nil, fmt.Errorf("broken symlink target %q", repositoryRelative(root, current))
			}
			if err != nil {
				return "", nil, err
			}
			if info.Mode()&os.ModeSymlink == 0 {
				if !info.IsDir() && i < len(parts)-1 {
					return "", nil, fmt.Errorf("broken symlink target %q", repositoryRelative(root, current))
				}
				continue
			}
			if seen[probe] {
				return "", nil, fmt.Errorf("symlink cycle at %q", repositoryRelative(root, probe))
			}
			seen[probe] = true
			target, err := os.Readlink(probe)
			if err != nil {
				return "", nil, err
			}
			if filepath.IsAbs(target) {
				return "", nil, fmt.Errorf("absolute symlink target %q", target)
			}
			remaining := filepath.Join(parts[i+1:]...)
			current = filepath.Clean(filepath.Join(filepath.Dir(probe), target, remaining))
			if _, err := repositoryPath(root, current); err != nil {
				return "", nil, err
			}
			followed = true
			break
		}
		if followed {
			continue
		}
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("broken symlink target %q", repositoryRelative(root, current))
		}
		return current, info, err
	}
}

func copyRepositoryNode(ctx context.Context, root, src, dst string, budget *repositoryBudget, active map[string]bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return copyRepositorySymlink(ctx, root, src, dst, budget, active)
	}
	if info.IsDir() {
		if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyRepositoryNode(ctx, root, filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), budget, active); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("non-regular symlink target %q", repositoryRelative(root, src))
	}
	if err := budget.add(repositoryRelative(root, src), info.Size()); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if err := copyWithContext(ctx, out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) error {
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func repositoryPath(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("symlink target escapes repository: %q", path)
	}
	if strings.EqualFold(strings.Split(rel, string(filepath.Separator))[0], ".git") {
		return "", fmt.Errorf("symlink targets repository metadata: %q", rel)
	}
	return rel, nil
}

func repositoryRelative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func scanRepository(ctx context.Context, dir string, limits Limits, rejectSymlinks bool) (*repositoryBudget, error) {
	budget := &repositoryBudget{limits: limits}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
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
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if rejectSymlinks {
				return fmt.Errorf("%w: symlink remained after expansion: %q", ErrLimitExceeded, rel)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: non-regular files are not allowed: %q", ErrLimitExceeded, rel)
		}
		return budget.add(rel, info.Size())
	})
	return budget, err
}

func enforceLimits(ctx context.Context, dir string, limits Limits) error {
	if _, err := scanRepository(ctx, dir, limits, true); err != nil {
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
