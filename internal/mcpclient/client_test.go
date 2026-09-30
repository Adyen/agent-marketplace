package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
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
		if method == "initialize" && os.Getenv("GO_MCP_FAKE_INITERR") == "1" {
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "error": map[string]any{"code": -32000, "message": "initialize refused " + os.Getenv("SECRET_TOKEN")}})
			continue
		}
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
			case "rpcerror":
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "error": map[string]any{"code": -32000, "message": "denied " + os.Getenv("SECRET_TOKEN")}})
				continue
			case "toolerror":
				result = map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "tool failed hard " + os.Getenv("SECRET_TOKEN")}}}
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

// spec: code-mode-mcp/crashed-server-is-not-restarted
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

// spec: code-mode-mcp/hung-server-killed-at-deadline
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
}

// spec: code-mode-mcp/session-close-stops-child-servers
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

// spec: code-mode-mcp/remote-endpoints-require-https-or-loopback
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
}

func TestHTTPDoesNotFollowRedirects(t *testing.T) {
	var destHits int32
	var destAuth []string
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&destHits, 1)
		destAuth = append(destAuth, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer dest.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL, http.StatusFound)
	}))
	defer src.Close()

	secret := "Bearer test-secret-xyz"
	c, err := NewHTTP(&codemode.ServerDef{URL: src.URL, Headers: map[string]string{"Authorization": secret}})
	if err != nil {
		t.Fatalf("NewHTTP: %v", err)
	}
	err = c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected the redirect to be refused")
	}
	if atomic.LoadInt32(&destHits) != 0 {
		t.Fatalf("redirected request reached the destination %d times with auth %v", destHits, destAuth)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "test-secret-xyz") {
		t.Fatalf("declared header leaked in error: %v", err)
	}
}

func TestHTTPDirectRequestSendsDeclaredHeaders(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		result := json.RawMessage(`{"protocolVersion":"` + protocolVersion + `","capabilities":{}}`)
		if req.Method == "tools/list" {
			result = json.RawMessage(`{"tools":[{"name":"ping"}]}`)
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_ = json.NewEncoder(w).Encode(rpcReply{JSONRPC: "2.0", ID: req.ID, Result: result})
	}))
	defer server.Close()

	c, err := NewHTTP(&codemode.ServerDef{URL: server.URL, Headers: map[string]string{"Authorization": "Bearer test-secret-xyz"}})
	if err != nil {
		t.Fatalf("NewHTTP: %v", err)
	}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools on direct endpoint: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("tools = %+v", tools)
	}
	if gotAuth != "Bearer test-secret-xyz" {
		t.Fatalf("declared header not sent to the configured endpoint: %q", gotAuth)
	}
}

// spec: code-mode-mcp/environment-references-expanded-before-start
func TestEnvironmentReferencesExpandedBeforeStart(t *testing.T) {
	t.Setenv("AM_EXPAND_TOKEN", "expanded-secret")
	t.Setenv("AM_EXPAND_BIN", os.Args[0])
	t.Setenv("AM_HOST_ONLY", "must-not-forward")

	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	res := fakeResource(&codemode.ServerDef{
		Command: "${AM_EXPAND_BIN}",
		Args:    []string{"-test.run", "TestMain"},
		Env: map[string]string{
			"GO_MCP_FAKE":  "1",
			"SECRET_TOKEN": "${env:AM_EXPAND_TOKEN}",
		},
	})
	out, err := pool.Call(context.Background(), "fake", res, "env", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.(map[string]any)["text"] != "expanded-secret" {
		t.Fatalf("env value not expanded: %v", out)
	}
	for _, kv := range declaredEnv(res.ServerDef().Env) {
		if strings.HasPrefix(kv, "AM_HOST_ONLY=") {
			t.Fatalf("host-only variable forwarded: %v", kv)
		}
	}
}

// spec: code-mode-mcp/environment-references-expanded-before-start
func TestHeaderAndURLReferencesExpandedBeforeStart(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		result := json.RawMessage(`{"protocolVersion":"` + protocolVersion + `","capabilities":{}}`)
		if req.Method == "tools/list" {
			result = json.RawMessage(`{"tools":[{"name":"ping"}]}`)
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_ = json.NewEncoder(w).Encode(rpcReply{JSONRPC: "2.0", ID: req.ID, Result: result})
	}))
	defer server.Close()

	t.Setenv("AM_HDR_TOKEN", "expanded-header-secret")
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	def := &codemode.ServerDef{
		URL:     server.URL + "/${AM_URL_SUFFIX}",
		Headers: map[string]string{"Authorization": "Bearer ${AM_HDR_TOKEN}"},
	}
	t.Setenv("AM_URL_SUFFIX", "mcp")
	res := &codemode.Resource{Ref: "reg/mcp/remote", Kind: "mcp", Name: "remote", Registry: "reg", Assessment: "allow"}
	res.SetOrigin(&codemode.Origin{Server: def})
	tools, err := pool.Tools(context.Background(), "remote", res)
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 1 || gotAuth != "Bearer expanded-header-secret" {
		t.Fatalf("expanded values not applied: tools=%v auth=%q", tools, gotAuth)
	}
}

