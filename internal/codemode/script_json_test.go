package codemode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type wirePool struct{}

func (wirePool) Tools(ctx context.Context, alias string, res *Resource) ([]ToolSchema, error) {
	return []ToolSchema{{
		Name:        "ping",
		Description: "pong back",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}}}`),
	}}, nil
}

type wireCallResult struct {
	BodyText string   `json:"text"`
	Segments []string `json:"segments,omitempty"`
}

func (wirePool) Call(ctx context.Context, alias string, res *Resource, tool string, args json.RawMessage) (any, error) {
	return wireCallResult{BodyText: "pong", Segments: []string{"a"}}, nil
}

func (wirePool) Close() error { return nil }

func wireEngine(t *testing.T) *Engine {
	t.Helper()
	e, _ := testEngine(t, []Resource{
		{Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow"},
	})
	e.Pool = wirePool{}
	return e
}

// spec: code-mode-mcp/listtools-returns-documented-json-fields
func TestListToolsReturnsJSONFields(t *testing.T) {
	e := wireEngine(t)
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => (await mcp.srv.listTools())[0]`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var tool map[string]any
	if err := json.Unmarshal(res.Value, &tool); err != nil {
		t.Fatalf("decode result %s: %v", res.Value, err)
	}
	if tool["name"] != "ping" || tool["description"] != "pong back" {
		t.Fatalf("tool fields = %s", res.Value)
	}
	schema, ok := tool["inputSchema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("inputSchema = %v, want a JSON object", tool["inputSchema"])
	}
	if strings.Contains(string(res.Value), "Name") || strings.Contains(string(res.Value), "Description") {
		t.Fatalf("Go struct field names leaked: %s", res.Value)
	}
}

// spec: code-mode-mcp/tool-calls-preserve-json-field-names
func TestToolCallPreservesJSONFieldNames(t *testing.T) {
	e := wireEngine(t)
	args, _ := json.Marshal(map[string]any{
		"servers": []string{"reg/mcp/srv"},
		"code":    `async () => { const r = await mcp.srv.ping(); return [r.text, r.BodyText === undefined, r.segments] }`,
	})
	res, _, err := e.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(res.Value) != `["pong",true,["a"]]` {
		t.Fatalf("tool result = %s, want JSON field names", res.Value)
	}
}
