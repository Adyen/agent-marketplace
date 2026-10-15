package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Env wraps a temp-directory sandbox and provides helpers for E2E tests.
// Each test gets its own isolated filesystem: HOME, workspace, cache, and
// org-config directories are all under a temp root. The agent-marketplace
// binary runs as a subprocess with overridden env vars so it never touches
// the host system.
type Env struct {
	t         *testing.T
	binary    string // path to the built agent-marketplace binary
	home      string // $HOME for subprocess
	workspace string // cwd for subprocess
	repos     string // bare repos directory
	etc       string // org config directory
	path      string // PATH for subprocess (host PATH with `devin` shadowed out)
}

// newEnv creates an isolated sandbox for a test and registers cleanup.
func newEnv(t *testing.T) *Env {
	t.Helper()

	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	workspace := filepath.Join(dir, "workspace")
	repos := filepath.Join(dir, "repos")
	etc := filepath.Join(dir, "etc", "agent-marketplace")

	for _, d := range []string{home, workspace, repos, etc} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("e2e: mkdir %s: %v", d, err)
		}
	}

	return &Env{
		t:         t,
		binary:    binaryPath,
		home:      home,
		workspace: workspace,
		repos:     repos,
		etc:       etc,
		path:      sandboxPath(t),
	}
}

// shadowedBinaries lists executables that must never be resolvable from a
// sandbox subprocess. The suite's premise is isolation from the host env, and
// `doctor` detects an installed Devin CLI by walking PATH: a developer with
// Devin installed would otherwise see different output than the Docker runner,
// making assertions on the detection lines host-dependent.
//
// NOTE: this is POSIX-oriented. Names are matched literally (`devin`, not
// `devin.exe`) and PATHEXT is ignored, so shadowing provides no coverage on
// Windows. The suite is only expected to run on macOS/Linux (and in the Docker
// runner).
var shadowedBinaries = []string{"devin"}

// The sanitized PATH is identical for every Env, and building it means
// symlink-farming a host bin directory that can hold thousands of entries
// (/usr/local/bin). Build it at most once per test binary instead of once per
// test case, and share the result across parallel tests.
var (
	sandboxPathOnce sync.Once
	sandboxPathVal  string
	sandboxPathErr  error
	shadowRootDir   string // once-per-binary temp dir; removed by TestMain
)

// sandboxPath returns the deterministic PATH used by every sandbox subprocess,
// building the shadow symlink farm on first use. It fails the calling test
// loudly rather than degrading: a PATH that silently lost /usr/bin produces
// baffling, unrelated failures.
//
// sanitizePath itself returns an error (rather than taking a *testing.T and
// calling t.Fatalf) precisely so this wrapper can report the failure to every
// test that asks for a PATH. A t.Fatalf inside sync.Once.Do would only abort
// the one test that happened to win the race, leaving the rest with an empty
// PATH — the very silent degradation being fixed.
func sandboxPath(t *testing.T) string {
	t.Helper()
	sandboxPathOnce.Do(func() {
		shadowRootDir, sandboxPathErr = os.MkdirTemp("", "e2e-shadowbin-")
		if sandboxPathErr != nil {
			return
		}
		sandboxPathVal, sandboxPathErr = sanitizePath(os.Getenv("PATH"), shadowRootDir)
	})
	if sandboxPathErr != nil {
		t.Fatalf("e2e: build sanitized PATH: %v", sandboxPathErr)
	}
	return sandboxPathVal
}

// cleanupShadowRoot removes the once-per-binary shadow directory. Called from
// TestMain after all tests have finished, since the directory outlives every
// individual test's t.TempDir().
func cleanupShadowRoot() error {
	if shadowRootDir == "" {
		return nil
	}
	return os.RemoveAll(shadowRootDir)
}

