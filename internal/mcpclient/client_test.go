package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/codemode"
)

func TestMain(m *testing.M) {
	switch os.Getenv("GO_MCP_FAKE") {
	case "1":
		fakeServer()
		return
	case "stderr-exit":
		fmt.Fprintf(os.Stderr, "boom %s boom\n", os.Getenv("SECRET_TOKEN"))
		_, _ = io.WriteString(os.Stderr, strings.Repeat("z", 4096))
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func fakeServer() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			continue
		}
		var method string
		var id json.RawMessage
		_ = json.Unmarshal(msg["method"], &method)
		id = msg["id"]
		if method == "notifications/initialized" {
			continue
		}
		var result any
		switch method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "fake"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "ping", "description": "echo"},
				{"name": "env", "description": "read env"},
				{"name": "crash"},
			}}
		case "tools/call":
			var p struct {
				Name string                 `json:"name"`
				Args map[string]interface{} `json:"arguments"`
			}
			_ = json.Unmarshal(msg["params"], &p)
			switch p.Name {
			case "ping":
				out, _ := json.Marshal(p.Args)
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": string(out)}}}
			case "env":
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": os.Getenv("SECRET_TOKEN")}}}
			case "toolerror":
				result = map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "tool failed hard"}}}
			case "hang":
				time.Sleep(10 * time.Second)
			case "crash":
				os.Exit(2)
			}
		default:
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "error": map[string]any{"code": -32601, "message": "unknown"}})
			continue
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
		if method == "initialize" && os.Getenv("GO_MCP_FAKE_STALL") == "1" {
			time.Sleep(time.Hour)
		}
	}
}

func fakeDef(env map[string]string, args ...string) *codemode.ServerDef {
	return &codemode.ServerDef{
		Command: os.Args[0],
		Args:    append([]string{"-test.run", "TestMain"}, args...),
		Env:     env,
	}
}

func fakeEnv() map[string]string { return map[string]string{"GO_MCP_FAKE": "1"} }

func fakeResource(def *codemode.ServerDef) *codemode.Resource {
	r := &codemode.Resource{Ref: "reg/mcp/fake", Kind: "mcp", Name: "fake", Registry: "reg", Assessment: "allow"}
	r.SetOrigin(&codemode.Origin{Server: def})
	return r
}

func TestSpawnListToolsAndCall(t *testing.T) {
	def := fakeDef(fakeEnv())
	c, err := Spawn(context.Background(), def)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = c.Close() }()
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 3 || tools[0].Name != "ping" {
		t.Fatalf("tools = %+v", tools)
	}
	out, err := c.Call(context.Background(), "ping", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	m := out.(map[string]any)
	if !strings.Contains(m["text"].(string), `"x":1`) {
		t.Fatalf("out = %v", m)
	}
}

func TestEnvironmentFiltering(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "s3cret")
	t.Setenv("LEAKED", "should-not-appear")

	def := fakeDef(map[string]string{"GO_MCP_FAKE": "1", "SECRET_TOKEN": ""})
	c, err := Spawn(context.Background(), def)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = c.Close() }()
	out, err := c.Call(context.Background(), "env", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.(map[string]any)["text"] != "s3cret" {
		t.Fatalf("env not forwarded: %v", out)
	}

	env := declaredEnv(def.Env)
	for _, kv := range env {
		if strings.HasPrefix(kv, "LEAKED=") {
			t.Fatalf("undeclared env leaked: %v", env)
		}
	}
}

func TestCrashNotRestarted(t *testing.T) {
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	res := fakeResource(fakeDef(fakeEnv()))
	if _, err := pool.Call(context.Background(), "fake", res, "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err := pool.Call(context.Background(), "fake", res, "crash", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected crash error")
	}
	_, err2 := pool.Call(context.Background(), "fake", res, "ping", json.RawMessage(`{}`))
	var ce *codemode.Error
	if !errors.As(err2, &ce) || ce.Code != codemode.ErrCodeToolError {
		t.Fatalf("expected %s after crash, got %v", codemode.ErrCodeToolError, err2)
	}
}

func TestHangKilledAtDeadline(t *testing.T) {
	pool := NewPool(NewSchemaCache(t.TempDir()), 150*time.Millisecond)
	res := fakeResource(fakeDef(fakeEnv()))
	_, err := pool.Call(context.Background(), "fake", res, "hang", json.RawMessage(`{}`))
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeTimeout {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeTimeout, err)
	}
}

