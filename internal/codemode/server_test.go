package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// spec: code-mode-mcp/requests-answered-over-stdio
func TestServerE2E(t *testing.T) {
	store := &SnapshotStore{}
	store.Store(&Snapshot{Resources: []Resource{{
		Ref:         "reg/skill/one",
		Kind:        "skill",
		Name:        "one",
		Description: "first",
		Registry:    "reg",
		Revision:    "rev1",
		Assessment:  "allow",
	}}, AsOf: time.Now()})

	engine := &Engine{
		Snapshots: store,
		Skills:    NewFileSkillLoader(false),
		Log:       NewSessionLog(t.TempDir()+"/s.jsonl", "e2e"),
	}
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search","arguments":{"code":"async () => catalog.filter(r => r.kind === 'skill').map(r => r.ref)"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"ping"}`,
	}, "\n") + "\n")
	var out bytes.Buffer
	srv := &Server{Engine: engine, In: in, Out: &out}
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	var resps []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var r rpcResponse
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("response line %q: %v", line, err)
		}
		resps = append(resps, r)
	}
	if len(resps) != 4 {
		t.Fatalf("expected 4 responses, got %d: %s", len(resps), out.String())
	}

	if resps[0].Error != nil {
		t.Fatalf("initialize error: %+v", resps[0].Error)
	}

	raw, _ := json.Marshal(resps[1].Result)
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Tools) != 2 {
		t.Fatalf("tools/list: %s", raw)
	}

	raw, _ = json.Marshal(resps[2].Result)
	var call struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &call); err != nil || call.IsError {
		t.Fatalf("search call: %s", raw)
	}
	var payload struct {
		Value    []string `json:"value"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(call.Content[0].Text), &payload); err != nil {
		t.Fatalf("search payload: %v", err)
	}
	if len(payload.Value) != 1 || payload.Value[0] != "reg/skill/one" {
		t.Fatalf("search value: %s", call.Content[0].Text)
	}

	if resps[3].Error != nil {
		t.Fatalf("ping error: %+v", resps[3].Error)
	}
}

func serveTestEngine() *Engine {
	store := &SnapshotStore{}
	store.Store(&Snapshot{Resources: []Resource{{
		Ref: "reg/mcp/srv", Kind: "mcp", Name: "srv", Registry: "reg", Assessment: "allow",
	}}, AsOf: time.Now()})
	return &Engine{Snapshots: store, Skills: fakeSkills{}, Pool: newFakePool()}
}

// spec: code-mode-mcp/server-exits-on-shutdown
func TestServeReturnsOnContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	var out bytes.Buffer
	srv := &Server{Engine: serveTestEngine(), In: pr, Out: &out}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	if _, err := fmt.Fprintln(pw, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatalf("write ping request: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v on context cancel", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve blocked on open stdin after context cancel")
	}
}

func TestServeCancelCancelsPendingToolCall(t *testing.T) {
	engine := serveTestEngine()
	engine.Pool = &blockingPool{}
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	var out bytes.Buffer
	srv := &Server{Engine: engine, In: pr, Out: &out}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	if _, err := fmt.Fprintln(pw, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute","arguments":{"servers":["reg/mcp/srv"],"code":"async () => mcp.srv.ping()"}}}`); err != nil {
		t.Fatalf("write tools/call request: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v after cancelling a pending call", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after context cancel with a pending request")
	}
}