// sanitizePath returns a PATH equivalent to hostPath in which none of
// shadowedBinaries is resolvable. Directories that do not contain any shadowed
// binary are passed through untouched (in order); a directory that does contain
// one is replaced by a shadow directory under shadowRoot holding symlinks to all
// of its *other* entries, so no unrelated tool (git, sh, ...) disappears.
//
// Any failure is returned, never swallowed: dropping a directory would remove
// co-located tools such as git and sh from the sandbox PATH.
func sanitizePath(hostPath, shadowRoot string) (string, error) {
	dirs := filepath.SplitList(hostPath)
	out := make([]string, 0, len(dirs))
	for i, dir := range dirs {
		if dir == "" {
			continue
		}
		if !dirHasAny(dir, shadowedBinaries) {
			out = append(out, dir)
			continue
		}
		// Each shadowed PATH entry gets its own shadow dir, keyed by index, so
		// the same binary appearing in several directories is handled and the
		// directories stay independent.
		shadow := filepath.Join(shadowRoot, strconv.Itoa(i))
		if err := shadowDir(dir, shadow, shadowedBinaries); err != nil {
			return "", fmt.Errorf("shadow PATH entry %q: %w", dir, err)
		}
		out = append(out, shadow)
	}
	return strings.Join(out, string(os.PathListSeparator)), nil
}

// dirHasAny reports whether dir directly contains any of the named files.
func dirHasAny(dir string, names []string) bool {
	for _, name := range names {
		if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// shadowDir populates shadow with symlinks to every entry of dir except the
// excluded names.
func shadowDir(dir, shadow string, exclude []string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(shadow, 0o755); err != nil {
		return err
	}
	skip := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		skip[name] = true
	}
	// The link target must be absolute: a relative PATH entry (e.g. "bin")
	// would otherwise be resolved relative to the shadow dir and dangle.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if skip[entry.Name()] {
			continue
		}
		link := filepath.Join(shadow, entry.Name())
		if err := os.Symlink(filepath.Join(absDir, entry.Name()), link); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// ---------- sanitizePath unit tests ----------

// lookInPath resolves name against a PATH list without touching the process
// environment, mirroring what a subprocess exec would find. Kept local to the
// test so it stays hermetic under t.Parallel().
func lookInPath(pathList, name string) (string, bool) {
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p) // Stat, not Lstat: a shadow entry is a symlink.
		if err != nil || fi.IsDir() || fi.Mode().Perm()&0o111 == 0 {
			continue
		}
		return p, true
	}
	return "", false
}

// writeExec creates an executable file named name inside dir.
func writeExec(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %s/%s: %v", dir, name, err)
	}
}