// spec: code-mode-mcp/unresolvable-reference-fails-before-start
func TestUnresolvableReferenceFailsBeforeStart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		def     *codemode.ServerDef
		field   string
		varName string
	}{
		{"unset env value", fakeDef(map[string]string{"GO_MCP_FAKE": "1", "SECRET_TOKEN": "${AM_UNSET_TOKEN}"}), "env.SECRET_TOKEN", "AM_UNSET_TOKEN"},
		{"unset arg", &codemode.ServerDef{Command: "sh", Args: []string{"-c", "${AM_UNSET_ARG}"}, Env: fakeEnv()}, "args", "AM_UNSET_ARG"},
		{"file reference", &codemode.ServerDef{Command: "sh", Args: []string{"${file:/etc/passwd}"}, Env: fakeEnv()}, "args", "file:/etc/passwd"},
		{"unset header", &codemode.ServerDef{URL: "https://mcp.example.com/mcp", Headers: map[string]string{"X-Auth": "${AM_UNSET_HDR}"}}, "headers.X-Auth", "AM_UNSET_HDR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
			res := fakeResource(tc.def)
			_, err := pool.Tools(context.Background(), "fake", res)
			var ce *codemode.Error
			if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeStartFailed {
				t.Fatalf("expected %s, got %v", codemode.ErrCodeStartFailed, err)
			}
			if !strings.Contains(ce.Message, tc.field) || !strings.Contains(ce.Message, tc.varName) {
				t.Fatalf("error must name field %q and variable %q: %q", tc.field, tc.varName, ce.Message)
			}
			if len(pool.servers) != 0 {
				t.Fatal("a process started despite the unresolvable reference")
			}
		})
	}
}

// spec: code-mode-mcp/remote-initialization-rejection-reports-start-failure
func TestRemoteInitializationRejectionReportsStartFailure(t *testing.T) {
	secret := "expanded-header-secret-789"
	t.Setenv("AM_REJECT_TOKEN", secret)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("denied credential " + r.Header.Get("Authorization") + " rejected"))
	}))
	defer server.Close()

	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	res := &codemode.Resource{Ref: "reg/mcp/remote", Kind: "mcp", Name: "remote", Registry: "reg", Assessment: "allow"}
	res.SetOrigin(&codemode.Origin{Server: &codemode.ServerDef{
		URL:     server.URL,
		Headers: map[string]string{"Authorization": "Bearer ${AM_REJECT_TOKEN}"},
	}})
	_, err := pool.Tools(context.Background(), "remote", res)
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeStartFailed {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeStartFailed, err)
	}
	if !strings.Contains(ce.Message, "401") {
		t.Fatalf("start failure must report the rejection: %q", ce.Message)
	}
	if strings.Contains(ce.Message, secret) {
		t.Fatalf("expanded header value leaked in error: %q", ce.Message)
	}
	if len(ce.Message) > 2200 {
		t.Fatalf("rejection body not bounded: %d bytes", len(ce.Message))
	}
}

// spec: code-mode-mcp/remote-initialization-rejection-reports-start-failure
func TestStdioInitializeRejectionReportsStartFailure(t *testing.T) {
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	res := fakeResource(fakeDef(map[string]string{"GO_MCP_FAKE": "1", "GO_MCP_FAKE_INITERR": "1"}))
	_, err := pool.Tools(context.Background(), "fake", res)
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeStartFailed {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeStartFailed, err)
	}
}

// spec: code-mode-mcp/remote-initialization-rejection-reports-start-failure
func TestStdioInitializeRejectionRedactsDeclaredEnv(t *testing.T) {
	secret := "s3cr3t-init-98765"
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	res := fakeResource(fakeDef(map[string]string{"GO_MCP_FAKE": "1", "GO_MCP_FAKE_INITERR": "1", "SECRET_TOKEN": secret}))
	_, err := pool.Tools(context.Background(), "fake", res)
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeStartFailed {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeStartFailed, err)
	}
	if strings.Contains(ce.Message, secret) {
		t.Fatalf("declared env value leaked in start failure: %q", ce.Message)
	}
}

// spec: code-mode-mcp/failures-carry-stable-codes
func TestStdioToolCallErrorRedactsDeclaredEnv(t *testing.T) {
	secret := "s3cr3t-rpc-98765"
	c, err := Spawn(context.Background(), fakeDef(map[string]string{"GO_MCP_FAKE": "1", "SECRET_TOKEN": secret}))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = c.Close() }()
	_, err = c.Call(context.Background(), "rpcerror", json.RawMessage(`{}`))
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeToolError {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeToolError, err)
	}
	if strings.Contains(ce.Message, secret) {
		t.Fatalf("declared env value leaked in tool error: %q", ce.Message)
	}

	_, err = c.Call(context.Background(), "toolerror", json.RawMessage(`{}`))
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeToolError {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeToolError, err)
	}
	if strings.Contains(ce.Message, secret) {
		t.Fatalf("declared env value leaked in tool-error content: %q", ce.Message)
	}
}

// spec: code-mode-mcp/remote-initialization-rejection-reports-start-failure
func TestHTTPInitializeRPCErrorFailsStart(t *testing.T) {
	var initializedSeen int32
	secret := "Bearer rpc-secret-456"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Method == "notifications/initialized" {
			atomic.StoreInt32(&initializedSeen, 1)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"error": map[string]any{"code": -32000, "message": "denied " + r.Header.Get("Authorization")},
		})
	}))
	defer server.Close()

	c, err := NewHTTP(&codemode.ServerDef{URL: server.URL, Headers: map[string]string{"Authorization": secret}})
	if err != nil {
		t.Fatalf("NewHTTP: %v", err)
	}
	err = c.Initialize(context.Background())
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeStartFailed {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeStartFailed, err)
	}
	if strings.Contains(ce.Message, secret) {
		t.Fatalf("declared header leaked in start failure: %q", ce.Message)
	}
	if atomic.LoadInt32(&initializedSeen) != 0 {
		t.Fatal("initialized notification sent after a rejected handshake")
	}
}
