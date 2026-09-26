package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/dop251/goja"
)

type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

type SkillContent struct {
	Ref      string    `json:"ref"`
	Revision string    `json:"revision"`
	Digest   string    `json:"digest"`
	Content  string    `json:"content"`
	Files    []string  `json:"files"`
	Findings []Finding `json:"findings,omitempty"`
}

type SkillLoader interface {
	Load(ctx context.Context, res *Resource) (SkillContent, error)
	ReadFile(ctx context.Context, res *Resource, path string) (string, error)
}

type ServerPool interface {
	Tools(ctx context.Context, alias string, res *Resource) ([]ToolSchema, error)

	Call(ctx context.Context, alias string, res *Resource, tool string, args json.RawMessage) (any, error)

	Close() error
}

type Engine struct {
	Snapshots *SnapshotStore
	Skills    SkillLoader
	Pool      ServerPool
	Log       *SessionLog
	Limits    Limits

	Gate func(ctx context.Context, res *Resource) error
}

type StartObserver interface {
	ObserveStarts(func(alias string, res *Resource, def *ServerDef))
}

type toolCallArgs struct {
	Code    string   `json:"code"`
	Servers []string `json:"servers,omitempty"`
}

func (e *Engine) Search(ctx context.Context, argsJSON json.RawMessage) (Result, []string, error) {
	var args toolCallArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil || args.Code == "" {
		return Result{}, nil, CodeError(ErrCodeInvalidArguments, "search requires {code}")
	}
	code := args.Code
	if stripped, serr := StripTypes(code); serr == nil {
		code = stripped
	}
	snap := e.Snapshots.Load()
	res, err := Run(ctx, code, Bindings{"catalog": snap.catalogJS()}, e.Limits)
	var warnings []string
	if snap.Stale {
		warnings = append(warnings, ErrCodeCatalogStale)
	}
	return res, warnings, err
}

func (e *Engine) Execute(ctx context.Context, argsJSON json.RawMessage) (Result, []string, error) {
	var args toolCallArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil || args.Code == "" {
		return Result{}, nil, CodeError(ErrCodeInvalidArguments, "execute requires {servers, code}")
	}
	code := args.Code
	if stripped, serr := StripTypes(code); serr == nil {
		code = stripped
	}
	snap := e.Snapshots.Load()

	declared := make(map[string]*Resource, len(args.Servers))
	names := make(map[string]int, len(args.Servers))
	for _, ref := range args.Servers {
		if !SanitizeRef(ref) {
			return Result{}, nil, CodeError(ErrCodeNotFound, "malformed ref %q", ref)
		}
		res := snap.Find(ref)
		if res == nil {
			return Result{}, nil, CodeError(ErrCodeNotFound, "no resource matches ref %q", ref)
		}
		if res.Kind != "mcp" {
			return Result{}, nil, CodeError(ErrCodeUnsupportedKind, "ref %q is kind %s, not an MCP server", ref, res.Kind)
		}
		declared[ref] = res
		names[res.Name]++
	}

	for _, ref := range args.Servers {
		if err := e.gate(ctx, declared[ref]); err != nil {
			return Result{}, nil, err
		}
	}

	if obs, ok := e.Pool.(StartObserver); ok {
		obs.ObserveStarts(func(alias string, res *Resource, def *ServerDef) {
			digest := ""
			if def != nil {
				digest = def.Digest
			}
			e.logEvent("server.start", res.Ref, res.Revision, digest)
		})
	}

	res, err := e.runExecute(ctx, snap, declared, names, args, code)
	var warnings []string
	if snap.Stale {
		warnings = append(warnings, ErrCodeCatalogStale)
	}
	return res, warnings, err
}

func (e *Engine) runExecute(ctx context.Context, snap *Snapshot, declared map[string]*Resource, names map[string]int, args toolCallArgs, code string) (Result, error) {

	aliases := make(map[string]string, len(declared))
	for ref, res := range declared {
		alias := res.Name
		if names[res.Name] > 1 {
			alias = res.Registry + "-" + res.Name
		}
		aliases[alias] = ref
	}

	vmBinder := func(runCtx context.Context, vm *goja.Runtime, counter *int64, limit int) (Bindings, error) {
		aliasObjects := make(map[string]*goja.Object, len(aliases))
		aliasNames := make([]string, 0, len(aliases))
		for alias, ref := range aliases {
			aliasObjects[alias] = e.aliasObject(runCtx, vm, alias, declared[ref], counter, limit)
			aliasNames = append(aliasNames, alias)
		}
		mcpObj := vm.NewDynamicObject(&mcpDynamic{vm: vm, aliases: aliasObjects, names: aliasNames})
		return Bindings{
			"catalog": snap.catalogJS(),
			"skills":  e.skillsBinding(runCtx, snap),
			"mcp":     mcpObj,
		}, nil
	}
	return RunWithBinder(ctx, code, vmBinder, e.Limits)
}