func TestSanitizePath(t *testing.T) {
	t.Parallel()

	sep := string(os.PathListSeparator)

	t.Run("clean dirs pass through unchanged", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		a := filepath.Join(root, "a")
		b := filepath.Join(root, "b")
		writeExec(t, a, "git")
		writeExec(t, b, "sh")

		in := a + sep + b
		got, err := sanitizePath(in, filepath.Join(root, "shadow"))
		if err != nil {
			t.Fatalf("sanitizePath: %v", err)
		}
		if got != in {
			t.Fatalf("expected untouched PATH %q, got %q", in, got)
		}
		if _, err := os.Stat(filepath.Join(root, "shadow")); !os.IsNotExist(err) {
			t.Fatalf("no shadow dir should have been created, stat err = %v", err)
		}
	})

	t.Run("shadowed binary is unresolvable but siblings survive", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		bin := filepath.Join(root, "bin")
		writeExec(t, bin, "devin")
		writeExec(t, bin, "git")

		got, err := sanitizePath(bin, filepath.Join(root, "shadow"))
		if err != nil {
			t.Fatalf("sanitizePath: %v", err)
		}
		if got == bin {
			t.Fatalf("expected %q to be replaced by a shadow dir, got %q", bin, got)
		}
		if p, ok := lookInPath(got, "devin"); ok {
			t.Fatalf("devin must not be resolvable through sanitized PATH, found %q", p)
		}
		if _, ok := lookInPath(got, "git"); !ok {
			t.Fatalf("co-located git must stay resolvable through %q", got)
		}
	})

	t.Run("ordering is preserved", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		first := filepath.Join(root, "first")
		mid := filepath.Join(root, "mid")
		last := filepath.Join(root, "last")
		writeExec(t, first, "git")
		writeExec(t, mid, "devin")
		writeExec(t, last, "sh")

		got, err := sanitizePath(first+sep+mid+sep+last, filepath.Join(root, "shadow"))
		if err != nil {
			t.Fatalf("sanitizePath: %v", err)
		}
		dirs := filepath.SplitList(got)
		if len(dirs) != 3 {
			t.Fatalf("expected 3 PATH entries, got %d (%q)", len(dirs), got)
		}
		if dirs[0] != first || dirs[2] != last {
			t.Fatalf("clean entries moved: %q", got)
		}
		if dirs[1] == mid {
			t.Fatalf("shadowed entry was not replaced: %q", got)
		}
	})

	t.Run("shadowed binary in several dirs gets a shadow each", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		one := filepath.Join(root, "one")
		two := filepath.Join(root, "two")
		writeExec(t, one, "devin")
		writeExec(t, one, "git")
		writeExec(t, two, "devin")
		writeExec(t, two, "sh")

		got, err := sanitizePath(one+sep+two, filepath.Join(root, "shadow"))
		if err != nil {
			t.Fatalf("sanitizePath: %v", err)
		}
		dirs := filepath.SplitList(got)
		if len(dirs) != 2 {
			t.Fatalf("expected 2 PATH entries, got %d (%q)", len(dirs), got)
		}
		if dirs[0] == dirs[1] {
			t.Fatalf("each shadowed dir needs its own shadow, both were %q", dirs[0])
		}
		if p, ok := lookInPath(got, "devin"); ok {
			t.Fatalf("devin resolvable at %q through %q", p, got)
		}
		for _, name := range []string{"git", "sh"} {
			if _, ok := lookInPath(got, name); !ok {
				t.Fatalf("%s must stay resolvable through %q", name, got)
			}
		}
	})

	t.Run("relative PATH entries yield working symlinks", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		bin := filepath.Join(root, "relbin")
		writeExec(t, bin, "devin")
		writeExec(t, bin, "git")

		// A relative entry must be absolutised before symlinking, otherwise the
		// link resolves relative to the shadow dir and dangles.
		rel, err := filepath.Rel(mustGetwd(t), bin)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		got, err := sanitizePath(rel, filepath.Join(root, "shadow"))
		if err != nil {
			t.Fatalf("sanitizePath: %v", err)
		}
		if _, ok := lookInPath(got, "git"); !ok {
			t.Fatalf("git must resolve through shadowed relative entry %q -> %q", rel, got)
		}
		if _, ok := lookInPath(got, "devin"); ok {
			t.Fatalf("devin must not resolve through %q", got)
		}
	})

	t.Run("unreadable shadowed dir is a loud error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory read permissions")
		}
		t.Parallel()
		root := t.TempDir()
		bin := filepath.Join(root, "bin")
		writeExec(t, bin, "devin")
		writeExec(t, bin, "git")
		// --x: Stat(dir/devin) succeeds, ReadDir(dir) fails.
		if err := os.Chmod(bin, 0o111); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(bin, 0o755) })

		if _, err := sanitizePath(bin, filepath.Join(root, "shadow")); err == nil {
			t.Fatal("expected an error for an unreadable shadowed dir, got nil")
		}
	})
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

// resolvePath maps container-style absolute paths from the test table to the
// local temp directory structure:
//
//	/root/...                      -> home/...
//	/workspace/...                 -> workspace/...
//	/repos/...                     -> repos/...
//	/etc/agent-marketplace/...     -> etc/...
func (e *Env) resolvePath(p string) (string, error) {
	mappings := []struct {
		prefix string
		base   string
	}{
		{"/root", e.home},
		{"/workspace", e.workspace},
		{"/repos", e.repos},
		{"/etc/agent-marketplace", e.etc},
	}
	for _, m := range mappings {
		if p == m.prefix || p == m.prefix+"/" {
			return m.base, nil
		}
		if strings.HasPrefix(p, m.prefix+"/") {
			rel := strings.TrimPrefix(p, m.prefix+"/")
			joined := filepath.Join(m.base, rel)
			cleaned := filepath.Clean(joined)
			if cleaned == m.base || strings.HasPrefix(cleaned, m.base+string(filepath.Separator)) {
				return cleaned, nil
			}
			return "", fmt.Errorf("path %q escapes sandbox mapping %q", p, m.prefix)
		}
	}
	return p, nil
}

