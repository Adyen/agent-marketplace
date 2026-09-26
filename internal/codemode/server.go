package codemode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

const (
	mcpProtocolVersion = "2025-06-18"
	serverName         = "agent-marketplace"
	serverVersion      = "code-mode"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type Server struct {
	Engine *Engine
	In     io.Reader
	Out    io.Writer
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Server) Serve(ctx context.Context) error {
	if closer, ok := s.In.(io.Closer); ok {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				_ = closer.Close()
			case <-done:
			}
		}()
	}
	scanner := bufio.NewScanner(s.In)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	enc := json.NewEncoder(s.Out)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		resp, ok := s.dispatch(ctx, &msg)
		if !ok {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return scanner.Err()
}

func (s *Server) dispatch(ctx context.Context, msg *rpcMessage) (rpcResponse, bool) {
	respond := func(result any) (rpcResponse, bool) {
		return rpcResponse{JSONRPC: "2.0", ID: msg.ID, Result: result}, true
	}
	fail := func(code int, message string) (rpcResponse, bool) {
		return rpcResponse{JSONRPC: "2.0", ID: msg.ID, Error: &rpcError{Code: code, Message: message}}, true
	}

	switch msg.Method {
	case "initialize":
		return respond(map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		})
	case "notifications/initialized", "notifications/cancelled":
		return rpcResponse{}, false
	case "ping":
		return respond(map[string]any{})
	case "tools/list":
		return respond(map[string]any{"tools": ToolDefinitions()})
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return fail(-32602, "invalid tools/call params")
		}
		return respond(s.callTool(ctx, params.Name, params.Arguments))
	default:

		if len(msg.ID) == 0 {
			return rpcResponse{}, false
		}
		return fail(-32601, "method not found: "+msg.Method)
	}
}

func (s *Server) callTool(ctx context.Context, name string, args json.RawMessage) toolResult {
	var (
		res      Result
		warnings []string
		err      error
	)
	switch name {
	case "search":
		res, warnings, err = s.Engine.Search(ctx, args)
	case "execute":
		res, warnings, err = s.Engine.Execute(ctx, args)
	default:
		return errorResult(CodeError(ErrCodeNotFound, "unknown tool %q", name))
	}
	if err != nil {
		var ce *Error
		if errors.As(err, &ce) {
			return errorResult(ce)
		}
		return errorResult(CodeError(ErrCodeScriptError, "%v", err))
	}
	payload, merr := json.Marshal(map[string]any{
		"value":     res.Value,
		"truncated": res.Truncated,
		"warnings":  warnings,
	})
	if merr != nil {
		return errorResult(CodeError(ErrCodeScriptError, "marshal result: %v", merr))
	}
	slog.Debug("code-mode tool call", "tool", name, "truncated", res.Truncated, "warnings", warnings)
	return toolResult{Content: []toolContent{{Type: "text", Text: string(payload)}}}
}

func errorResult(err *Error) toolResult {
	payload, _ := json.Marshal(map[string]any{"error": err})
	return toolResult{Content: []toolContent{{Type: "text", Text: string(payload)}}, IsError: true}
}

func ToolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "search",
			"description": "Run an async JavaScript arrow function against the read-only code-mode catalog. " + dtsCatalog + " Never starts servers and never writes. Returns {value, truncated, warnings}.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"code": map[string]any{"type": "string", "description": "async arrow function source, e.g. async () => catalog.filter(r => r.kind === 'skill')"}},
				"required":   []string{"code"},
			},
		},
		{
			"name":        "execute",
			"description": "Run an async JavaScript arrow function with catalog, skills, and the declared MCP servers bound under mcp. " + dtsCatalog + dtsSkills + dtsMCP + " Every ref in servers must be an MCP server from the catalog; only those servers may start. Returns {value, truncated, warnings}.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code":    map[string]any{"type": "string", "description": "async arrow function source"},
					"servers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "exact MCP server refs to bind under mcp"},
				},
				"required": []string{"code"},
			},
		},
	}
}

const dtsCatalog = `
declare const catalog: Array<{
  ref: string;            // "<registry>/<kind>/<name>"
  kind: "skill"|"command"|"subagent"|"hook"|"mcp"|"collection";
  name: string; description: string; tags?: string[];
  registry: string; revision: string;
  assessment: "allow"|"warn"; findings?: Array<{message:string,severity?:string,rule?:string}>;
  tools?: Array<{name:string,description?:string,inputSchema?:object}>|"unknown";
}>;`

const dtsSkills = `
declare const skills: {
  load(ref: string): Promise<{ref:string,revision:string,digest:string,content:string,files:string[],findings?:object[]}>;
  read(ref: string, path: string): Promise<string>;
};`

const dtsMCP = `
declare const mcp: Record<string, {
  listTools(): Promise<Array<{name:string,description?:string,inputSchema?:object}>>;
  [tool: string]: (args?: object) => Promise<any>;
}>;`
