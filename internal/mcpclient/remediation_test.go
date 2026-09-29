package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/codemode"
)

func TestToolsLazyMarketplaceDefResolvesDigest(t *testing.T) {
	def := fakeDef(fakeEnv())
	res := &codemode.Resource{Ref: "m:x/mcp/fake", Kind: "mcp", Name: "fake", Registry: "x", Assessment: "allow"}
	res.SetOrigin(&codemode.Origin{Marketplace: &codemode.MarketplaceOrigin{Kind: "mcp", Name: "fake"}})

	cache := NewSchemaCache(t.TempDir())
	pool := NewPool(cache, 5*time.Second)
	var resolved int
	pool.ResolveDef = func(ctx context.Context, r *codemode.Resource) (*codemode.ServerDef, error) {
		resolved++
		return def, nil
	}
	tools, err := pool.Tools(context.Background(), "fake", res)
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("tools = %+v", tools)
	}
	if resolved != 1 {
		t.Fatalf("ResolveDef called %d times", resolved)
	}

	if err := cache.Put(DefinitionDigest(def), tools); err != nil {
		t.Fatalf("Put: %v", err)
	}
	pool2 := NewPool(cache, 5*time.Second)
	var resolved2 int
	pool2.ResolveDef = func(ctx context.Context, r *codemode.Resource) (*codemode.ServerDef, error) {
		resolved2++
		return def, nil
	}
	got, err := pool2.Tools(context.Background(), "fake", res)
	if err != nil || len(got) != 3 {
		t.Fatalf("cached Tools = %v, %v", got, err)
	}
	if len(pool2.servers) != 0 {
		t.Fatal("server spawned on schema cache hit")
	}
	if resolved2 > 1 {
		t.Fatalf("ResolveDef called %d times on cache hit", resolved2)
	}
}

func TestPoolDefCacheKeyedByRevision(t *testing.T) {
	mkRes := func(rev string) *codemode.Resource {
		r := &codemode.Resource{Ref: "m:x/mcp/fake", Kind: "mcp", Name: "fake", Registry: "x", Revision: rev, Assessment: "allow"}
		r.SetOrigin(&codemode.Origin{Marketplace: &codemode.MarketplaceOrigin{Kind: "mcp", Name: "fake", Revision: rev}})
		return r
	}
	defs := map[string]*codemode.ServerDef{
		"v1": fakeDef(fakeEnv()),
		"v2": fakeDef(map[string]string{"GO_MCP_FAKE": "1", "REV": "v2"}),
	}
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	var starts []string
	pool.ObserveStarts(func(alias string, res *codemode.Resource, def *codemode.ServerDef) {
		starts = append(starts, res.Revision+"@"+DefinitionDigest(def))
	})
	pool.ResolveDef = func(ctx context.Context, r *codemode.Resource) (*codemode.ServerDef, error) {
		return defs[r.Revision], nil
	}
	if _, err := pool.Call(context.Background(), "fake", mkRes("v1"), "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("call v1: %v", err)
	}
	if _, err := pool.Call(context.Background(), "fake", mkRes("v2"), "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("call v2: %v", err)
	}
	if len(pool.servers) != 2 {
		t.Fatalf("expected 2 servers for distinct revisions, got %d", len(pool.servers))
	}
	if len(starts) != 2 || starts[1] != "v2@"+DefinitionDigest(defs["v2"]) {
		t.Fatalf("second call did not bind v2 definition: starts = %v", starts)
	}
}

func TestPoolBindsServerByExactRefNotAlias(t *testing.T) {
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	resA := &codemode.Resource{Ref: "a/mcp/fake", Kind: "mcp", Name: "fake", Registry: "a", Assessment: "allow"}
	resA.SetOrigin(&codemode.Origin{Server: fakeDef(fakeEnv())})
	resB := &codemode.Resource{Ref: "b/mcp/fake", Kind: "mcp", Name: "fake", Registry: "b", Assessment: "allow"}
	resB.SetOrigin(&codemode.Origin{Server: fakeDef(map[string]string{"GO_MCP_FAKE": "1", "OTHER": "x"})})

	if _, err := pool.Call(context.Background(), "fake", resA, "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("call A: %v", err)
	}
	if _, err := pool.Call(context.Background(), "fake", resB, "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("call B: %v", err)
	}
	if len(pool.servers) != 2 {
		t.Fatalf("expected 2 distinct servers for distinct refs, got %d", len(pool.servers))
	}
}