func (e *Env) mustResolvePath(p string) string {
	e.t.Helper()
	resolved, err := e.resolvePath(p)
	if err != nil {
		e.t.Fatalf("resolve sandbox path %q: %v", p, err)
	}
	return resolved
}

// ---------- command execution ----------

// ExecResult holds the output of a command run inside the sandbox.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Combined string
}

const concurrencyHandshakeTimeout = 10 * time.Second

// openFIFOWriter waits a bounded amount of time for the subprocess reader.
// A plain os.OpenFile(O_WRONLY) can hide an early sync failure forever because
// opening a FIFO writer blocks until a reader exists.
func openFIFOWriter(t *testing.T, path string, processDone <-chan struct{}, result *ExecResult) *os.File {
	t.Helper()
	deadline := time.NewTimer(concurrencyHandshakeTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		// The writer must not survive an Exec while the fixture starts the
		// contending process. An inherited writer would keep the FIFO readable,
		// leaving the first process blocked on EOF while it holds the ownership
		// lock and the second process waits for that lock.
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err == nil {
			return os.NewFile(uintptr(fd), path)
		}
		if !errors.Is(err, syscall.ENXIO) {
			t.Fatalf("open FIFO writer %s: %v", path, err)
		}
		select {
		case <-processDone:
			t.Fatalf("sync exited before opening FIFO reader (exit=%d):\n%s", result.ExitCode, result.Combined)
		case <-deadline.C:
			t.Fatal("timed out waiting for sync to open FIFO reader")
		case <-ticker.C:
		}
	}
}

// waitForProcess bounds the completion half of a concurrency handshake.
func waitForProcess(t *testing.T, name string, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(concurrencyHandshakeTimeout):
		t.Fatalf("timed out waiting for %s subprocess", name)
	}
}

// subprocessEnv returns an environment isolated from every host-controlled
// application path and policy setting. Fixture-specific overrides must be
// applied after this baseline with subprocessEnvWithOverrides.
func (e *Env) subprocessEnv() []string {
	hostEnv := os.Environ()
	filtered := make([]string, 0, len(hostEnv)+8)
	for _, v := range hostEnv {
		key, _, _ := strings.Cut(v, "=")
		if isSandboxedAppEnv(key) {
			continue
		}
		filtered = append(filtered, v)
	}
	filtered = append(filtered,
		"HOME="+e.home,
		"XDG_STATE_HOME="+filepath.Join(e.home, ".local", "state"),
		"XDG_CONFIG_HOME="+filepath.Join(e.home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(e.home, ".cache"),
		"CODEX_HOME="+filepath.Join(e.home, ".codex"),
		// Sanitized so host-installed tools the CLI probes for (currently the
		// Devin CLI) cannot change assertion outcomes.
		"PATH="+e.path,
		"AGENT_MARKETPLACE_V3_ORG_CONFIG="+filepath.Join(e.etc, "config.yaml"),
		"AGENT_MARKETPLACE_E2E_INVOCATION_DIR="+e2eInvocationDir,
	)
	return filtered
}

func isSandboxedAppEnv(key string) bool {
	switch key {
	case "HOME", "PATH", "XDG_STATE_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "CODEX_HOME", "OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR":
		return true
	default:
		return strings.HasPrefix(key, "AGENT_MARKETPLACE_")
	}
}

func subprocessEnvWithOverrides(base []string, overrides map[string]string) []string {
	env := append([]string(nil), base...)
	positions := make(map[string]int, len(env))
	for i, value := range env {
		key, _, _ := strings.Cut(value, "=")
		positions[key] = i
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		assignment := key + "=" + overrides[key]
		if i, ok := positions[key]; ok {
			env[i] = assignment
			continue
		}
		env = append(env, assignment)
	}
	return env
}

// Exec runs a command inside the sandbox and returns the result.
func (e *Env) Exec(cmd ...string) ExecResult {
	e.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Dir = e.workspace
	c.Env = e.subprocessEnv()

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()

	result := ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Combined: stdout.String() + stderr.String(),
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			e.t.Fatalf("exec %v: %v", cmd, err)
		}
	}

	return result
}

