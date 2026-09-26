package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/codemode"
	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestMcpEnableThenDisableUserScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	out, err := executeCommand("mcp", "enable", "--scope", "user")
	if err != nil {
		t.Fatalf("mcp enable: %v\n%s", err, out)
	}
	cfgPath, err := config.UserPersonalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(data), "codeMode") || !strings.Contains(string(data), "enabled: true") {
		t.Fatalf("codeMode.enabled missing:\n%s", data)
	}

	out, err = executeCommand("mcp", "disable", "--scope", "user")
	if err != nil {
		t.Fatalf("mcp disable: %v\n%s", err, out)
	}
	data, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config read: %v", err)
	}
	if strings.Contains(string(data), "codeMode") {
		t.Fatalf("codeMode block not removed:\n%s", data)
	}
}

func TestMcpEnableRejectsUnknownScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := executeCommand("mcp", "enable", "--scope", "bogus"); err == nil {
		t.Fatal("expected unknown scope to fail")
	}
}

func TestMcpServerListsExactlySearchAndExecute(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runMCPServer(t.Context(), inR, outW) }()

	send := func(v map[string]any) {
		raw, _ := json.Marshal(v)
		if _, err := inW.Write(append(raw, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	_ = inW.Close()

	dec := json.NewDecoder(outR)
	var names []string
	for i := 0; i < 2; i++ {
		var resp struct {
			ID     int `json:"id"`
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := dec.Decode(&resp); err != nil {
			t.Fatalf("decode response %d: %v", i, err)
		}
		for _, tool := range resp.Result.Tools {
			names = append(names, tool.Name)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("server did not exit on stdin close")
	}
	if len(names) != 2 || names[0] != "search" || names[1] != "execute" {
		t.Fatalf("tools = %v", names)
	}
}

func TestMcpServerExitsOnContextCancel(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inW.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runMCPServer(ctx, inR, io.Discard) }()

	if _, err := inW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("server did not exit on context cancel while stdin stayed open")
	}
}

func TestInitCodeModeFlagEnablesCodeMode(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	out, err := executeCommand("init", "--harnesses", "claude", "--code-mode")
	if err != nil {
		t.Fatalf("init --code-mode: %v\n%s", err, out)
	}
	cfgPath, err := config.UserPersonalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "codeMode") || !strings.Contains(string(data), "enabled: true") {
		t.Fatalf("codeMode.enabled missing after init --code-mode:\n%s", data)
	}
}

func TestPromoteFailsWithoutSessionEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	_, err := executeCommand("promote", "reg/skill/never-used")
	if err == nil || !strings.Contains(err.Error(), "no code-mode session log") {
		t.Fatalf("expected missing-evidence error, got %v", err)
	}
}

func TestPromoteFindsLatestSessionEntry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	logDir := filepath.Join(dir, "agent-marketplace", "mcp-sessions")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := codemode.SessionEntry{Timestamp: time.Now().Add(-time.Hour).UTC(), Session: "a", Event: "skill.load", Ref: "reg/skill/pdf", Revision: "v1", Digest: "sha256:1"}
	recent := codemode.SessionEntry{Timestamp: time.Now().UTC(), Session: "b", Event: "server.start", Ref: "reg/mcp/srv", Revision: "v2", Digest: "sha256:2"}
	for i, e := range []codemode.SessionEntry{old, recent} {
		raw, _ := json.Marshal(e)
		if err := os.WriteFile(filepath.Join(logDir, string(rune('a'+i))+".jsonl"), append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := latestSessionEntry("reg/mcp/srv")
	if err != nil {
		t.Fatalf("latestSessionEntry: %v", err)
	}
	if entry.Session != "b" || entry.Revision != "v2" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, err := latestSessionEntry("reg/skill/absent"); err == nil {
		t.Fatal("expected error for unrecorded ref")
	}
}
