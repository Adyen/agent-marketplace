package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

func TestCommandProcessReportsResults(t *testing.T) {
	host := t.TempDir()
	t.Setenv("HOME", host)
	sentinel := filepath.Join(host, ".agents", "config.yaml")
	writeFile(t, sentinel, "host-sentinel")
	env := os.Environ()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		data, err := os.ReadFile(sentinel)
		if after, _ := os.Getwd(); after != cwd || !reflect.DeepEqual(env, os.Environ()) || err != nil || string(data) != "host-sentinel" {
			t.Error("failed child changed parent state or host sentinel")
		}
	}()
	for _, tc := range []struct {
		mode   string
		status string
		failed bool
	}{
		{"pass", "pass", false},
		{"skip", "skip", false},
		{"fail", "fail", true},
		{"panic", "fail", true},
		{"exit", "", true},
		{"cleanup-failure", "fail", true},
		{"output", "pass", false},
		{"malformed", "", true},
		{"nonzero", "fail", true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			result := runCommandProcess(t.Context(), t, "TestCommandProcessFixture", "^TestCommandProcessFixture$", "COMMAND_PROCESS_FIXTURE="+tc.mode)
			if result.status != tc.status || (result.err != nil) != tc.failed {
				t.Fatalf("status=%q err=%v output=%s", result.status, result.err, result.output)
			}
			if len(result.output) > 64*1024 {
				t.Fatalf("output is unbounded: %d bytes", len(result.output))
			}
			if tc.mode == "output" && !strings.Contains(result.output, "output-tail-sentinel") {
				t.Fatal("output tail was lost")
			}
			assertCommandSandboxRemoved(t, result.root)
		})
	}
}

func TestCommandProcessCoverageStaysInProcess(t *testing.T) {
	if testing.CoverMode() == "" && !*commandTestsInProcess {
		t.Skip("requires coverage or explicit in-process mode")
	}
	if isolateCommandTest(t) {
		t.Fatal("coverage or serial mode spawned a child instead of recording parent counters")
	}
	t.Setenv("COMMAND_PROCESS_SERIAL_PROBE", "parent")
}

func TestCommandProcessSelectors(t *testing.T) {
	for _, tc := range []struct{ selector, pattern string }{
		{"TestDoesNotExist", "^TestDoesNotExist$"},
		{"TestWrongName", "^TestCommandProcessFixture$"},
	} {
		result := runCommandProcess(t.Context(), t, tc.selector, tc.pattern, "COMMAND_PROCESS_FIXTURE=pass")
		if result.err == nil {
			t.Fatalf("invalid selector passed: %+v", tc)
		}
	}
	result := runCommandProcess(t.Context(), t, "TestCommandProcessFixture", exactCommandTestPattern("TestCommandProcessFixture/a[1]"), "COMMAND_PROCESS_FIXTURE=filter")
	if result.err != nil || strings.Count(result.output, "selected-sentinel") != 1 || strings.Contains(result.output, "unselected-sentinel") {
		t.Fatalf("incorrect exact subtest selection: %v\n%s", result.err, result.output)
	}
}

func TestCommandProcessCancellationAndDescendants(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "orphan-pipe", "orphan-closed"} {
		t.Run(mode, func(t *testing.T) {
			ready := filepath.Join(t.TempDir(), "pid")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if mode == "timeout" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 2*time.Second)
				defer stop()
			}
			done := make(chan commandProcessResult, 1)
			go func() {
				done <- runCommandProcess(ctx, t, "TestCommandProcessFixture", "^TestCommandProcessFixture$", "COMMAND_PROCESS_FIXTURE="+mode, "COMMAND_PROCESS_READY="+ready)
			}()
			var pid []byte
			for len(pid) == 0 && ctx.Err() == nil {
				pid, _ = os.ReadFile(ready)
				time.Sleep(10 * time.Millisecond)
			}
			if mode == "cancel" {
				cancel()
			}
			result := <-done
			if len(pid) == 0 {
				t.Fatalf("descendant never started: %v\n%s", result.err, result.output)
			}
			if mode == "cancel" && !errors.Is(result.err, context.Canceled) {
				t.Fatalf("missing cancellation: %v", result.err)
			}
			if mode == "timeout" && !errors.Is(result.err, context.DeadlineExceeded) {
				t.Fatalf("missing timeout: %v", result.err)
			}
			if mode == "orphan-closed" && result.err != nil {
				t.Fatalf("completed child failed: %v\n%s", result.err, result.output)
			}
			assertCommandSandboxRemoved(t, result.root)
			state, _ := exec.Command("/bin/ps", "-o", "stat=", "-p", string(pid)).Output()
			if state = []byte(strings.TrimSpace(string(state))); len(state) != 0 && state[0] != 'Z' {
				t.Fatalf("descendant %s survived: %s", pid, state)
			}
		})
	}
}