// Run runs a shell command string via sh -c and returns the result.
func (e *Env) Run(shell string) ExecResult {
	e.t.Helper()
	return e.Exec("sh", "-c", shell)
}

// RunAM runs agent-marketplace with the given arguments.
func (e *Env) RunAM(args ...string) ExecResult {
	e.t.Helper()
	cmd := append([]string{e.binary}, args...)
	return e.Exec(cmd...)
}

// RunAMWithEnv runs the agent-marketplace binary with the given extra env vars
// overriding the sandbox defaults.
func (e *Env) RunAMWithEnv(extra map[string]string, args ...string) ExecResult {
	e.t.Helper()
	env := subprocessEnvWithOverrides(e.subprocessEnv(), extra)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, e.binary, args...)
	c.Dir = e.workspace
	c.Env = env

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()

	result := ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Combined: stdout.String() + stderr.String(),
		ExitCode: 0,
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			e.t.Fatalf("exec agent-marketplace %v: %v", args, err)
		}
	}
	return result
}

// RunAMFrom runs agent-marketplace with the given arguments from a custom
// working directory. The cwd is resolved through the sandbox path mapper.
func (e *Env) RunAMFrom(cwd string, args ...string) ExecResult {
	e.t.Helper()
	cmd := append([]string{e.binary}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Dir = e.mustResolvePath(cwd)
	c.Env = e.subprocessEnv()

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()

	result := ExecResult{
		ExitCode: 0,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Combined: stdout.String() + stderr.String(),
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			e.t.Fatalf("exec %v from %s: %v", args, cwd, err)
		}
	}
	return result
}

// MustRun runs a shell command and fails the test if it exits non-zero.
func (e *Env) MustRun(shell string) ExecResult {
	e.t.Helper()
	r := e.Run(shell)
	if r.ExitCode != 0 {
		e.t.Fatalf("command failed (exit %d): %s\n%s", r.ExitCode, shell, r.Combined)
	}
	return r
}

// MustRunAM runs agent-marketplace and fails if it exits non-zero.
func (e *Env) MustRunAM(args ...string) ExecResult {
	e.t.Helper()
	r := e.RunAM(args...)
	if r.ExitCode != 0 {
		e.t.Fatalf("agent-marketplace %s failed (exit %d):\n%s",
			strings.Join(args, " "), r.ExitCode, r.Combined)
	}
	return r
}

func (e *Env) MustLockOrganizationConfig(configPath string) {
	e.t.Helper()
	r := e.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_V3_ORG_CONFIG": configPath,
	}, "lock", "--scope", "organization")
	if r.ExitCode != 0 {
		e.t.Fatalf("lock organization config %s failed (exit %d):\n%s", configPath, r.ExitCode, r.Combined)
	}
}

// ---------- file injection ----------

// WriteFile creates a file inside the sandbox with the given content.
// Container-style paths (/root/..., /workspace/...) are resolved
// automatically.
func (e *Env) WriteFile(path, content string) {
	e.t.Helper()
	localPath := e.mustResolvePath(path)
	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(localPath, []byte(content), 0o644); err != nil {
		e.t.Fatalf("write %s: %v", localPath, err)
	}
}

