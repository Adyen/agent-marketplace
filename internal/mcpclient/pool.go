package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/codemode"
)

type SchemaCache struct {
	dir string
}

func DefaultSchemaCachePath() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "agent-marketplace", "mcp-tools")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "agent-marketplace", "mcp-tools")
}

func NewSchemaCache(dir string) *SchemaCache { return &SchemaCache{dir: dir} }

func DefinitionDigest(def *codemode.ServerDef) string {
	if def.Digest != "" {
		return def.Digest
	}
	raw, _ := json.Marshal(def)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (c *SchemaCache) path(digest string) string {
	safe := digest
	if len(safe) > 7 && safe[:7] == "sha256:" {
		safe = safe[7:]
	}
	return filepath.Join(c.dir, safe+".json")
}

func (c *SchemaCache) Get(digest string) ([]codemode.ToolSchema, bool) {
	if c == nil || c.dir == "" {
		return nil, false
	}
	data, err := os.ReadFile(c.path(digest))
	if err != nil {
		return nil, false
	}
	var tools []codemode.ToolSchema
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, false
	}
	return tools, true
}

func (c *SchemaCache) Put(digest string, tools []codemode.ToolSchema) error {
	if c == nil || c.dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".tmp-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), c.path(digest))
}

type serverEntry struct {
	client Client
	dead   bool
}

type Pool struct {
	Schemas *SchemaCache

	Deadline time.Duration

	ResolveDef func(ctx context.Context, res *codemode.Resource) (*codemode.ServerDef, error)

	mu      sync.Mutex
	servers map[string]*serverEntry
	defs    map[string]*codemode.ServerDef
	onStart func(alias string, res *codemode.Resource, def *codemode.ServerDef)
	closed  bool
}

func NewPool(schemas *SchemaCache, deadline time.Duration) *Pool {
	if deadline <= 0 {
		deadline = 30 * time.Second
	}
	return &Pool{Schemas: schemas, Deadline: deadline, servers: map[string]*serverEntry{}, defs: map[string]*codemode.ServerDef{}}
}

func (p *Pool) ObserveStarts(fn func(alias string, res *codemode.Resource, def *codemode.ServerDef)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStart = fn
}

func (p *Pool) resolveDef(ctx context.Context, res *codemode.Resource) (*codemode.ServerDef, error) {
	if def := res.ServerDef(); def != nil {
		return def, nil
	}
	key := res.Ref + "|" + res.Revision
	p.mu.Lock()
	if def := p.defs[key]; def != nil {
		p.mu.Unlock()
		return def, nil
	}
	p.mu.Unlock()
	if p.ResolveDef == nil {
		return nil, nil
	}
	def, err := p.ResolveDef(ctx, res)
	if err != nil {
		return nil, err
	}
	if def != nil {
		p.mu.Lock()
		p.defs[key] = def
		p.mu.Unlock()
	}
	return def, nil
}

func serverKey(res *codemode.Resource, def *codemode.ServerDef) string {
	digest := ""
	if def != nil {
		digest = DefinitionDigest(def)
	}
	return res.Ref + "|" + digest
}

func (p *Pool) get(ctx context.Context, key, alias string, res *codemode.Resource, def *codemode.ServerDef) (*serverEntry, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "session closed")
	}
	if e, ok := p.servers[key]; ok {
		p.mu.Unlock()
		if e.dead {
			return nil, codemode.CodeError(codemode.ErrCodeToolError, "server %q exited and is not restarted", alias)
		}
		return e, nil
	}
	p.mu.Unlock()

	client, err := p.startOne(ctx, def, res.Ref)
	if err != nil {
		return nil, err
	}
	slog.Info("code-mode starting MCP server", "alias", alias, "ref", res.Ref)
	entry := &serverEntry{client: client}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = client.Close()
		return nil, codemode.CodeError(codemode.ErrCodeToolError, "session closed")
	}
	if existing, ok := p.servers[key]; ok {
		_ = client.Close()
		return existing, nil
	}
	p.servers[key] = entry
	if p.onStart != nil {
		p.onStart(alias, res, def)
	}
	return entry, nil
}

func (p *Pool) startOne(ctx context.Context, def *codemode.ServerDef, ref string) (Client, error) {
	if def == nil {
		return nil, codemode.CodeError(codemode.ErrCodeStartFailed, "ref %q has no server definition", ref)
	}
	if def.URL != "" {
		return NewHTTP(def)
	}
	return Spawn(ctx, def)
}

func (p *Pool) markDead(key string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.servers[key]; ok {
		e.dead = true
	}
	slog.Warn("code-mode MCP server marked dead", "key", key, "error", err)
}

func (p *Pool) Tools(ctx context.Context, alias string, res *codemode.Resource) ([]codemode.ToolSchema, error) {
	def, err := p.resolveDef(ctx, res)
	if err != nil {
		return nil, err
	}
	digest := ""
	if def != nil {
		digest = DefinitionDigest(def)
	}
	if digest != "" {
		if tools, ok := p.Schemas.Get(digest); ok {
			slog.Debug("code-mode schema cache hit", "alias", alias)
			return tools, nil
		}
	}
	key := serverKey(res, def)
	entry, err := p.get(ctx, key, alias, res, def)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.Deadline)
	defer cancel()
	tools, err := entry.client.ListTools(ctx)
	if err != nil {
		var ce *codemode.Error
		if errors.As(err, &ce) && ce.Code == codemode.ErrCodeToolError {
			p.markDead(key, err)
		}
		return nil, err
	}
	if digest != "" {
		if err := p.Schemas.Put(digest, tools); err != nil {
			slog.Warn("code-mode schema cache write failed", "alias", alias, "error", err)
		}
	}
	return tools, nil
}

func (p *Pool) Call(ctx context.Context, alias string, res *codemode.Resource, tool string, args json.RawMessage) (any, error) {
	def, err := p.resolveDef(ctx, res)
	if err != nil {
		return nil, err
	}
	key := serverKey(res, def)
	entry, err := p.get(ctx, key, alias, res, def)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.Deadline)
	defer cancel()
	out, err := entry.client.Call(ctx, tool, args)
	if err != nil {
		var ce *codemode.Error
		if errors.As(err, &ce) && ce.Code == codemode.ErrCodeTimeout {
			p.markDead(key, err)
		}
		return nil, err
	}
	return out, nil
}

func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	var first error
	for alias, e := range p.servers {
		if err := e.client.Close(); err != nil && first == nil {
			first = err
		}
		slog.Debug("code-mode stopped MCP server", "alias", alias)
	}
	return first
}
