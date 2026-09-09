package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	processIsolatedSelectorEnv   = "AGENT_MARKETPLACE_PROCESS_TEST_SELECTOR"
	processIsolatedCompletionEnv = "AGENT_MARKETPLACE_PROCESS_TEST_COMPLETION"
	processIsolatedRootEnv       = "AGENT_MARKETPLACE_PROCESS_TEST_ROOT"
)

var commandTestsInProcess = flag.Bool("command-tests-in-process", false, "run command scenarios serially in-process (automatic with coverage)")
var commandProcessTempDir string
var commandProcessGit string

func isolateCommandTest(t *testing.T) bool {
	t.Helper()
	if selector := os.Getenv(processIsolatedSelectorEnv); selector != "" {
		if selector != t.Name() {
			t.Fatalf("child selector %q does not match %q", selector, t.Name())
		}
		t.Cleanup(func() {
			status := "pass"
			if t.Failed() {
				status = "fail"
			} else if t.Skipped() {
				status = "skip"
			}
			data, err := json.Marshal(commandCompletion{Selector: selector, Status: status})
			if err == nil {
				err = os.WriteFile(os.Getenv(processIsolatedCompletionEnv), data, 0o600)
			}
			if err != nil {
				t.Errorf("write child completion: %v", err)
			}
		})
		return false
	}
	if *commandTestsInProcess || testing.CoverMode() != "" {
		return false
	}
	t.Parallel()
	pattern := exactCommandTestPattern(t.Name())
	if filter := strings.SplitN(flag.Lookup("test.run").Value.String(), "/", 2); len(filter) == 2 {
		pattern += "/" + filter[1]
	}
	result := runCommandProcess(t.Context(), t, t.Name(), pattern)
	if result.err != nil {
		t.Fatalf("scenario %s: %v\n%s", t.Name(), result.err, result.output)
	}
	if result.status == "skip" {
		t.Skipf("child skipped:\n%s", result.output)
	}
	if testing.Verbose() && result.output != "" {
		t.Logf("child output:\n%s", result.output)
	}
	return true
}

func exactCommandTestPattern(name string) string {
	parts := strings.Split(name, "/")
	for i, part := range parts {
		parts[i] = "^" + regexp.QuoteMeta(part) + "$"
	}
	return strings.Join(parts, "/")
}

type commandCompletion struct {
	Selector string `json:"selector"`
	Status   string `json:"status"`
}

type commandProcessResult struct {
	status string
	output string
	err    error
	root   string
}

func runCommandProcess(parent context.Context, t *testing.T, selector, pattern string, extraEnv ...string) commandProcessResult {
	t.Helper()
	root, err := os.MkdirTemp(commandProcessTempDir, "agent-marketplace-process-test-")
	if err != nil {
		return commandProcessResult{err: err, root: root}
	}
	defer func() {
		if err := removeCommandSandbox(root); err != nil {
			t.Errorf("remove child sandbox: %v", err)
		}
	}()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return commandProcessResult{err: err, root: root}
	}
	root = canonicalRoot
	for _, dir := range []string{"home", "repo", "cache", "config", "state", "tmp", "org", "bin"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			return commandProcessResult{err: err, root: root}
		}
	}
	if commandProcessGit != "" {
		if err := os.Symlink(commandProcessGit, filepath.Join(root, "bin", "git")); err != nil {
			return commandProcessResult{err: err, root: root}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return commandProcessResult{err: err, root: root}
	}
	deadline := time.Now().Add(2 * time.Minute)
	if end, ok := t.Deadline(); ok && end.Add(-2*time.Second).Before(deadline) {
		deadline = end.Add(-2 * time.Second)
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	completionPath := filepath.Join(root, "completion.json")
	cmd := exec.CommandContext(ctx, executable, "-test.run="+pattern, "-test.v", "-test.count=1", "-test.timeout=0")
	cmd.Dir = filepath.Join(root, "repo")
	cmd.Env = append(commandProcessEnvironment(root, completionPath, selector), extraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killCommandProcessGroup(cmd) }
	cmd.WaitDelay = 500 * time.Millisecond
	output := &commandOutputTail{}
	cmd.Stdout, cmd.Stderr = output, output
	err = cmd.Run()
	// A descendant can retain the output descriptors after the test process
	// exits. Kill the whole group, then close both readers to bound cleanup.
	killErr := killCommandProcessGroup(cmd)
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		err = errors.Join(err, killErr)
	}
	result := commandProcessResult{root: root, output: output.String(), err: err}
	if ctx.Err() != nil {
		result.err = errors.Join(result.err, ctx.Err())
	}
	data, readErr := os.ReadFile(completionPath)
	var completion commandCompletion
	if readErr != nil {
		result.err = errors.Join(result.err, fmt.Errorf("missing child completion: %w", readErr))
	} else if err := json.Unmarshal(data, &completion); err != nil || completion.Selector != selector || (completion.Status != "pass" && completion.Status != "skip" && completion.Status != "fail") {
		result.err = errors.Join(result.err, errors.New("invalid child completion"))
	} else {
		result.status = completion.Status
		if result.err != nil {
			result.status = "fail"
		}
		if result.status == "fail" {
			result.err = errors.Join(result.err, errors.New("child assertions failed"))
		}
	}
	return result
}

func commandProcessEnvironment(root, completionPath, selector string) []string {
	return []string{
		"PATH=" + filepath.Join(root, "bin") + ":/usr/bin:/bin",
		"HOME=" + filepath.Join(root, "home"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_STATE_HOME=" + filepath.Join(root, "state"),
		"TMPDIR=" + filepath.Join(root, "tmp"),
		"CODEX_HOME=" + filepath.Join(root, "home", ".codex"),
		"PWD=" + filepath.Join(root, "repo"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=maintenance.auto",
		"GIT_CONFIG_VALUE_0=0",
		"AGENT_MARKETPLACE_NO_TUI=1",
		"AGENT_MARKETPLACE_LOG_ENABLED=false",
		processIsolatedRootEnv + "=" + root,
		processIsolatedSelectorEnv + "=" + selector,
		processIsolatedCompletionEnv + "=" + completionPath,
	}
}

func killCommandProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func removeCommandSandbox(root string) error {
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if entry != nil && entry.IsDir() {
			if chmodErr := os.Chmod(path, 0o700); chmodErr != nil {
				return chmodErr
			}
			return nil
		}
		return err
	})
	return errors.Join(walkErr, os.RemoveAll(root))
}

type commandOutputTail struct {
	mu   sync.Mutex
	data []byte
}

func (out *commandOutputTail) Write(p []byte) (int, error) {
	out.mu.Lock()
	defer out.mu.Unlock()
	const limit = 64 * 1024
	n := len(p)
	if n >= limit {
		out.data = append(out.data[:0], p[n-limit:]...)
	} else {
		if len(out.data)+n > limit {
			out.data = out.data[len(out.data)+n-limit:]
		}
		out.data = append(out.data, p...)
	}
	return n, nil
}

func (out *commandOutputTail) String() string {
	out.mu.Lock()
	defer out.mu.Unlock()
	return string(out.data)
}