// ReadFile reads a file inside the sandbox and returns its content.
// Container-style paths (/root/..., /workspace/...) are resolved
// automatically.
func (e *Env) ReadFile(path string) string {
	e.t.Helper()
	localPath := e.mustResolvePath(path)
	data, err := os.ReadFile(localPath)
	if err != nil {
		e.t.Fatalf("read %s: %v", localPath, err)
	}
	return string(data)
}

// WriteGlobalConfig writes ~/.agents/config.yaml inside the sandbox.
// Supplies v4 and a test harness for omitted fixture defaults; it never
// translates authored installation fields. Refusal tests use WriteFile.
func (e *Env) WriteGlobalConfig(content string) {
	e.t.Helper()
	content = strings.TrimSpace(content)
	if !strings.Contains(content, "apiVersion:") {
		content = "apiVersion: agent-marketplace/v4\n" + content
	}
	if !strings.Contains(content, "harnesses:") {
		content = "harnesses:\n  - factory\n" + content
	}
	e.WriteFile("/root/.agents/config.yaml", content)
}

// WriteRepoConfig writes .agents/config.yaml in the workspace.
// Automatically injects apiVersion if missing; project harnesses may be declared.
func (e *Env) WriteRepoConfig(content string) {
	e.t.Helper()
	content = strings.TrimSpace(content)
	if !strings.Contains(content, "apiVersion:") {
		content = "apiVersion: agent-marketplace/v4\n" + content
	}
	e.WriteFile("/workspace/.agents/config.yaml", content)
}

// WriteLocalConfig writes .agents/config.local.yaml in the workspace.
func (e *Env) WriteLocalConfig(content string) {
	e.t.Helper()
	content = strings.TrimSpace(content)
	if !strings.Contains(content, "apiVersion:") {
		content = "apiVersion: agent-marketplace/v4\n" + content
	}
	e.WriteFile("/workspace/.agents/config.local.yaml", content)
}

// WriteOrgConfig writes the canonical v4 organization config to the managed
// location (/etc/agent-marketplace/config.yaml in the sandbox).
func (e *Env) WriteOrgConfig(content string) {
	e.t.Helper()
	content = strings.TrimSpace(content)
	if !strings.Contains(content, "apiVersion:") {
		content = "apiVersion: agent-marketplace/v4\n" + content
	}
	if !strings.Contains(content, "harnesses:") {
		content = "harnesses:\n  - factory\n" + content
	}
	e.WriteFile("/etc/agent-marketplace/config.yaml", content)
}

// WriteOrgConfigWithMode writes an explicit organization config to the
// managed location (/etc/agent-marketplace/config.yaml in the sandbox) using
// the given mode. It does not auto-inject apiVersion, harnesses, or other
// defaults, so callers supply the exact file content and permissions.
func (e *Env) WriteOrgConfigWithMode(content string, mode os.FileMode) {
	e.t.Helper()
	e.WriteFile("/etc/agent-marketplace/config.yaml", strings.TrimSpace(content)+"\n")
	localPath := e.mustResolvePath("/etc/agent-marketplace/config.yaml")
	if err := os.Chmod(localPath, mode); err != nil {
		e.t.Fatalf("chmod %s: %v", localPath, err)
	}
}

// InitGitRepo initialises the workspace directory as a git repo so that
// FindRepoRoot can locate .agents/config.yaml.
func (e *Env) InitGitRepo() {
	e.t.Helper()
	e.MustRun(`git init && git add -A 2>/dev/null; git -c user.name='E2E Test' -c user.email='e2e@test.local' commit --allow-empty -m "init"`)
}

// CreateBareRepo creates a bare git repo in the sandbox's repos directory,
// seeds it with the given files map (path -> content), commits and pushes.
// Returns the file:// URL usable in config.
func (e *Env) CreateBareRepo(name string, files map[string]string) string {
	e.t.Helper()
	script := buildRepoScript(e.repos, name, files)
	e.MustRun(script)
	return fmt.Sprintf("file://%s/%s.git", e.repos, name)
}

// ---------- assertions ----------