func TestToolErrorCatchableCode(t *testing.T) {
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	res := fakeResource(fakeDef(fakeEnv()))
	_, err := pool.Call(context.Background(), "fake", res, "toolerror", json.RawMessage(`{}`))
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeToolError {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeToolError, err)
	}
}

func TestSchemaCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cache := NewSchemaCache(dir)
	tools := []codemode.ToolSchema{{Name: "ping"}}
	def := fakeDef(fakeEnv())
	if err := cache.Put(DefinitionDigest(def), tools); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := cache.Get(DefinitionDigest(def))
	if !ok || len(got) != 1 || got[0].Name != "ping" {
		t.Fatalf("Get = %v %v", got, ok)
	}

	pool := NewPool(cache, 5*time.Second)
	got, err := pool.Tools(context.Background(), "fake", fakeResource(def))
	if err != nil || len(got) != 1 {
		t.Fatalf("Tools = %v %v", got, err)
	}
	if len(pool.servers) != 0 {
		t.Fatal("server spawned on cache hit")
	}
	_ = filepath.Join
}

func TestPoolCloseStopsChildren(t *testing.T) {
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	res := fakeResource(fakeDef(fakeEnv()))
	if _, err := pool.Call(context.Background(), "fake", res, "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("call: %v", err)
	}
	if err := pool.Close(); err != nil {

		if !strings.Contains(err.Error(), "signal") && !strings.Contains(err.Error(), "killed") {
			t.Fatalf("Close: %v", err)
		}
	}
	_, err := pool.Call(context.Background(), "fake", res, "ping", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected closed-pool error")
	}
}

func TestHTTPRejectedInsecure(t *testing.T) {
	_, err := NewHTTP(&codemode.ServerDef{URL: "http://example.com/mcp"})
	if err == nil {
		t.Fatal("expected insecure URL rejection")
	}
}

func TestStartFailedCapturesBoundedRedactedStderr(t *testing.T) {
	secret := "s3cr3t-value-12345"
	def := fakeDef(map[string]string{"GO_MCP_FAKE": "stderr-exit", "SECRET_TOKEN": secret})
	c, err := Spawn(context.Background(), def)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = c.Close() }()
	err = c.Initialize(context.Background())
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeStartFailed {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeStartFailed, err)
	}
	if !strings.Contains(ce.Message, "boom") {
		t.Fatalf("stderr not included in start failure: %q", ce.Message)
	}
	if strings.Contains(ce.Message, secret) {
		t.Fatalf("declared secret leaked in error: %q", ce.Message)
	}
	if len(ce.Message) > 2200 {
		t.Fatalf("stderr not bounded near 2KB: %d bytes", len(ce.Message))
	}
	if strings.Contains(ce.Message, "-test.run") || strings.Contains(ce.Message, "SECRET_TOKEN=") {
		t.Fatalf("raw arguments or environment leaked: %q", ce.Message)
	}
}

func TestBlockedStdinWriteCancelledAtDeadline(t *testing.T) {
	def := fakeDef(map[string]string{"GO_MCP_FAKE": "1", "GO_MCP_FAKE_STALL": "1"})
	c, err := Spawn(context.Background(), def)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = c.Close() }()
	big := json.RawMessage(`{"blob":"` + strings.Repeat("y", 8<<20) + `"}`)

	first := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := c.Call(ctx, "ping", big)
		first <- err
	}()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	start2 := time.Now()
	_, err2 := c.Call(ctx2, "ping", big)
	var ce *codemode.Error
	if !errors.As(err2, &ce) || ce.Code != codemode.ErrCodeTimeout {
		t.Fatalf("queued call must finish on its own ctx with %s, got %v", codemode.ErrCodeTimeout, err2)
	}
	if time.Since(start2) > 1500*time.Millisecond {
		t.Fatalf("queued call waited on blocked exchange instead of its own deadline")
	}

	select {
	case err1 := <-first:
		if !errors.As(err1, &ce) || ce.Code != codemode.ErrCodeTimeout {
			t.Fatalf("expected %s, got %v", codemode.ErrCodeTimeout, err1)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked stdin write did not finish at its deadline")
	}

	_, err3 := c.Call(context.Background(), "ping", json.RawMessage(`{}`))
	if !errors.As(err3, &ce) || ce.Code != codemode.ErrCodeToolError {
		t.Fatalf("expected %s for dead server, got %v", codemode.ErrCodeToolError, err3)
	}
}

func TestStartFailedCoded(t *testing.T) {
	_, err := Spawn(context.Background(), &codemode.ServerDef{Command: "/nonexistent/binary/xyz"})
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeStartFailed {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeStartFailed, err)
	}
	_ = fmt.Sprint
}