// spec: code-mode-mcp/only-declared-environment-forwarded
func TestDeclaredEnvForwardsOnlyDeclared(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("TOKEN", "abc")
	env := declaredEnv(map[string]string{"TOKEN": ""})
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "HOME=") {
			t.Fatalf("undeclared env forwarded: %v", env)
		}
	}
	if len(env) != 1 || env[0] != "TOKEN=abc" {
		t.Fatalf("env = %v", env)
	}

	env = declaredEnv(map[string]string{"PATH": ""})
	found := false
	for _, kv := range env {
		if kv == "PATH=/usr/bin:/bin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("declared PATH not forwarded: %v", env)
	}
}

func TestHTTPLoopbackHostExactMatch(t *testing.T) {
	for _, bad := range []string{
		"http://localhost.evil.com/mcp",
		"http://localhostevil/mcp",
		"http://127.0.0.1.evil.com/mcp",
		"http://example.com/@localhost",
		"http://foo.localhost.evil/mcp",
	} {
		if _, err := NewHTTP(&codemode.ServerDef{URL: bad}); err == nil {
			t.Fatalf("URL %q must be rejected", bad)
		}
	}
	for _, good := range []string{
		"http://localhost:8080/mcp",
		"http://127.0.0.1:9999/mcp",
		"http://[::1]:7777/mcp",
		"https://mcp.example.com/mcp",
	} {
		if _, err := NewHTTP(&codemode.ServerDef{URL: good}); err != nil {
			t.Fatalf("URL %q rejected: %v", good, err)
		}
	}
}

func TestPoolObserveStartsFiresOncePerServer(t *testing.T) {
	pool := NewPool(NewSchemaCache(t.TempDir()), 5*time.Second)
	var starts []string
	pool.ObserveStarts(func(alias string, res *codemode.Resource, def *codemode.ServerDef) {
		starts = append(starts, res.Ref+"@"+def.Digest)
	})
	res := fakeResource(fakeDef(fakeEnv()))
	if _, err := pool.Call(context.Background(), "fake", res, "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("call: %v", err)
	}
	if _, err := pool.Call(context.Background(), "fake", res, "ping", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("call2: %v", err)
	}
	if len(starts) != 1 || !strings.HasPrefix(starts[0], "reg/mcp/fake@") {
		t.Fatalf("starts = %v", starts)
	}
}

func TestCallCancelledContextKillsServer(t *testing.T) {
	pool := NewPool(NewSchemaCache(t.TempDir()), 30*time.Second)
	res := fakeResource(fakeDef(fakeEnv()))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := pool.Call(ctx, "fake", res, "hang", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error on cancelled call")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("hung server not terminated promptly")
	}
	var ce *codemode.Error
	if !errors.As(err, &ce) || ce.Code != codemode.ErrCodeTimeout {
		t.Fatalf("expected %s, got %v", codemode.ErrCodeTimeout, err)
	}
}

func BenchmarkToolSchemaCacheLookup(b *testing.B) {
	cache := NewSchemaCache(b.TempDir())
	tools := make([]codemode.ToolSchema, 64)
	for i := range tools {
		tools[i] = codemode.ToolSchema{Name: "tool" + strings.Repeat("x", i%8)}
	}
	def := fakeDef(fakeEnv())
	digest := DefinitionDigest(def)
	if err := cache.Put(digest, tools); err != nil {
		b.Fatal(err)
	}
	pool := NewPool(cache, time.Second)
	res := fakeResource(def)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := pool.Tools(context.Background(), "fake", res)
		if err != nil || len(got) != 64 {
			b.Fatalf("Tools = %v, %v", got, err)
		}
	}
}