// AssertExitCode checks the exit code of a result.
func (e *Env) AssertExitCode(r ExecResult, want int) {
	e.t.Helper()
	if r.ExitCode != want {
		e.t.Errorf("exit code: got %d, want %d\noutput:\n%s", r.ExitCode, want, r.Combined)
	}
}

// AssertOutputContains checks that the combined output contains the substring.
func (e *Env) AssertOutputContains(r ExecResult, substr string) {
	e.t.Helper()
	if !strings.Contains(r.Combined, substr) {
		e.t.Errorf("output does not contain %q\nfull output:\n%s", substr, r.Combined)
	}
}

// AssertOutputNotContains checks that the combined output does NOT contain the substring.
func (e *Env) AssertOutputNotContains(r ExecResult, substr string) {
	e.t.Helper()
	if strings.Contains(r.Combined, substr) {
		e.t.Errorf("output unexpectedly contains %q\nfull output:\n%s", substr, r.Combined)
	}
}

// AssertFileExists checks that a file exists inside the sandbox.
// Container-style paths are resolved automatically.
func (e *Env) AssertFileExists(path string) {
	e.t.Helper()
	localPath := e.mustResolvePath(path)
	if _, err := os.Stat(localPath); err != nil {
		e.t.Errorf("file %q does not exist in sandbox", path)
	}
}

func checkPathAbsent(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

// AssertFileAbsent checks that a file does NOT exist inside the sandbox.
func (e *Env) AssertFileAbsent(path string) {
	e.t.Helper()
	localPath := e.mustResolvePath(path)
	if absent, err := checkPathAbsent(localPath); err != nil {
		e.t.Errorf("cannot inspect %q: %v", path, err)
	} else if !absent {
		e.t.Errorf("file %q unexpectedly exists in sandbox", path)
	}
}

// AssertFileContains checks that a file inside the sandbox contains the substring.
func (e *Env) AssertFileContains(path, substr string) {
	e.t.Helper()
	localPath := e.mustResolvePath(path)
	data, err := os.ReadFile(localPath)
	if err != nil {
		e.t.Errorf("cannot read %q: %v", path, err)
		return
	}
	if !strings.Contains(string(data), substr) {
		e.t.Errorf("file %q does not contain %q\ncontent:\n%s", path, substr, string(data))
	}
}

// AssertRepoFactorySkillExists checks for .factory/skills/<name>/SKILL.md
// in the repo-scoped path (workspace/.factory).
func (e *Env) AssertRepoFactorySkillExists(name string) {
	e.t.Helper()
	e.AssertFileExists(fmt.Sprintf("/workspace/.factory/skills/%s/SKILL.md", name))
}

// AssertRepoFactorySkillAbsent checks that .factory/skills/<name> does NOT
// exist at the repo-scope path.
func (e *Env) AssertRepoFactorySkillAbsent(name string) {
	e.t.Helper()
	e.AssertFileAbsent(fmt.Sprintf("/workspace/.factory/skills/%s", name))
}

// ---------- non-fatal check methods (return bool, no t.Errorf) ----------

// CheckFileExists reports whether a file exists inside the sandbox.
func (e *Env) CheckFileExists(path string) bool {
	e.t.Helper()
	localPath := e.mustResolvePath(path)
	_, err := os.Stat(localPath)
	return err == nil
}

// CheckSymlink reports whether a path is a symbolic link.
func (e *Env) CheckSymlink(path string) bool {
	e.t.Helper()
	localPath := e.mustResolvePath(path)
	fi, err := os.Lstat(localPath)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// CheckRegularFile reports whether a path exists as a non-symlink regular file.
func (e *Env) CheckRegularFile(path string) bool {
	e.t.Helper()
	fi, err := os.Lstat(e.mustResolvePath(path))
	return err == nil && fi.Mode().IsRegular()
}

// CheckFileContains reports whether a file inside the sandbox contains the substring.
func (e *Env) CheckFileContains(path, substr string) bool {
	e.t.Helper()
	localPath := e.mustResolvePath(path)
	data, err := os.ReadFile(localPath)
	return err == nil && strings.Contains(string(data), substr)
}
