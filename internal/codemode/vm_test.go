package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunReturnsValue(t *testing.T) {
	res, err := Run(context.Background(), `async () => 1 + 2`, nil, Limits{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(string(res.Value)) != "3" {
		t.Fatalf("value = %s", res.Value)
	}
}

func TestRunAwaitHostCall(t *testing.T) {
	bind := Bindings{
		"svc": map[string]any{
			"double": func(x int) int { return x * 2 },
		},
	}
	res, err := Run(context.Background(), `async () => await svc.double(21)`, bind, Limits{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Value) != "42" {
		t.Fatalf("value = %s", res.Value)
	}
}

func TestRunTimeout(t *testing.T) {
	start := time.Now()
	_, err := Run(context.Background(), `async () => { for(;;){} }`, nil, Limits{Deadline: 50 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeTimeout {
		t.Fatalf("expected %s, got %v", ErrCodeTimeout, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("interrupt took too long")
	}
}

func TestRunOutputCap(t *testing.T) {
	res, err := Run(context.Background(), `async () => "x".repeat(4096)`, nil, Limits{OutputCap: 128})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Fatal("expected truncated result")
	}
	if len(res.Value) > 256 {
		t.Fatalf("value not truncated: %d bytes", len(res.Value))
	}
}

func TestRunOutputCapSerializedWithinCap(t *testing.T) {
	res, err := Run(context.Background(), `async () => '"'.repeat(4096)`, nil, Limits{OutputCap: 128})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Fatal("expected truncated result")
	}
	if len(res.Value) > 128 {
		t.Fatalf("serialized output exceeds cap: %d bytes", len(res.Value))
	}
	var parsed map[string]any
	if err := json.Unmarshal(res.Value, &parsed); err != nil {
		t.Fatalf("truncated output is not valid JSON: %v", err)
	}
	if parsed["truncated"] != true {
		t.Fatalf("marker = %s", res.Value)
	}
}

func TestRunUndefinedSerializesToValidJSON(t *testing.T) {
	res, err := Run(context.Background(), `async () => undefined`, nil, Limits{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var parsed any
	if err := json.Unmarshal(res.Value, &parsed); err != nil {
		t.Fatalf("undefined result is not valid JSON: %q", res.Value)
	}
}

func TestRunHostCallLimit(t *testing.T) {
	calls := 0
	bind := Bindings{
		"svc": map[string]any{
			"tick": func() { calls++ },
		},
	}
	_, err := Run(context.Background(), `async () => { for (let i = 0; i < 10; i++) svc.tick() }`, bind, Limits{HostCallLimit: 3})
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeHostCallLimit {
		t.Fatalf("expected %s, got %v", ErrCodeHostCallLimit, err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRunNoDangerousGlobals(t *testing.T) {
	res, err := Run(context.Background(), `async () => [
		typeof process, typeof require, typeof fetch, typeof os, typeof globalThis.process
	]`, nil, Limits{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Value) != `["undefined","undefined","undefined","undefined","undefined"]` {
		t.Fatalf("globals exposed: %s", res.Value)
	}
}

func TestRunScriptErrorCode(t *testing.T) {
	_, err := Run(context.Background(), `async () => { throw new Error("boom") }`, nil, Limits{})
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != ErrCodeScriptError {
		t.Fatalf("expected %s, got %v", ErrCodeScriptError, err)
	}
	if strings.Contains(ce.Message, "internal/codemode") {
		t.Fatalf("host paths leaked: %s", ce.Message)
	}
}

func TestRunNonFunctionRejected(t *testing.T) {
	_, err := Run(context.Background(), `42`, nil, Limits{})
	if err == nil {
		t.Fatal("expected error for non-function code")
	}
}

func TestRunCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	_, err := Run(ctx, `async () => { for(;;){} }`, nil, Limits{Deadline: 10 * time.Second})
	if err == nil {
		t.Fatal("expected error on cancellation")
	}
}