func TestCommandProcessConcurrentIsolation(t *testing.T) {
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG_PARAMETERS", "SSH_AUTH_SOCK", "HTTPS_PROXY", "PATH"} {
		t.Setenv(key, "parent-only-sentinel")
	}
	env := os.Environ()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	logger, org := slog.Default(), config.OrganizationConfigPath()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	results := make(chan commandProcessResult, 2)
	remaining := 2
	defer func() {
		cancel()
		for remaining > 0 {
			<-results
			remaining--
		}
	}()
	for _, sentinel := range []string{"first", "second"} {
		go func() {
			results <- runCommandProcess(ctx, t, "TestCommandProcessFixture", "^TestCommandProcessFixture$", "COMMAND_PROCESS_FIXTURE=isolation", "COMMAND_PROCESS_SENTINEL="+sentinel, "COMMAND_PROCESS_BARRIER="+listener.Addr().String())
		}()
	}
	var connections []net.Conn
	seen := map[string]string{}
	for range 2 {
		conn, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var state map[string]string
		if err := json.NewDecoder(conn).Decode(&state); err != nil {
			t.Fatal(err)
		}
		for key, value := range state {
			if value == "" || seen[key] == value {
				t.Fatalf("children shared or omitted %s: %q", key, value)
			}
			seen[key] = value
		}
		connections = append(connections, conn)
	}
	for _, conn := range connections {
		if _, err := conn.Write([]byte("release\n")); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		result := <-results
		remaining--
		if result.err != nil {
			t.Errorf("isolated child failed: %v\n%s", result.err, result.output)
		}
		assertCommandSandboxRemoved(t, result.root)
	}
	if after, _ := os.Getwd(); after != cwd || !reflect.DeepEqual(os.Environ(), env) || slog.Default() != logger || config.OrganizationConfigPath() != org {
		t.Fatal("children changed parent globals")
	}
}

func TestCommandProcessRealLockContention(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv(ownership.LockTimeoutEnvVar, "0")
	root := t.TempDir()
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := store.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, want := range []string{"blocked", "free"} {
		result := runCommandProcess(t.Context(), t, "TestCommandProcessFixture", "^TestCommandProcessFixture$", "COMMAND_PROCESS_FIXTURE=lock", "COMMAND_PROCESS_LOCK="+root, "COMMAND_PROCESS_WANT="+want, "XDG_STATE_HOME="+state, ownership.LockTimeoutEnvVar+"=0")
		if result.err != nil {
			t.Fatalf("lock %s: %v\n%s", want, result.err, result.output)
		}
		release()
	}
}

func assertCommandSandboxRemoved(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sandbox remains: %s (%v)", root, err)
	}
}

