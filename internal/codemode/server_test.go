package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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