func (e *Engine) aliasObject(ctx context.Context, vm *goja.Runtime, alias string, res *Resource, counter *int64, limit int) *goja.Object {
	pool := e.Pool
	obj := vm.NewDynamicObject(&aliasDynamic{
		vm: vm,
		get: func(tool string) (any, bool) {
			if tool == "listTools" {
				return func(goja.FunctionCall) goja.Value {
					countHostCall(vm, counter, limit)
					tools, err := pool.Tools(ctx, alias, res)
					if err != nil {
						throwCoded(vm, coded(err, ErrCodeToolError, "list tools on %s", alias))
						return nil
					}
					return vm.ToValue(tools)
				}, true
			}
			return func(call goja.FunctionCall) goja.Value {
				countHostCall(vm, counter, limit)
				var args json.RawMessage
				if av := call.Argument(0); av != nil && !goja.IsUndefined(av) && !goja.IsNull(av) {
					var exported any
					if err := vm.ExportTo(av, &exported); err == nil {
						args, _ = json.Marshal(exported)
					}
				}
				out, err := pool.Call(ctx, alias, res, tool, args)
				if err != nil {
					throwCoded(vm, coded(err, ErrCodeToolError, "tool %s on %s", tool, alias))
					return nil
				}
				return vm.ToValue(out)
			}, true
		},
	})
	return obj
}

type mcpDynamic struct {
	vm      *goja.Runtime
	aliases map[string]*goja.Object
	names   []string
}

func (d *mcpDynamic) Get(key string) goja.Value {
	if obj, ok := d.aliases[key]; ok {
		return obj
	}
	panic(d.vm.ToValue(CodeError(ErrCodeNotDeclared, "server %q was not listed in the servers argument", key)))
}

func (d *mcpDynamic) Set(key string, val goja.Value) bool { return false }
func (d *mcpDynamic) Has(key string) bool {
	_, ok := d.aliases[key]
	return ok
}
func (d *mcpDynamic) Delete(key string) bool { return false }
func (d *mcpDynamic) Keys() []string         { return d.names }

type aliasDynamic struct {
	vm  *goja.Runtime
	get func(string) (any, bool)
}

func (d *aliasDynamic) Get(key string) goja.Value {
	if key == "listTools" {
		v, _ := d.get(key)
		return d.vm.ToValue(v)
	}
	v, ok := d.get(key)
	if !ok {
		return goja.Undefined()
	}
	return d.vm.ToValue(v)
}

func (d *aliasDynamic) Set(key string, val goja.Value) bool { return false }
func (d *aliasDynamic) Has(key string) bool                 { return key == "listTools" }
func (d *aliasDynamic) Delete(key string) bool              { return false }
func (d *aliasDynamic) Keys() []string                      { return []string{"listTools"} }

func (e *Engine) skillsBinding(ctx context.Context, snap *Snapshot) map[string]any {
	return map[string]any{
		"load": func(ref string) (map[string]any, error) {
			res, err := e.gatedSkill(snap, ref)
			if err != nil {
				return nil, err
			}
			if err := e.gate(ctx, res); err != nil {
				return nil, err
			}
			content, err := e.Skills.Load(ctx, res)
			if err != nil {
				return nil, err
			}
			e.logEvent("skill.load", res.Ref, res.Revision, content.Digest)
			return map[string]any{
				"ref": content.Ref, "revision": content.Revision,
				"digest": content.Digest, "content": content.Content,
				"files": content.Files, "findings": content.Findings,
			}, nil
		},
		"read": func(ref, path string) (string, error) {
			res, err := e.gatedSkill(snap, ref)
			if err != nil {
				return "", err
			}
			if err := e.gate(ctx, res); err != nil {
				return "", err
			}
			content, err := e.Skills.ReadFile(ctx, res, path)
			if err != nil {
				return "", err
			}
			e.logEvent("skill.read", res.Ref, res.Revision, "")
			return content, nil
		},
	}
}

func (e *Engine) gatedSkill(snap *Snapshot, ref string) (*Resource, error) {
	if !SanitizeRef(ref) {
		return nil, CodeError(ErrCodeNotFound, "malformed ref %q", ref)
	}
	res := snap.Find(ref)
	if res == nil {
		return nil, CodeError(ErrCodeNotFound, "no resource matches ref %q", ref)
	}
	if res.Kind != "skill" {
		return nil, CodeError(ErrCodeUnsupportedKind, "ref %q is kind %s; only skills load on demand", ref, res.Kind)
	}
	return res, nil
}

func (e *Engine) gate(ctx context.Context, res *Resource) error {
	if e.Gate == nil || res == nil {
		return nil
	}
	return e.Gate(ctx, res)
}

func (e *Engine) logEvent(event, ref, revision, digest string) {
	if e.Log == nil {
		return
	}
	if err := e.Log.Append(event, ref, revision, digest); err != nil {
		slog.Warn("code-mode session log append failed", "event", event, "ref", ref, "error", err)
	}
}

func coded(err error, code, format string, args ...any) *Error {
	var ce *Error
	if asError(err, &ce) {
		return ce
	}
	return CodeError(code, "%s: %v", fmt.Sprintf(format, args...), err)
}

func asError(err error, target **Error) bool {
	for err != nil {
		if ce, ok := err.(*Error); ok {
			*target = ce
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