func TestCommandProcessFixture(t *testing.T) {
	mode := os.Getenv("COMMAND_PROCESS_FIXTURE")
	if mode == "" {
		t.Skip("runner fixture")
	}
	if isolateCommandTest(t) {
		return
	}
	switch mode {
	case "skip":
		t.Skip("intentional skip")
	case "fail":
		t.Error("intentional failure")
	case "panic":
		panic("intentional panic")
	case "exit":
		os.Exit(0)
	case "cleanup-failure":
		t.Cleanup(func() { t.Error("intentional cleanup failure") })
	case "output":
		fmt.Print(strings.Repeat("x", 128*1024), "output-tail-sentinel\n")
	case "malformed", "nonzero":
		data := "{"
		code := 0
		if mode == "nonzero" {
			data = `{"selector":"TestCommandProcessFixture","status":"pass"}`
			code = 17
		}
		if err := os.WriteFile(os.Getenv(processIsolatedCompletionEnv), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		os.Exit(code)
	case "filter":
		t.Run("a[1]", func(t *testing.T) { fmt.Println("selected-sentinel") })
		t.Run("a1", func(t *testing.T) { fmt.Println("unselected-sentinel") })
	case "cancel", "timeout", "orphan-pipe", "orphan-closed":
		cmd := exec.Command("/bin/sh", "-c", "exec sleep 60")
		if mode == "orphan-pipe" {
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		writeFile(t, os.Getenv("COMMAND_PROCESS_READY"), fmt.Sprint(cmd.Process.Pid))
		readOnly := filepath.Join(os.Getenv(processIsolatedRootEnv), "snapshot")
		writeFile(t, filepath.Join(readOnly, "file"), "immutable")
		if err := os.Chmod(readOnly, 0o500); err != nil {
			t.Fatal(err)
		}
		if mode == "cancel" || mode == "timeout" {
			time.Sleep(time.Minute)
		}
	case "isolation":
		commandIsolationFixture(t)
	case "lock":
		store, err := ownership.Open(os.Getenv("COMMAND_PROCESS_LOCK"))
		if err != nil {
			t.Fatal(err)
		}
		release, err := store.Lock()
		if os.Getenv("COMMAND_PROCESS_WANT") == "blocked" {
			if !errors.Is(err, ownership.ErrLockTimeout) {
				t.Fatalf("lock did not contend: %v", err)
			}
		} else if err != nil {
			t.Fatalf("released lock still contends: %v", err)
		}
		if release != nil {
			release()
		}
	}
}

func commandIsolationFixture(t *testing.T) {
	t.Helper()
	root, sentinel := os.Getenv(processIsolatedRootEnv), os.Getenv("COMMAND_PROCESS_SENTINEL")
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG_PARAMETERS", "SSH_AUTH_SOCK", "HTTPS_PROXY"} {
		if _, ok := os.LookupEnv(key); ok {
			t.Fatalf("inherited host variable %s", key)
		}
	}
	state := map[string]string{"root": root, "sentinel": sentinel, "org": config.OrganizationConfigPath()}
	cwd, err := os.Getwd()
	if err != nil || cwd != filepath.Join(root, "repo") {
		t.Fatalf("child cwd=%q err=%v", cwd, err)
	}
	state["cwd"] = cwd
	runGit(t, cwd, "init", "--initial-branch=main")
	state["cachedRoot"] = config.FindRepoRoot()
	if state["cachedRoot"] != cwd {
		t.Fatalf("wrong initial cached root: %q", state["cachedRoot"])
	}
	for _, key := range []string{"HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "CODEX_HOME", "TMPDIR"} {
		value := os.Getenv(key)
		if !strings.HasPrefix(value, root+string(os.PathSeparator)) {
			t.Fatalf("%s escaped sandbox: %q", key, value)
		}
		state[key] = value
		writeFile(t, filepath.Join(value, "sentinel"), sentinel)
	}
	isTerminal = func(uintptr) bool { return sentinel == "first" }
	writeFile(t, config.OrganizationConfigPath(), "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	conn, err := net.DialTimeout("tcp", os.Getenv("COMMAND_PROCESS_BARRIER"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(state); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	if isTerminal(0) != (sentinel == "first") || config.FindRepoRoot() != state["cachedRoot"] {
		t.Fatal("sibling changed internal hook or cached repo root")
	}
	for _, key := range []string{"HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "CODEX_HOME", "TMPDIR"} {
		data, err := os.ReadFile(filepath.Join(state[key], "sentinel"))
		if err != nil || string(data) != sentinel {
			t.Fatalf("sibling changed %s sentinel: %q (%v)", key, data, err)
		}
	}
}
